package tests

import (
	"errors"
	"testing"

	"webtyp.com/externalcalls"
	"webtyp.com/model"
	"webtyp.com/time"
)

// runner answers one declaration and counts what it was asked; err is what Run answers.
type runner struct {
	def   model.ExternalCallDefinition
	known bool
	err   error
	keys  []string
}

func (r *runner) Declaration(module, entity, call string) (model.ExternalCallDefinition, bool) {
	return r.def, r.known
}

func (r *runner) Run(c externalcalls.QueuedCall, key string) error {
	r.keys = append(r.keys, key)
	return r.err
}

func TestRun_AnsweredCallIsDone(t *testing.T) {
	f := newFixture(t)
	id := f.addMode("receipt", model.RetryInBackground)
	r := &runner{
		known: true,
		def:   model.ExternalCallDefinition{Name: "receipt", Mode: model.RetryInBackground, MaxAttempts: 3},
		err:   nil,
	}
	rep, err := f.queue.RunPendingCalls(r, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Done != 1 {
		t.Errorf("got %v, want Done: 1", rep)
	}
	row := f.row(id)
	if row.State != "done" {
		t.Errorf("got %s, want done", row.State)
	}
	if len(r.keys) != 1 || r.keys[0] != id {
		t.Errorf("got keys %v", r.keys)
	}
}

func TestRun_FailureRetriesWithBackoffThenFails(t *testing.T) {
	f := newFixture(t)
	*f.clock = 1000
	id := f.addMode("receipt", model.RetryInBackground)
	r := &runner{
		known: true,
		def:   model.ExternalCallDefinition{Name: "receipt", Mode: model.RetryInBackground, MaxAttempts: 3},
		err:   errors.New("down"),
	}

	rep, err := f.queue.RunPendingCalls(r, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Retried != 1 {
		t.Errorf("run 1: got %v", rep)
	}
	row := f.row(id)
	if row.State != "queued" || row.Attempts != 1 || row.NextAttemptAt != 1000+time.Nano(time.Second) {
		t.Errorf("run 1: got state %s, attempts %d, next %d", row.State, row.Attempts, row.NextAttemptAt)
	}

	*f.clock = row.NextAttemptAt
	rep, err = f.queue.RunPendingCalls(r, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Retried != 1 {
		t.Errorf("run 2: got %v", rep)
	}
	row = f.row(id)
	if row.NextAttemptAt != *f.clock+time.Nano(2*time.Second) {
		t.Errorf("run 2: got next %d, want %d", row.NextAttemptAt, *f.clock+time.Nano(2*time.Second))
	}

	*f.clock = row.NextAttemptAt
	rep, err = f.queue.RunPendingCalls(r, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 1 {
		t.Errorf("run 3: got %v", rep)
	}
	row = f.row(id)
	if row.State != "failed" || row.Attempts != 3 || row.LastError != "down" {
		t.Errorf("run 3: got state %s, attempts %d, err %q", row.State, row.Attempts, row.LastError)
	}
}

func TestRun_KeyIsTheCallIdOnEveryAttempt(t *testing.T) {
	f := newFixture(t)
	*f.clock = 1000
	id := f.addMode("receipt", model.RetryInBackground)
	r := &runner{
		known: true,
		def:   model.ExternalCallDefinition{Name: "receipt", Mode: model.RetryInBackground, MaxAttempts: 3},
		err:   errors.New("down"),
	}

	for i := 0; i < 3; i++ {
		_, err := f.queue.RunPendingCalls(r, 10)
		if err != nil {
			t.Fatal(err)
		}
		*f.clock += time.Nano(2 * time.Second) // advance past any backoff
	}

	if len(r.keys) != 3 || r.keys[0] != id || r.keys[1] != id || r.keys[2] != id {
		t.Errorf("got keys %v", r.keys)
	}
}

func TestRun_UnknownDeclarationFailsWithoutRunning(t *testing.T) {
	f := newFixture(t)
	id := f.addMode("receipt", model.RetryInBackground)
	r := &runner{known: false}

	rep, err := f.queue.RunPendingCalls(r, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 1 {
		t.Errorf("got %v", rep)
	}
	row := f.row(id)
	if row.State != "failed" {
		t.Errorf("got state %s", row.State)
	}
	if len(r.keys) != 0 {
		t.Errorf("runner called: %v", r.keys)
	}
}

func TestRun_OnlyRetryInBackgroundIsRun(t *testing.T) {
	f := newFixture(t)
	idRetry := f.addMode("a", model.RetryInBackground)
	idWait := f.addMode("b", model.WaitForReply)
	idConf := f.addMode("c", model.WaitForConfirmation)

	r := &runner{
		known: true,
		def:   model.ExternalCallDefinition{Name: "a", Mode: model.RetryInBackground, MaxAttempts: 3},
		err:   nil,
	}

	rep, err := f.queue.RunPendingCalls(r, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Done != 1 {
		t.Errorf("got %v", rep)
	}

	if f.row(idWait).State != "queued" {
		t.Errorf("WaitForReply changed")
	}
	if f.row(idConf).State != "queued" {
		t.Errorf("WaitForConfirmation changed")
	}
	if len(r.keys) != 1 || r.keys[0] != idRetry {
		t.Errorf("run keys: %v", r.keys)
	}
}

func TestRun_RespectsTheLimit(t *testing.T) {
	f := newFixture(t)
	_ = f.addMode("a", model.RetryInBackground)
	_ = f.addMode("b", model.RetryInBackground)
	id3 := f.addMode("c", model.RetryInBackground)

	r := &runner{
		known: true,
		def:   model.ExternalCallDefinition{Name: "a", Mode: model.RetryInBackground, MaxAttempts: 3},
		err:   nil,
	}

	rep, err := f.queue.RunPendingCalls(r, 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Done != 2 {
		t.Errorf("got %v", rep)
	}
	if f.row(id3).State != "queued" {
		t.Errorf("3rd call not queued")
	}
}

func TestRun_NeedsARunnerAndALimit(t *testing.T) {
	f := newFixture(t)
	r := &runner{}

	_, err := f.queue.RunPendingCalls(nil, 10)
	if err != externalcalls.ErrRunnerRequired {
		t.Errorf("got %v", err)
	}

	_, err = f.queue.RunPendingCalls(r, 0)
	if err != externalcalls.ErrRunLimitNotPositive {
		t.Errorf("got %v", err)
	}
}

func TestConfirm_ConfirmationCallBecomesDone(t *testing.T) {
	f := newFixture(t)
	id := f.addMode("settle", model.WaitForConfirmation)

	if err := f.queue.ConfirmCall(id); err != nil {
		t.Fatal(err)
	}
	if f.row(id).State != "done" {
		t.Errorf("got state %s", f.row(id).State)
	}

	if err := f.queue.ConfirmCall(id); err != externalcalls.ErrCallNotQueued {
		t.Errorf("got %v, want ErrCallNotQueued", err)
	}
}

func TestConfirm_RejectFailsWithTheReason(t *testing.T) {
	f := newFixture(t)
	id := f.addMode("settle", model.WaitForConfirmation)

	if err := f.queue.RejectCall(id, "declined"); err != nil {
		t.Fatal(err)
	}
	row := f.row(id)
	if row.State != "failed" || row.LastError != "declined" {
		t.Errorf("got state %s, err %q", row.State, row.LastError)
	}
}

func TestConfirm_OtherModesAreRefused(t *testing.T) {
	f := newFixture(t)
	idRetry := f.addMode("a", model.RetryInBackground)
	idWait := f.addMode("b", model.WaitForReply)

	if err := f.queue.ConfirmCall(idRetry); err != externalcalls.ErrNotAwaitingConfirmation {
		t.Errorf("Confirm RetryInBackground: got %v", err)
	}
	if err := f.queue.RejectCall(idWait, "x"); err != externalcalls.ErrNotAwaitingConfirmation {
		t.Errorf("Reject WaitForReply: got %v", err)
	}
	if err := f.queue.ConfirmCall("nope"); err != externalcalls.ErrCallNotFound {
		t.Errorf("Confirm missing: got %v", err)
	}
}
