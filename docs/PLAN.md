---
PLAN: "refactor: backoff v0.2.0 (time.Duration delays)"
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 18185206359011912867
PR: https://github.com/webtyp/externalcalls/pull/2
REVIEWED_COMMIT: 4359657e71d25265062d5cb5f38e19547cab2ebf
---

> Read AGENTS.md at the repo root before any change.
> **Finish by yourself:** when the delivery check passes, commit, push and open the pull request. Never ask whether to
> proceed or finalize — nobody answers, and a session that waits ends without your work. Commit after every stage.
> Never run `git reset`, `git restore`, `git checkout -- <file>` or `git stash` on your own work.
> **Publishing:** you publish only by OPENING THE PULL REQUEST (a `git push` from your sandbox does not reach GitHub, and work not submitted is lost when the session ends).
> **Never ask a yes/no question about progress**; the answer is always yes. Ask only when a signature in this plan is wrong, and name the symbol.
> Never write `go.mod`/`go.sum` lines by hand: only `go get` and `go mod tidy` do. No scratch files and no scripts over the source.

# Plan: `externalcalls` on `backoff` v0.2.0

## Why (decided: P47 B of the architecture master)

`webtyp.com/time` v0.6.5 has a type for a wait, `time.Duration` (nanoseconds, never an instant), with `time.Nanosecond`, `time.Microsecond`, `time.Millisecond`, `time.Second`, `(Duration).Milliseconds() int64` and `(Nano).Add(Duration) Nano`. `webtyp.com/backoff` v0.2.0 takes and answers `time.Duration` instead of `time.Nano` (an instant): `New(first, longest time.Duration) (Policy, error)` and `(Policy).DelayAfter(failures int64) (time.Duration, error)`. This repo called the v0.1.0 API with `time.Nano`; this plan moves it to the new one. Behaviour does not change.

## Tools

```bash
go install webtyp.com/devflow/cmd/gotest@latest
go get webtyp.com/backoff@v0.2.0 webtyp.com/time@v0.6.5
go mod tidy
```
`GOPRIVATE=webtyp.com/*,github.com/veltylabs/*`, `GOFLAGS=-mod=mod`; no `replace` in `go.mod`. If a module cannot be fetched, stop and say so in the PR; never invent a hash. If a signature in the published tags differs from this plan, stop and name the symbol in the PR; never adapt it by guessing.

## Stage 1 — `externalcalls.go`

The line that schedules the next attempt in `MarkFailedAttempt` (today `externalcalls.go:167`): `row.NextAttemptAt = now + delay` becomes `row.NextAttemptAt = now.Add(delay)` (`now` is the `time.Nano` read from `q.now()` two lines above; `delay` is now a `time.Duration`). `New(db, ids, now func() time.Nano, retry backoff.Policy)` keeps its signature: `backoff.Policy` is the same type.

## Stage 2 — tests (`tests/queue_test.go`)

1. Line 14: `const second time.Nano = 1000000000` becomes `const second = time.Second` (a `time.Duration`).
2. Line 233: `row.NextAttemptAt != 1000+second` becomes `row.NextAttemptAt != time.Nano(1000).Add(second)`. Every other expectation of the same kind (`grep -n "+second\|+ second\|\*second" tests/queue_test.go`) is rewritten with `.Add(...)` on a `time.Nano`, never by casting a `Duration` to a `Nano`.
3. `backoff.New(second, 60*second)` (lines 38 and 134) stays as written: now both arguments are `time.Duration`.

## Stage 3 — docs

`README.md:47` `backoff.New(1000000000, 60000000000)` becomes `backoff.New(time.Second, 60*time.Second)` and the example imports `"webtyp.com/time"`; the same in any other doc (`grep -rn "backoff.New(" README.md docs`). `docs/diagrams/PLACEMENT.md` already says the package imports `time`; add that it now also uses `time.Duration` for the retry delay.

`gotest` must end green on every destination it runs; a tree that does not compile or whose tests did not run is a failed delivery.
`git diff --stat main` lists `externalcalls.go`, `tests/queue_test.go`, `README.md`, `docs/diagrams/PLACEMENT.md`, `go.mod`, `go.sum`; nothing is deleted.
