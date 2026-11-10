# Tests Map

| Test | Rule |
|---|---|
| [TestQueue_AddStoresAQueuedCall](../tests/queue_test.go) | clock 1000 → `Add` a request for call `charge` → row: `State == "queued"`, `Attempts == 0`, `NextAttemptAt == 1000`, `CreatedAt == UpdatedAt == 1000`, `TenantId == "acme"`, `Call == "charge"`, `RecordId == "pay-1"`, `Mode == RetryInBackground`; the returned id is the row's `Id` |
| [TestQueue_AddRequiresEveryField](../tests/queue_test.go) | for each of `TenantId`, `Module`, `Entity`, `RecordId`, `Call`, `Mode` left empty (one at a time) → `Add` returns `externalcalls.ErrRequestFieldRequired` and `ListDue(10)` is empty |
| [TestQueue_AddRequiresATransaction](../tests/queue_test.go) | `Add(nil, validRequest)` → `externalcalls.ErrTransactionRequired` |
| [TestNew_RequiresItsDependencies](../tests/queue_test.go) | `New` with a nil db, nil ids, nil now, or `backoff.Policy{}` → `externalcalls.ErrDependenciesRequired` each time |
| [TestQueue_ListDueOrdersByNextAttempt](../tests/queue_test.go) | add `a` at clock 100, `b` at 200, `c` at 300; clock 250 → `ListDue(10)` returns `a`, `b` in that order (not `c`); `ListDue(1)` returns only `a` |
| [TestQueue_ListDueSkipsSettledCalls](../tests/queue_test.go) | add `a`, `b`; `MarkDone(a)`; `MarkFailedAttempt(b, waitForReply, "x")` → `ListDue(10)` is empty |
| [TestQueue_MarkDone](../tests/queue_test.go) | add → `MarkDone(id)` → `State == "done"`, `LastError == ""`; `MarkDone(id)` again → `ErrCallNotQueued`; `MarkDone("nope")` → `ErrCallNotFound` |
| [TestQueue_RetryInBackgroundWaitsThenFails](../tests/queue_test.go) | call `{Name:"receipt", Mode: model.RetryInBackground, MaxAttempts: 3}`, clock 1000 → failure 1: `State "queued"`, `Attempts 1`, `NextAttemptAt == 1000 + 1s`, `LastError` = the reason; clock moves to that instant → failure 2: `NextAttemptAt == clock + 2s`; failure 3: `State "failed"`, `Attempts 3` |
| [TestQueue_WaitForReplyFailsAtOnce](../tests/queue_test.go) | call `{Name:"charge", Mode: model.WaitForReply, MaxWaitSeconds: 10}` → one failure → `State "failed"`, `Attempts 1` |
| [TestQueue_WaitForConfirmationFailsAtOnce](../tests/queue_test.go) | call `{Name:"settle", Mode: model.WaitForConfirmation}` → one failure → `State "failed"`, `Attempts 1` |
| [TestQueue_FailedAttemptNeedsAValidCall](../tests/queue_test.go) | `MarkFailedAttempt(id, model.ExternalCallDefinition{Name: "charge"}, "x")` (mode 0) → `model.ErrExternalCallModeUndeclared`; the row is unchanged (`Attempts 0`, `State "queued"`) |
| [TestRun_AnsweredCallIsDone](../tests/run_test.go) | `id := f.addMode("receipt", model.RetryInBackground)`; runner known, `def {Name:"receipt", Mode: model.RetryInBackground, MaxAttempts: 3}`, `err nil`; `RunPendingCalls(r, 10)` → `Report{Done: 1}`; `f.row(id).State == "done"`; `r.keys == []string{id}` |
| [TestRun_FailureRetriesWithBackoffThenFails](../tests/run_test.go) | same def, `err = errors.New("down")`, clock 1000: run 1 → `Report{Retried: 1}`, row `queued`, `Attempts 1`, `NextAttemptAt == 1000 + 1s`; clock to that instant, run 2 → `Retried: 1`, `NextAttemptAt == clock + 2s`; clock advanced, run 3 → `Report{Failed: 1}`, `State "failed"`, `Attempts 3`, `LastError == "down"` |
| [TestRun_KeyIsTheCallIdOnEveryAttempt](../tests/run_test.go) | the previous scenario → `r.keys` is `[id, id, id]` (the same id each time) |
| [TestRun_UnknownDeclarationFailsWithoutRunning](../tests/run_test.go) | runner `known false` → `Report{Failed: 1}`, `State "failed"`, `LastError` is the fixed reason (contains `no longer declares`), `r.keys` is empty |
| [TestRun_OnlyRetryInBackgroundIsRun](../tests/run_test.go) | add one call of each mode; run with a runner that succeeds → `Report{Done: 1}`; the `WaitForReply` and `WaitForConfirmation` rows stay `queued`; `r.keys` has exactly the id of the `RetryInBackground` one |
| [TestRun_RespectsTheLimit](../tests/run_test.go) | three due `RetryInBackground` calls; `RunPendingCalls(r, 2)` → `Report{Done: 2}`; the third stays `queued` |
| [TestRun_NeedsARunnerAndALimit](../tests/run_test.go) | `RunPendingCalls(nil, 10)` → `ErrRunnerRequired`; `RunPendingCalls(r, 0)` → `ErrRunLimitNotPositive`; nothing changes |
| [TestConfirm_ConfirmationCallBecomesDone](../tests/run_test.go) | `id := f.addMode("settle", model.WaitForConfirmation)`; `ConfirmCall(id)` → `State "done"`; again → `ErrCallNotQueued` |
| [TestConfirm_RejectFailsWithTheReason](../tests/run_test.go) | the same, `RejectCall(id, "declined")` → `State "failed"`, `LastError == "declined"` |
| [TestConfirm_OtherModesAreRefused](../tests/run_test.go) | a `RetryInBackground` call and a `WaitForReply` call: `ConfirmCall` and `RejectCall` on each → `ErrNotAwaitingConfirmation`, rows unchanged; `ConfirmCall("nope")` → `ErrCallNotFound` |
