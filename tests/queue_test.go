package tests

import (
	"testing"

	"webtyp.com/backoff"
	"webtyp.com/externalcalls"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/storage/mem"
	"webtyp.com/time"
)

const second = time.Second

type fakeIDs struct{ n int }

func (f *fakeIDs) NewID() string {
	f.n++
	return "call-" + string(rune('0'+f.n))
}

// creator is what a real transaction of crud offers: Create. Here it writes to the same database.
type creator struct{ db *orm.DB }

func (c creator) Create(rec model.Model) error { return c.db.Create(rec) }

type fixture struct {
	t     *testing.T
	db    *orm.DB
	queue *externalcalls.Queue
	clock *time.Nano
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := orm.New(mem.New())
	policy, err := backoff.New(second, 60*second)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Nano(1000)
	queue, err := externalcalls.New(db, &fakeIDs{}, func() time.Nano { return clock }, policy)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, db: db, queue: queue, clock: &clock}
}

func (f *fixture) add(call string) string {
	f.t.Helper()
	id, err := f.queue.Add(creator{f.db}, externalcalls.Request{TenantId: "acme", Module: "payments", Entity: "payment", RecordId: "pay-1", Call: call})
	if err != nil {
		f.t.Fatalf("Add: %v", err)
	}
	return id
}

func (f *fixture) row(id string) externalcalls.QueuedCall {
	f.t.Helper()
	model := &externalcalls.QueuedCall{}
	row, err := externalcalls.ReadOneQueuedCall(f.db.Query(model).Where(externalcalls.QueuedCall_.Id).Eq(id), model)
	if err != nil {
		f.t.Fatalf("read %s: %v", id, err)
	}
	return *row
}

func TestQueue_AddStoresAQueuedCall(t *testing.T) {
	f := newFixture(t)
	id, err := f.queue.Add(creator{f.db}, externalcalls.Request{
		TenantId: "acme", Module: "payments", Entity: "payment", RecordId: "pay-1", Call: "charge",
	})
	if err != nil {
		t.Fatal(err)
	}
	row := f.row(id)
	if row.State != "queued" {
		t.Errorf("got %q, want queued", row.State)
	}
	if row.Attempts != 0 {
		t.Errorf("got %d, want 0", row.Attempts)
	}
	if row.NextAttemptAt != 1000 {
		t.Errorf("got %d, want 1000", row.NextAttemptAt)
	}
	if row.CreatedAt != 1000 || row.UpdatedAt != 1000 {
		t.Errorf("got %d/%d, want 1000/1000", row.CreatedAt, row.UpdatedAt)
	}
	if row.TenantId != "acme" || row.Call != "charge" || row.RecordId != "pay-1" {
		t.Errorf("got %q, %q, %q", row.TenantId, row.Call, row.RecordId)
	}
	if id != row.Id {
		t.Errorf("id mismatch: %q != %q", id, row.Id)
	}
}

func TestQueue_AddRequiresEveryField(t *testing.T) {
	f := newFixture(t)
	reqs := []externalcalls.Request{
		{Module: "payments", Entity: "payment", RecordId: "pay-1", Call: "charge"},
		{TenantId: "acme", Entity: "payment", RecordId: "pay-1", Call: "charge"},
		{TenantId: "acme", Module: "payments", RecordId: "pay-1", Call: "charge"},
		{TenantId: "acme", Module: "payments", Entity: "payment", Call: "charge"},
		{TenantId: "acme", Module: "payments", Entity: "payment", RecordId: "pay-1"},
	}
	for i, req := range reqs {
		_, err := f.queue.Add(creator{f.db}, req)
		if err != externalcalls.ErrRequestFieldRequired {
			t.Errorf("req %d: got %v, want ErrRequestFieldRequired", i, err)
		}
	}
	due, err := f.queue.ListDue(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("got %d due calls, want 0", len(due))
	}
}

func TestQueue_AddRequiresATransaction(t *testing.T) {
	f := newFixture(t)
	_, err := f.queue.Add(nil, externalcalls.Request{TenantId: "acme", Module: "payments", Entity: "payment", RecordId: "pay-1", Call: "charge"})
	if err != externalcalls.ErrTransactionRequired {
		t.Errorf("got %v, want ErrTransactionRequired", err)
	}
}

func TestNew_RequiresItsDependencies(t *testing.T) {
	db := orm.New(mem.New())
	ids := &fakeIDs{}
	now := func() time.Nano { return 1000 }
	policy, _ := backoff.New(second, 60*second)

	_, err := externalcalls.New(nil, ids, now, policy)
	if err != externalcalls.ErrDependenciesRequired {
		t.Errorf("nil db: got %v", err)
	}
	_, err = externalcalls.New(db, nil, now, policy)
	if err != externalcalls.ErrDependenciesRequired {
		t.Errorf("nil ids: got %v", err)
	}
	_, err = externalcalls.New(db, ids, nil, policy)
	if err != externalcalls.ErrDependenciesRequired {
		t.Errorf("nil now: got %v", err)
	}
	_, err = externalcalls.New(db, ids, now, backoff.Policy{})
	if err != externalcalls.ErrDependenciesRequired {
		t.Errorf("empty policy: got %v", err)
	}
}

func TestQueue_ListDueOrdersByNextAttempt(t *testing.T) {
	f := newFixture(t)
	*f.clock = 100
	idA := f.add("a")
	*f.clock = 200
	idB := f.add("b")
	*f.clock = 300
	_ = f.add("c")

	*f.clock = 250
	due, err := f.queue.ListDue(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 2 || due[0].Id != idA || due[1].Id != idB {
		t.Errorf("got %+v", due)
	}

	due, err = f.queue.ListDue(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Id != idA {
		t.Errorf("got %+v", due)
	}
}

func TestQueue_ListDueSkipsSettledCalls(t *testing.T) {
	f := newFixture(t)
	idA := f.add("a")
	idB := f.add("b")

	if err := f.queue.MarkDone(idA); err != nil {
		t.Fatal(err)
	}
	if err := f.queue.MarkFailedAttempt(idB, model.ExternalCallDefinition{Name: "b", Mode: model.WaitForReply, MaxWaitSeconds: 10}, "x"); err != nil {
		t.Fatal(err)
	}

	due, err := f.queue.ListDue(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 {
		t.Errorf("got %d due calls", len(due))
	}
}

func TestQueue_MarkDone(t *testing.T) {
	f := newFixture(t)
	id := f.add("charge")

	if err := f.queue.MarkDone(id); err != nil {
		t.Fatal(err)
	}
	row := f.row(id)
	if row.State != "done" || row.LastError != "" {
		t.Errorf("got %q, %q", row.State, row.LastError)
	}

	if err := f.queue.MarkDone(id); err != externalcalls.ErrCallNotQueued {
		t.Errorf("got %v, want ErrCallNotQueued", err)
	}
	if err := f.queue.MarkDone("nope"); err != externalcalls.ErrCallNotFound {
		t.Errorf("got %v, want ErrCallNotFound", err)
	}
}

func TestQueue_RetryInBackgroundWaitsThenFails(t *testing.T) {
	f := newFixture(t)
	*f.clock = 1000
	id := f.add("receipt")
	call := model.ExternalCallDefinition{Name: "receipt", Mode: model.RetryInBackground, MaxAttempts: 3}

	// failure 1
	if err := f.queue.MarkFailedAttempt(id, call, "reason 1"); err != nil {
		t.Fatal(err)
	}
	row := f.row(id)
	if row.State != "queued" || row.Attempts != 1 || row.NextAttemptAt != time.Nano(1000).Add(second) || row.LastError != "reason 1" {
		t.Errorf("fail 1: got %+v", row)
	}

	// failure 2
	*f.clock = time.Nano(1000).Add(second)
	if err := f.queue.MarkFailedAttempt(id, call, "reason 2"); err != nil {
		t.Fatal(err)
	}
	row = f.row(id)
	if row.NextAttemptAt != f.clock.Add(2 * second) {
		t.Errorf("fail 2: got %d, want %d", row.NextAttemptAt, f.clock.Add(2 * second))
	}

	// failure 3
	*f.clock = f.clock.Add(2 * second)
	if err := f.queue.MarkFailedAttempt(id, call, "reason 3"); err != nil {
		t.Fatal(err)
	}
	row = f.row(id)
	if row.State != "failed" || row.Attempts != 3 || row.LastError != "reason 3" {
		t.Errorf("fail 3: got %+v", row)
	}
}

func TestQueue_WaitForReplyFailsAtOnce(t *testing.T) {
	f := newFixture(t)
	id := f.add("charge")
	call := model.ExternalCallDefinition{Name: "charge", Mode: model.WaitForReply, MaxWaitSeconds: 10}

	if err := f.queue.MarkFailedAttempt(id, call, "x"); err != nil {
		t.Fatal(err)
	}
	row := f.row(id)
	if row.State != "failed" || row.Attempts != 1 {
		t.Errorf("got %+v", row)
	}
}

func TestQueue_WaitForConfirmationFailsAtOnce(t *testing.T) {
	f := newFixture(t)
	id := f.add("settle")
	call := model.ExternalCallDefinition{Name: "settle", Mode: model.WaitForConfirmation}

	if err := f.queue.MarkFailedAttempt(id, call, "x"); err != nil {
		t.Fatal(err)
	}
	row := f.row(id)
	if row.State != "failed" || row.Attempts != 1 {
		t.Errorf("got %+v", row)
	}
}

func TestQueue_FailedAttemptNeedsAValidCall(t *testing.T) {
	f := newFixture(t)
	id := f.add("charge")

	err := f.queue.MarkFailedAttempt(id, model.ExternalCallDefinition{Name: "charge"}, "x")
	if err != model.ErrExternalCallModeUndeclared {
		t.Errorf("got %v, want ErrExternalCallModeUndeclared", err)
	}
	row := f.row(id)
	if row.Attempts != 0 || row.State != "queued" {
		t.Errorf("got %+v", row)
	}
}
