// Package externalcalls is a durable queue of calls to other systems. A call is stored with its record
// (in the record's own transaction), listed when it is due, and ends done or failed. It never makes
// the call, never sleeps and never reads a clock: the caller injects the transaction, the clock and the ids.
//
//	queue, err := externalcalls.New(db, ids, time.NowNano, policy)
//	id, err := queue.Add(tx, externalcalls.Request{TenantId: "acme", Module: "payments", Entity: "payment", RecordId: "pay-1", Call: "charge"})
//
// Flow: https://github.com/webtyp/externalcalls/blob/main/docs/diagrams/CALL_LIFECYCLE.md
package externalcalls

import (
	"webtyp.com/backoff"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/time"
)

// State says where a queued call is in its life.
type State string

const (
	// StateQueued: waiting for its turn; it runs when its NextAttemptAt arrives.
	StateQueued State = "queued"
	// StateDone: the other system answered; the call is finished.
	StateDone State = "done"
	// StateFailed: the call will not be tried again; its LastError says why. The record is never deleted.
	StateFailed State = "failed"
)

type queueError string

func (e queueError) Error() string { return string(e) }

const (
	ErrDependenciesRequired queueError = "externalcalls: the database, the id generator, the clock and a retry policy made with backoff.New are all required, so no queue was created and nothing was stored. Pass all four to externalcalls.New."
	ErrTransactionRequired  queueError = "externalcalls: no transaction was given, so the call was not stored and the record is unaffected. Pass the transaction of the record (a crud.Tx) to Add."
	ErrRequestFieldRequired queueError = "externalcalls: the request lacks TenantId, Module, Entity, RecordId or Call, so the call was not stored. Fill all five fields of externalcalls.Request."
	ErrCallNotFound         queueError = "externalcalls: there is no queued call with that id, so nothing changed. Use the id that Add returned."
	ErrCallNotQueued        queueError = "externalcalls: that call is already done or failed, so nothing changed. Only a queued call can be settled."
)

// Creator is what Add needs from the transaction of the record: crud.Tx satisfies it.
type Creator interface {
	Create(rec model.Model) error
}

// Request says which call to queue and for which record.
//
//	externalcalls.Request{TenantId: "acme", Module: "payments", Entity: "payment", RecordId: "pay-1", Call: "charge"}
type Request struct {
	TenantId string
	Module   string
	Entity   string
	RecordId string
	Call     string // the call's name as declared in the entity's ExternalCalls, e.g. "charge"
}

// Queue stores and settles queued calls.
type Queue struct {
	db    *orm.DB
	ids   model.IDGenerator
	now   func() time.Nano
	retry backoff.Policy
}

// New creates a Queue. retry must come from backoff.New: it decides how long a RetryInBackground call waits
// after each failure.
//
// Example: queue, err := externalcalls.New(db, ids, time.NowNano, policy)
func New(db *orm.DB, ids model.IDGenerator, now func() time.Nano, retry backoff.Policy) (*Queue, error) {
	if db == nil || ids == nil || now == nil {
		return nil, ErrDependenciesRequired
	}
	if _, err := retry.DelayAfter(1); err != nil {
		return nil, ErrDependenciesRequired
	}
	return &Queue{db: db, ids: ids, now: now, retry: retry}, nil
}

// Add stores a queued call through tx, the transaction of the record, and answers its id. The call is due at once.
//
// Example: id, err := queue.Add(tx, externalcalls.Request{...})
func (q *Queue) Add(tx Creator, r Request) (string, error) {
	if tx == nil {
		return "", ErrTransactionRequired
	}
	if r.TenantId == "" || r.Module == "" || r.Entity == "" || r.RecordId == "" || r.Call == "" {
		return "", ErrRequestFieldRequired
	}
	now := q.now()
	row := &QueuedCall{
		Id:            q.ids.NewID(),
		TenantId:      r.TenantId,
		Module:        r.Module,
		Entity:        r.Entity,
		RecordId:      r.RecordId,
		Call:          r.Call,
		State:         string(StateQueued),
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := tx.Create(row); err != nil {
		return "", err
	}
	return row.Id, nil
}

// ListDue answers up to limit queued calls whose next attempt has arrived, the oldest first.
//
// Example: due, err := queue.ListDue(50)
func (q *Queue) ListDue(limit int) ([]QueuedCall, error) {
	row := &QueuedCall{}
	query := q.db.Query(row).
		Where(QueuedCall_.State).Eq(string(StateQueued)).
		Where(QueuedCall_.NextAttemptAt).Lte(int64(q.now())).
		OrderBy(QueuedCall_.NextAttemptAt).Asc().
		Limit(limit)
	list, err := ReadAllQueuedCall(query)
	if err != nil {
		return nil, err
	}
	due := make([]QueuedCall, len(list))
	for i := range list {
		due[i] = *list[i]
	}
	return due, nil
}

// MarkDone settles a queued call as done.
//
// Example: err := queue.MarkDone(id)
func (q *Queue) MarkDone(id string) error {
	row, err := q.queued(id)
	if err != nil {
		return err
	}
	row.State = string(StateDone)
	row.LastError = ""
	row.UpdatedAt = q.now()
	return q.db.UpdateFields(row, []string{QueuedCall_.State, QueuedCall_.LastError, QueuedCall_.UpdatedAt}, orm.Eq(QueuedCall_.Id, row.Id))
}

// MarkFailedAttempt records one failed attempt of a queued call. A RetryInBackground call that has attempts left goes back
// to queued, due after the backoff delay; every other case ends failed (a call that waits for a reply or for a confirmation
// is never retried in the background). call is the declaration of the call in the entity, checked with its Validate.
//
// Example: err := queue.MarkFailedAttempt(id, model.ExternalCallDefinition{Name: "receipt", Mode: model.RetryInBackground, MaxAttempts: 8}, "provider down")
func (q *Queue) MarkFailedAttempt(id string, call model.ExternalCallDefinition, reason string) error {
	if err := call.Validate(); err != nil {
		return err
	}
	row, err := q.queued(id)
	if err != nil {
		return err
	}
	now := q.now()
	row.Attempts++
	row.LastError = reason
	row.UpdatedAt = now
	row.State = string(StateFailed)
	if call.Mode == model.RetryInBackground && row.Attempts < call.MaxAttempts {
		delay, delayErr := q.retry.DelayAfter(row.Attempts)
		if delayErr != nil {
			return delayErr
		}
		row.State = string(StateQueued)
		row.NextAttemptAt = now + delay
	}
	return q.db.UpdateFields(row, []string{QueuedCall_.State, QueuedCall_.Attempts, QueuedCall_.NextAttemptAt, QueuedCall_.LastError, QueuedCall_.UpdatedAt}, orm.Eq(QueuedCall_.Id, row.Id))
}

// queued reads a call by id and refuses one that is not queued.
func (q *Queue) queued(id string) (*QueuedCall, error) {
	row := &QueuedCall{}
	_, err := ReadOneQueuedCall(q.db.Query(row).Where(QueuedCall_.Id).Eq(id), row)
	if err != nil {
		if orm.IsNotFound(err) {
			return nil, ErrCallNotFound
		}
		return nil, err
	}
	if State(row.State) != StateQueued {
		return nil, ErrCallNotQueued
	}
	return row, nil
}
