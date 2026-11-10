package externalcalls

import "webtyp.com/model"

// Runner is what RunPendingCalls needs from the host (crud and appkit, in a later plan): the declaration of a call and the
// way to make it. The queue never makes a call itself.
type Runner interface {
	// Declaration answers the declaration of the call `call` of `entity` in `module` (its mode and limits), and false when
	// the entity no longer declares it.
	Declaration(module, entity, call string) (model.ExternalCallDefinition, bool)
	// Run makes the call for the record of the queued call and answers the failure of the other system, or nil when it
	// answered. key is the idempotency key: the same value on every attempt of the same call, so a retried call is never
	// done twice by the other system.
	Run(call QueuedCall, key string) error
}

// Report says what one RunPendingCalls did.
type Report struct {
	Done    int // calls the other system answered
	Retried int // calls that failed and went back to the queue with a delay
	Failed  int // calls that will not be tried again
}

// RunPendingCalls runs up to limit due RetryInBackground calls through r, one after another, and settles each: done when
// Run answers nil; otherwise one failed attempt (back to queued with the backoff delay, or failed when MaxAttempts is
// reached). A call whose declaration r no longer knows ends failed. A failure of the queue itself (the database) stops the
// run and is returned with the report of what was settled so far.
//
// Example: report, err := queue.RunPendingCalls(runner, 50)
func (q *Queue) RunPendingCalls(r Runner, limit int) (Report, error) {
	var rep Report
	if r == nil {
		return rep, ErrRunnerRequired
	}
	if limit <= 0 {
		return rep, ErrRunLimitNotPositive
	}
	due, err := q.ListDue(limit)
	if err != nil {
		return rep, err
	}
	for _, c := range due {
		def, known := r.Declaration(c.Module, c.Entity, c.Call)
		if !known {
			row, err := q.queued(c.Id)
			if err != nil {
				return rep, err
			}
			if err := q.settle(row, StateFailed, reasonUnknownCall); err != nil {
				return rep, err
			}
			rep.Failed++
			continue
		}
		if runErr := r.Run(c, c.Id); runErr == nil {
			if err := q.MarkDone(c.Id); err != nil {
				return rep, err
			}
			rep.Done++
			continue
		} else {
			state, err := q.failAttempt(c.Id, def, runErr.Error())
			if err != nil {
				return rep, err
			}
			if state == StateQueued {
				rep.Retried++
			} else {
				rep.Failed++
			}
		}
	}
	return rep, nil
}
