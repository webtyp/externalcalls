# externalcalls
<img src="docs/img/badges.svg">

A durable queue of calls to other systems.

## Why This Exists

A call to a payment provider made inside a request handler is lost if the server dies between the database save and the external call.

**Without this:** the record is saved, but the system is inconsistent because the call never happened.
**With this:** the call intent is saved in the same transaction as the record. If the server dies, the call is retried later.

See [CALL_LIFECYCLE.md](docs/diagrams/CALL_LIFECYCLE.md) for the flow. It uses [webtyp.com/backoff](https://pkg.go.dev/webtyp.com/backoff) for retry delays and [webtyp.com/model](https://pkg.go.dev/webtyp.com/model) for call definitions.

## Before You Start

Get the package:
```bash
go get webtyp.com/externalcalls@v0.1.0
```

You need:
1. A database with the table created using [migrate](migrate/migrate.go).
2. A retry policy from `backoff.New`.

## Step-by-Step Guide

### Step 1: Create the Queue
**Goal:** Initialize the queue with dependencies.

```go
package main

import (
	"webtyp.com/backoff"
	"webtyp.com/externalcalls"
	"webtyp.com/fmt"
	"webtyp.com/model"
	"webtyp.com/orm"
	"webtyp.com/storage/mem"
	"webtyp.com/time"
)

func main() {
	db := orm.New(mem.New())
	ids := model.NewIDGenerator()
	policy, err := backoff.New(time.Second, 60*time.Second)
	if err != nil {
		fmt.Println(err)
		return
	}
	queue, err := externalcalls.New(db, ids, time.NowNano, policy)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Queue created")
}
```
**Expected output:** `Queue created`

### Step 2: Add a Call
**Goal:** Add a call in a transaction.

```go
package main

import (
	"webtyp.com/externalcalls"
	"webtyp.com/fmt"
)

// creator implements externalcalls.Creator for this example
type creator struct{}
func (c creator) Create(rec interface{}) error { return nil }

func main() {
    // assuming queue is initialized
	var queue *externalcalls.Queue
	id, err := queue.Add(creator{}, externalcalls.Request{
		TenantId: "tenant-1",
		Module:   "payments",
		Entity:   "payment",
		RecordId: "pay-1",
		Call:     "charge",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Added call:", id)
}
```
**Expected output:** `Added call: <id>`

### Step 3: List Due Calls and Settle
**Goal:** Process a call.

```go
package main

import (
	"webtyp.com/externalcalls"
	"webtyp.com/fmt"
)

func main() {
    // assuming queue is initialized
	var queue *externalcalls.Queue
	due, err := queue.ListDue(10)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, call := range due {
		err := queue.MarkDone(call.Id)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("Done:", call.Id)
	}
}
```
**Expected output:** `Done: <id>`

## Documentation Map

| Order | Document | What question it answers & when to read it |
|---|---|---|
| 1 | [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | How the system is designed, its flows, and boundaries. Read before modifying code. |
| 2 | [docs/diagrams/CALL_LIFECYCLE.md](docs/diagrams/CALL_LIFECYCLE.md) | The state machine of a call. Read to understand retries and failures. |
| 3 | [docs/diagrams/PLACEMENT.md](docs/diagrams/PLACEMENT.md) | Where this library fits in the architecture. Read to understand dependencies. |
| 4 | [docs/TESTS.md](docs/TESTS.md) | The rules verified by the test suite. Read when adding new features or fixing bugs. |
