# Architecture

## 1. Overview in One Minute

`externalcalls` is a durable queue for external system calls. It stores the intent to call an external system transactionally with the business record. It acts purely as a state machine: it queues calls, lists due calls, and records success or failure. The library also runs due background calls through a `Runner`. It does not execute the calls itself, sleep, or read a clock directly.

## 2. Repository Pieces & On-Disk Layout

| File / Directory | Purpose |
|---|---|
| `definition.go` | The schema definition for the `queued_call` table. |
| `definition_gen.go` | Code generated from the schema (structs, queries). |
| `externalcalls.go` | The main logic: `Queue`, states, and operations. |
| `run.go` | The executor `RunPendingCalls` and the `Runner` interface. |
| `migrate/migrate.go` | Tool to create or update the database table. |
| `tests/queue_test.go` | Unit tests for the queue logic. |
| `tests/run_test.go` | Unit tests for the executor. |
| `docs/diagrams/` | Mermaid diagrams illustrating flows and placement. |

## 3. Step-by-Step Flows

The core flow is documented in [CALL_LIFECYCLE.md](diagrams/CALL_LIFECYCLE.md).

*   **Happy Path:** A rule requests a call. It's added to the queue in a transaction (`Add`).
*   **Run the pending calls:** An executor calls `RunPendingCalls` which queries for due calls (`ListDue`), executes it through the runner, and marks it as done (`MarkDone`) or failed (`failAttempt`).
*   **Confirm a call:** A `WaitForConfirmation` call is answered by `ConfirmCall` or `RejectCall`.
*   **Retry Failure Path:** An executor fails to execute a call. It calls `MarkFailedAttempt`. If the mode allows retries (e.g., `RetryInBackground`) and the attempt limit is not reached, the call is re-queued with a delay.
*   **Terminal Failure Path:** If the attempt limit is reached, or the mode is `WaitForReply` or `WaitForConfirmation`, `MarkFailedAttempt` marks the call as permanently `failed`.

## 4. Security Boundaries & Contracts

*   **No Execution:** The library never executes external calls itself. This keeps it isolated from network concerns and specific external APIs.
*   **No `crud` Dependency:** It does not import `webtyp.com/crud`. It uses a generic `Creator` interface, allowing it to be used flexibly (though it is designed to work with `crud`'s transaction).
*   **Deterministic Limits:** Calls with modes `WaitForReply` or `WaitForConfirmation` are never retried in the background. Their declaration lacks an attempt limit, and hidden defaults are forbidden to ensure predictability.
*   **Call Mode:** the mode is stored in the row so the executor never lists calls it must not run.
*   **Idempotency Key:** the idempotency key is the call id.
