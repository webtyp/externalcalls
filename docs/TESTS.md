# Tests Map

| Test | Rule |
|---|---|
| [TestQueue_AddStoresAQueuedCall](../tests/queue_test.go) | clock 1000 → `Add` a request for call `charge` → row: `State == "queued"`, `Attempts == 0`, `NextAttemptAt == 1000`, `CreatedAt == UpdatedAt == 1000`, `TenantId == "acme"`, `Call == "charge"`, `RecordId == "pay-1"`; the returned id is the row's `Id` |
| [TestQueue_AddRequiresEveryField](../tests/queue_test.go) | for each of `TenantId`, `Module`, `Entity`, `RecordId`, `Call` left empty (one at a time) → `Add` returns `externalcalls.ErrRequestFieldRequired` and `ListDue(10)` is empty |
| [TestQueue_AddRequiresATransaction](../tests/queue_test.go) | `Add(nil, validRequest)` → `externalcalls.ErrTransactionRequired` |
| [TestNew_RequiresItsDependencies](../tests/queue_test.go) | `New` with a nil db, nil ids, nil now, or `backoff.Policy{}` → `externalcalls.ErrDependenciesRequired` each time |
| [TestQueue_ListDueOrdersByNextAttempt](../tests/queue_test.go) | add `a` at clock 100, `b` at 200, `c` at 300; clock 250 → `ListDue(10)` returns `a`, `b` in that order (not `c`); `ListDue(1)` returns only `a` |
| [TestQueue_ListDueSkipsSettledCalls](../tests/queue_test.go) | add `a`, `b`; `MarkDone(a)`; `MarkFailedAttempt(b, waitForReply, "x")` → `ListDue(10)` is empty |
| [TestQueue_MarkDone](../tests/queue_test.go) | add → `MarkDone(id)` → `State == "done"`, `LastError == ""`; `MarkDone(id)` again → `ErrCallNotQueued`; `MarkDone("nope")` → `ErrCallNotFound` |
| [TestQueue_RetryInBackgroundWaitsThenFails](../tests/queue_test.go) | call `{Name:"receipt", Mode: model.RetryInBackground, MaxAttempts: 3}`, clock 1000 → failure 1: `State "queued"`, `Attempts 1`, `NextAttemptAt == 1000 + 1s`, `LastError` = the reason; clock moves to that instant → failure 2: `NextAttemptAt == clock + 2s`; failure 3: `State "failed"`, `Attempts 3` |
| [TestQueue_WaitForReplyFailsAtOnce](../tests/queue_test.go) | call `{Name:"charge", Mode: model.WaitForReply, MaxWaitSeconds: 10}` → one failure → `State "failed"`, `Attempts 1` |
| [TestQueue_WaitForConfirmationFailsAtOnce](../tests/queue_test.go) | call `{Name:"settle", Mode: model.WaitForConfirmation}` → one failure → `State "failed"`, `Attempts 1` |
| [TestQueue_FailedAttemptNeedsAValidCall](../tests/queue_test.go) | `MarkFailedAttempt(id, model.ExternalCallDefinition{Name: "charge"}, "x")` (mode 0) → `model.ErrExternalCallModeUndeclared`; the row is unchanged (`Attempts 0`, `State "queued"`) |
