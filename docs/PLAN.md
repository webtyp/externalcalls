---
PLAN: "feat!: externalcalls runs the calls that wait in the background (RunPendingCalls) and settles the ones that wait for a confirmation (ConfirmCall, RejectCall)"
TAG: v0.2.0
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 5651496193766925125
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work. Commit after every stage.
> Never run `git reset`, `git restore`, `git checkout -- <file>`, `git checkout HEAD <path>` or `git stash` on your own work.
> **Publishing:** you publish only by OPENING THE PULL REQUEST (a `git push` from your sandbox does not reach GitHub, and work not submitted is lost when the session ends). Open it after Stage 2 with whatever compiles and keep committing to it.
> **Never ask a yes/no question about progress**; the answer is always yes. Ask only when a signature in this plan is wrong, and name the symbol.
> Never write `go.mod`/`go.sum` lines by hand: only `go get` and `go mod tidy` do. Never edit `definition_gen.go`: run `definitionc` after editing `definition.go`, then repeat the `go get` line of «Tools» and `go mod tidy`. No scratch files and no scripts over the source.

# Plan: the executor of `externalcalls` (D58, D60)

`externalcalls` v0.1.1 is only the queue: `Add`, `ListDue`, `MarkDone`, `MarkFailedAttempt`. Nobody runs the calls and nobody settles a call that waits for a provider's confirmation. This plan adds both, **without importing `crud`** (the host — `crud`/`appkit`, later plans — implements a narrow `Runner`).

## Ecosystem search

Searched `outbox` (its `Deliver` is a browser queue that writes outside the caller's transaction: not reusable, D36), `crud` (owns no queue), `backoff` (already used for the delay). Nothing runs queued server calls: **extend** this library.

## Decisions taken in this plan (so the executor needs none)

1. **Who runs what, by mode** (D60): only `RetryInBackground` calls are run by `RunPendingCalls`. A `WaitForReply` call is run inline by `crud` inside the request (which settles it with `MarkDone` / `MarkFailedAttempt`); a `WaitForConfirmation` call waits for `ConfirmCall` / `RejectCall`. To keep the executor from listing (and starving on) the other two, **the row carries its mode**: `Request.Mode` (required) is stored in a new `mode` column, and `ListDue` answers only `RetryInBackground` rows. Recovering a `WaitForReply` row left `queued` by a crash is NOT in this plan (it belongs to the plan of `crud`).
2. **The idempotency key is the id of the queued call** (`QueuedCall.Id`): stable across retries and unique per call. The draft said «the record id», which two calls of the same record would share.
3. **The executor never makes a call itself**: it asks `Runner.Run`. An unknown declaration (the entity no longer declares that call) ends the call `failed` with a fixed reason; it is never retried.
4. `Request` gains a required field and `QueuedCall` a column: a **breaking** change of an unreleased-to-consumers library → tag `v0.2.0`.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
go install webtyp.com/definitionc/cmd/definitionc@v0.1.42
export PATH="$PATH:$(go env GOPATH)/bin"
go get webtyp.com/model@v0.2.19 webtyp.com/backoff@v0.2.0 webtyp.com/time@v0.6.5 webtyp.com/orm@v0.12.33
go mod tidy
```
`model.ExternalCallMode` values (read at `model@v0.2.19`, `external_call.go`): `WaitForReply`, `RetryInBackground`, `WaitForConfirmation` (constants of type `ExternalCallMode`, zero is invalid). `model.ExternalCallDefinition{Name, Mode, MaxWaitSeconds, MaxAttempts}` and its `Validate()` are the ones `MarkFailedAttempt` already uses. If a signature differs, stop and say so; never adapt it by guessing.

## Stage 1 — `definition.go` and `Request`/`Add`/`ListDue`

1. `definition.go`: add after the `call` field: `{Name: "mode", Type: model.Int(), NotNull: true, DB: &model.FieldDB{}},` (a mode is never zero, so `NotNull` is right). Run `definitionc`, repeat the `go get` line, `go mod tidy`. `migrate/migrate.go` needs no change (`Sync` adds the column).
2. `externalcalls.go`:
   - `ErrRequestFieldRequired` text becomes: `"externalcalls: the request lacks TenantId, Module, Entity, RecordId, Call or a valid Mode, so the call was not stored. Fill all six fields of externalcalls.Request."`
   - `Request` gains, after `Call`: `Mode model.ExternalCallMode // how the call is run: WaitForReply, RetryInBackground or WaitForConfirmation`, and its doc example becomes `externalcalls.Request{TenantId: "acme", Module: "payments", Entity: "payment", RecordId: "pay-1", Call: "charge", Mode: model.RetryInBackground}`.
   - In `Add`, replace the empty-field test by (a new unexported `validMode` follows):

```go
	if r.TenantId == "" || r.Module == "" || r.Entity == "" || r.RecordId == "" || r.Call == "" || !validMode(r.Mode) {
		return "", ErrRequestFieldRequired
	}
```
   and in the `QueuedCall` literal add `Mode: int64(r.Mode),` after `Call: r.Call,`.

```go
// validMode says whether m is one of the three declared modes (the zero value is not).
func validMode(m model.ExternalCallMode) bool {
	return m == model.WaitForReply || m == model.RetryInBackground || m == model.WaitForConfirmation
}
```
   - In `ListDue` add one condition (written exactly) after the `State` one: `Where(QueuedCall_.Mode).Eq(int64(model.RetryInBackground)).`; update its doc comment: «answers up to limit queued RetryInBackground calls whose next attempt has arrived, the oldest first».

## Stage 2 — the settling methods (`externalcalls.go`) and the executor (`run.go`, new)

1. New errors in the `const` block of `externalcalls.go` (house pattern: what happened, whether data is harmed, what to do):

```go
	ErrNotAwaitingConfirmation queueError = "externalcalls: that call does not wait for a confirmation, or it is already done or failed, so nothing changed. Only a queued WaitForConfirmation call can be confirmed or rejected."
	ErrRunnerRequired          queueError = "externalcalls: no runner was given, so no call was made and nothing changed. Pass the Runner that makes the calls to RunPendingCalls."
	ErrRunLimitNotPositive     queueError = "externalcalls: the limit must be greater than zero, so no call was made and nothing changed. Pass how many due calls to run, for example 50."
```
   and the fixed reason of an unknown declaration, a named constant (it is data stored in `LastError`, not an error value): `const reasonUnknownCall = "externalcalls: the entity no longer declares this call, so it was marked failed without running"`.
2. Refactor `MarkFailedAttempt` so the executor can tell «retried» from «failed»: move its body (everything after `call.Validate()`) into `failAttempt`, and keep the exported method as a wrapper. Written exactly:

```go
func (q *Queue) MarkFailedAttempt(id string, call model.ExternalCallDefinition, reason string) error {
	_, err := q.failAttempt(id, call, reason)
	return err
}

// failAttempt is MarkFailedAttempt and also answers the state the call ended in: queued (retry) or failed.
func (q *Queue) failAttempt(id string, call model.ExternalCallDefinition, reason string) (State, error) {
	if err := call.Validate(); err != nil {
		return "", err
	}
	row, err := q.queued(id)
	if err != nil {
		return "", err
	}
	now := q.now()
	row.Attempts++
	row.LastError = reason
	row.UpdatedAt = now
	row.State = string(StateFailed)
	if call.Mode == model.RetryInBackground && row.Attempts < call.MaxAttempts {
		delay, delayErr := q.retry.DelayAfter(row.Attempts)
		if delayErr != nil {
			return "", delayErr
		}
		row.State = string(StateQueued)
		row.NextAttemptAt = now.Add(delay)
	}
	err = q.db.UpdateFields(row, []string{QueuedCall_.State, QueuedCall_.Attempts, QueuedCall_.NextAttemptAt, QueuedCall_.LastError, QueuedCall_.UpdatedAt}, orm.Eq(QueuedCall_.Id, row.Id))
	return State(row.State), err
}
```
3. Add to `externalcalls.go` (the confirmation side; `awaitingConfirmation` and `fail` are shared helpers):

```go
// ConfirmCall settles a queued WaitForConfirmation call as done: the provider confirmed it. Whether the confirmation is
// true is decided before (the module's Confirm rule asks the provider again): this only records the result.
//
// Example: err := queue.ConfirmCall(id)
func (q *Queue) ConfirmCall(id string) error {
	row, err := q.awaitingConfirmation(id)
	if err != nil {
		return err
	}
	return q.settle(row, StateDone, "")
}

// RejectCall settles a queued WaitForConfirmation call as failed: the provider refused it, or the module's Confirm rule
// did not accept the confirmation. reason is stored in LastError.
//
// Example: err := queue.RejectCall(id, "card declined")
func (q *Queue) RejectCall(id, reason string) error {
	row, err := q.awaitingConfirmation(id)
	if err != nil {
		return err
	}
	return q.settle(row, StateFailed, reason)
}

// awaitingConfirmation reads a queued call and refuses one that does not wait for a confirmation.
func (q *Queue) awaitingConfirmation(id string) (*QueuedCall, error) {
	row, err := q.queued(id)
	if err != nil {
		return nil, err
	}
	if model.ExternalCallMode(row.Mode) != model.WaitForConfirmation {
		return nil, ErrNotAwaitingConfirmation
	}
	return row, nil
}

// settle ends a queued call in state with reason as its last error.
func (q *Queue) settle(row *QueuedCall, state State, reason string) error {
	row.State = string(state)
	row.LastError = reason
	row.UpdatedAt = q.now()
	return q.db.UpdateFields(row, []string{QueuedCall_.State, QueuedCall_.LastError, QueuedCall_.UpdatedAt}, orm.Eq(QueuedCall_.Id, row.Id))
}
```
   `MarkDone` may now call `q.settle(row, StateDone, "")` (same behaviour, one write path): do it, and delete its old body.
4. `run.go` (new), written exactly:

```go
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
```
   (`golint`-style «else after continue» is style-checked by `gotest`: if it complains, restructure with a `runErr := r.Run(...)` before the `if`, same behaviour.)

## Stage 3 — tests (`tests/queue_test.go` fixes, `tests/run_test.go` new; one row each in `docs/TESTS.md`)

Fix `tests/queue_test.go` (do not delete a test): give every `externalcalls.Request` literal `Mode: model.RetryInBackground`; the helper `f.add(call)` uses it too and a new `f.addMode(call string, mode model.ExternalCallMode) string` takes the mode; in `TestQueue_AddRequiresEveryField` the five existing requests also carry the mode (so each still fails for its own missing field) and a sixth case with `Mode` left zero is added; `TestQueue_AddStoresAQueuedCall` also asserts `row.Mode == int64(model.RetryInBackground)`.

`tests/run_test.go` (package `tests`) uses `newFixture`, `creator`, `f.addMode`, `f.row`, and this double, written exactly:

```go
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
```

| Test | setup → call → expected |
|---|---|
| `TestRun_AnsweredCallIsDone` | `id := f.addMode("receipt", model.RetryInBackground)`; runner known, `def {Name:"receipt", Mode: model.RetryInBackground, MaxAttempts: 3}`, `err nil`; `RunPendingCalls(r, 10)` → `Report{Done: 1}`; `f.row(id).State == "done"`; `r.keys == []string{id}` |
| `TestRun_FailureRetriesWithBackoffThenFails` | same def, `err = errors.New("down")`, clock 1000: run 1 → `Report{Retried: 1}`, row `queued`, `Attempts 1`, `NextAttemptAt == 1000 + 1s`; clock to that instant, run 2 → `Retried: 1`, `NextAttemptAt == clock + 2s`; clock advanced, run 3 → `Report{Failed: 1}`, `State "failed"`, `Attempts 3`, `LastError == "down"` |
| `TestRun_KeyIsTheCallIdOnEveryAttempt` | the previous scenario → `r.keys` is `[id, id, id]` (the same id each time) |
| `TestRun_UnknownDeclarationFailsWithoutRunning` | runner `known false` → `Report{Failed: 1}`, `State "failed"`, `LastError` is the fixed reason (contains `no longer declares`), `r.keys` is empty |
| `TestRun_OnlyRetryInBackgroundIsRun` | add one call of each mode; run with a runner that succeeds → `Report{Done: 1}`; the `WaitForReply` and `WaitForConfirmation` rows stay `queued`; `r.keys` has exactly the id of the `RetryInBackground` one |
| `TestRun_RespectsTheLimit` | three due `RetryInBackground` calls; `RunPendingCalls(r, 2)` → `Report{Done: 2}`; the third stays `queued` |
| `TestRun_NeedsARunnerAndALimit` | `RunPendingCalls(nil, 10)` → `ErrRunnerRequired`; `RunPendingCalls(r, 0)` → `ErrRunLimitNotPositive`; nothing changes |
| `TestConfirm_ConfirmationCallBecomesDone` | `id := f.addMode("settle", model.WaitForConfirmation)`; `ConfirmCall(id)` → `State "done"`; again → `ErrCallNotQueued` |
| `TestConfirm_RejectFailsWithTheReason` | the same, `RejectCall(id, "declined")` → `State "failed"`, `LastError == "declined"` |
| `TestConfirm_OtherModesAreRefused` | a `RetryInBackground` call and a `WaitForReply` call: `ConfirmCall` and `RejectCall` on each → `ErrNotAwaitingConfirmation`, rows unchanged; `ConfirmCall("nope")` → `ErrCallNotFound` |

`docs/TESTS.md`: one row per new test and the changed rows of `TestQueue_AddStoresAQueuedCall` / `TestQueue_AddRequiresEveryField`, in the existing format `| [Name](../tests/file_test.go) | rule |`.

## Stage 4 — docs and `AGENTS.md`

1. `AGENTS.md` «This repo»: replace «It stores, orders and settles calls; it **never makes** a call» with «It stores, orders and settles calls and runs the due `RetryInBackground` ones through the injected `Runner`; it **never makes** a call itself»; keep every other line.
2. `docs/diagrams/CALL_LIFECYCLE.md`: add the executor and the confirmation branches to the flowchart (no `subgraph`): `RunPendingCalls → ListDue (RetryInBackground only) → Declaration known? → Runner.Run → MarkDone | failAttempt`, and `WaitForConfirmation → ConfirmCall → done | RejectCall → failed`; extend the numbered walkthrough; header says `tests/queue_test.go` and `tests/run_test.go` verify it.
3. `docs/ARCHITECTURE.md`: section 1 says the library also runs due background calls through a `Runner`; section 2 table gains `run.go`; section 3 gains «Run the pending calls» and «Confirm a call»; section 4 explains why the mode is stored in the row (so the executor never lists calls it must not run) and that the idempotency key is the call id.
4. `README.md` (≤150 lines): the `Request` examples carry `Mode: model.RetryInBackground` and import `"webtyp.com/model"`; add `### Step 4: Run the pending calls` with a complete compilable `main` (imports `"webtyp.com/fmt"`, never stdlib `fmt`; a `runner` type with the two methods; prints the `Report`).

## Delivery check

1. `gotest` ends green (vet, style, race, tests, tests index ✅).
2. `grep -rn "webtyp.com/crud" --include=*.go .` → empty (the library still does not import `crud`).
3. `git diff --stat` lists `definition.go`, `definition_gen.go`, `externalcalls.go`, `run.go`, `go.mod`, `go.sum`, `tests/queue_test.go`, `tests/run_test.go`, `docs/*`, `README.md`, `AGENTS.md`; nothing is deleted.
4. The pull request is open and `git diff --stat main` shows only the files of item 3.
