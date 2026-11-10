# Call Lifecycle

The life of one queued call; verified by `tests/queue_test.go` and `tests/run_test.go`

```mermaid
flowchart TD
    A[a rule asks for a call] --> B[Add: row queued<br/>in the record's transaction]
    B --> RunPendingCalls
    RunPendingCalls --> C[ListDue: queued and due<br/>RetryInBackground only]
    C --> Declaration[Declaration known?]
    Declaration -- yes --> D[Runner.Run]
    Declaration -- no --> I[failed]
    D -- answered --> E[MarkDone: done]
    D -- failed --> F[failAttempt]
    F --> G{mode of the call}
    G -- RetryInBackground<br/>attempts below MaxAttempts --> H[queued again<br/>next attempt after the backoff delay]
    G -- RetryInBackground<br/>attempts reached MaxAttempts --> I
    G -- WaitForReply or<br/>WaitForConfirmation --> I
    H --> C
    B --> WaitForConfirmation
    WaitForConfirmation --> Confirm[ConfirmCall]
    Confirm --> E
    WaitForConfirmation --> Reject[RejectCall]
    Reject --> I
```

1. A rule asks for a call.
2. The `Add` function queues the row in the record's transaction.
3. `RunPendingCalls` starts processing the queue.
4. The `ListDue` function queries for queued and due calls that are `RetryInBackground` only.
5. It checks if the declaration is known. If not, the call fails.
6. If known, the executor (`Runner.Run`) makes the call.
7. If the call is answered, `MarkDone` sets the state to done.
8. If the call fails, `failAttempt` is called.
9. The mode of the call determines the next step.
10. For `RetryInBackground` with attempts below `MaxAttempts`, it is queued again with a next attempt after the backoff delay, going back to `ListDue`.
11. For `RetryInBackground` with attempts reaching `MaxAttempts`, it fails.
12. For `WaitForReply` or `WaitForConfirmation`, it fails immediately on failure.
13. If the call is a `WaitForConfirmation`, it waits.
14. `ConfirmCall` marks the `WaitForConfirmation` call as done.
15. `RejectCall` marks the `WaitForConfirmation` call as failed.
