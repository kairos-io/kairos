# Rolling Batches Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run a command over many nodes a few at a time, stop on failure, detect nodes that do not come back from a reboot, and keep a per-batch record that the API and a new Batches UI show.

**Architecture:** A batch is a `command_batches` row plus one `command_batch_nodes` row per target node. A node's command row is created only when its slot opens, so no delivery path needs to change. One store method, `BatchAdvance`, does all the bookkeeping for a batch inside one transaction that locks the batch row: it reconciles node rows with their commands, times out nodes, stops the batch on failure, fills free slots with online nodes, skips offline ones and finishes the batch. A small `pkg/batches` scheduler calls it after every relevant event and on a ticker, and pushes the commands it created.

**Tech Stack:** Go, GORM (SQLite and PostgreSQL), Echo, Ginkgo v2 + Gomega, React + TypeScript, vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-10-22-rolling-batches-design.md`

## Global Constraints

- Defaults: concurrency `0` (all nodes at once), stop on failure `true`, reboot timeout `600` seconds.
- Commands that AuroraBoot reboots: `upgrade`, `reset`, `reboot`. Only these enter `rebooting` and get a deadline.
- Node row states, exactly: `waiting`, `running`, `rebooting`, `done`, `failed`, `timed_out`, `skipped`, `canceled`. Final: `done`, `failed`, `timed_out`, `skipped`, `canceled`.
- Batch phases, exactly: `running`, `completed`, `failed`, `stopped`.
- Messages, exactly: `fell back to passive`, `fell back to recovery`, `did not come back within <N> minutes` (N = timeout in minutes, rounded up), `offline when the batch reached it`, `batch stopped`, `stopped by the user` (batch stop reason), `node deleted`, `command deleted`, `command expired`, `command canceled`. A failed command's message is its result text.
- Table names: `command_batches`, `command_batch_nodes`.
- A node comes back when a heartbeat's boot time (now minus `metrics.uptimeSeconds`, via `store.BootedAt`) is after the row's `RebootingSince` plus `5 * time.Second`, or when it registers. Heartbeats without an uptime settle nothing.
- Go tests use Ginkgo v2 + Gomega, matching the surrounding suite. No `func TestXxx` with `t.Fatalf`.
- Code comments say why the current code is what it is. No history ("used to", "no longer", "replaces").
- No compatibility code for batches created before this change. `NodeCommand.FailFast`, `store.CancelBatchAfterFailure`, `CancelPendingInBatch`, `SummarizeBatch`, `BatchOutcome`, `ListByBatch` and `GET /api/v1/commands/batches/:batchID` are removed.
- Commits: `git commit --no-gpg-sign`, message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never push.
- Never `git add` the untracked `auroraboot-data/` directory in the repo root.
- Go test command for a package: `go run github.com/onsi/ginkgo/v2/ginkgo -r <dir>`. PostgreSQL specs run when `AURORABOOT_TEST_POSTGRES_DSN` is set and skip otherwise.
- UI checks: `cd ui && npm test -- <file>`, `npm run lint`, `npx tsc -b`.

## Review Focus

1. **AuroraBoot restarts while nodes are rebooting.** A node cannot report back while the server is down, so a deadline that passed during the downtime must not time the node out before it had a chance to re-register. Expected: on scheduler start, every `rebooting` deadline that has passed is moved to now plus that batch's timeout. Test in Task 4.
2. **A selection that matches no nodes.** Expected: `400` with `{"error": "no nodes match the selection"}`, and no batch is created. Test in Task 5.
3. **The same node listed twice in `nodeIDs`.** Expected: one node row, one command. Test in Task 4.
4. **Command history cleared while a node is `rebooting`.** `ClearHistory` deletes Completed commands, including the one a `rebooting` row points to. Expected: the row still settles from the node's registration or its deadline, and is not canceled as "command deleted". Test in Task 3.
5. **Stopping a batch that is not running.** Expected: `409` with `{"error": "batch is not running"}`, nothing changes. Test in Task 6.

---

## File Structure

- `pkg/store/batch.go` (rewrite): batch types, states, `BatchStore` interface.
- `internal/store/gorm/batches.go` (create): the GORM batch store, including `BatchAdvance`.
- `internal/store/gorm/store.go` (modify): register the new tables, extract `createCommands`, remove the fail-fast store code.
- `internal/store/gorm/adapters.go` (modify): `BatchStoreAdapter`.
- `pkg/batches/scheduler.go` (create): `Scheduler`.
- `pkg/handlers/commands.go` (modify): batch endpoints start batches, status hooks, single-node 409.
- `pkg/handlers/batches.go` (create): list, get, stop.
- `pkg/handlers/nodes.go`, `pkg/ws/handler.go`, `pkg/server/server.go`, `internal/cmd/web.go` (modify): hooks and wiring.
- `pkg/client/batches.go` (create), `pkg/client/types.go`, `pkg/client/commands.go`, `pkg/client/groups.go` (modify).
- `ui/src/api/batches.ts` (create), `ui/src/api/groups.ts`, `ui/src/api/nodes.ts` (modify).
- `ui/src/components/CommandDialog.tsx` (modify): Rollout section.
- `ui/src/pages/Batches.tsx`, `ui/src/pages/BatchDetail.tsx` (create); `ui/src/App.tsx`, `ui/src/components/Layout.tsx`, `ui/src/pages/GroupDetail.tsx`, `ui/src/pages/Nodes.tsx`, `ui/src/pages/NodeDetail.tsx` (modify).
- `docs/` swagger files: regenerated.

---

### Task 1: Batch types, tables and the read side of the store

**Files:**
- Rewrite: `pkg/store/batch.go`
- Create: `internal/store/gorm/batches.go`, `internal/store/gorm/batches_test.go`
- Modify: `internal/store/gorm/store.go` (`AutoMigrate` list), `internal/store/gorm/adapters.go`

Keep `BatchOutcome`, `SummarizeBatch` and `CancelBatchAfterFailure` (and `pkg/store/batch_test.go`) in this task, moved to the bottom of the rewritten `pkg/store/batch.go`; Task 5 removes them with their callers. The build must pass at the end of every task.

**Interfaces:**
- Produces, in `pkg/store`:

```go
const (
    BatchRunning   = "running"
    BatchCompleted = "completed"
    BatchFailed    = "failed"
    BatchStopped   = "stopped"

    BatchNodeWaiting   = "waiting"
    BatchNodeRunning   = "running"
    BatchNodeRebooting = "rebooting"
    BatchNodeDone      = "done"
    BatchNodeFailed    = "failed"
    BatchNodeTimedOut  = "timed_out"
    BatchNodeSkipped   = "skipped"
    BatchNodeCanceled  = "canceled"

    DefaultRebootTimeoutSeconds = 600
)

func IsFinalBatchNodeState(state string) bool
// RebootsNode reports whether AuroraBoot reboots the node for command: upgrade, reset, reboot.
func RebootsNode(command string) bool

type CommandBatch struct {
    ID                   string            `json:"id" gorm:"primaryKey"`
    Command              string            `json:"command"`
    Args                 map[string]string `json:"args" gorm:"serializer:json"`
    Selection            string            `json:"selection"`
    GroupID              *string           `json:"groupID" gorm:"index" extensions:"x-nullable"`
    Concurrency          int               `json:"concurrency"`
    StopOnFailure        bool              `json:"stopOnFailure"`
    RebootTimeoutSeconds int               `json:"rebootTimeoutSeconds"`
    Phase                string            `json:"phase" gorm:"index"`
    CreatedAt            time.Time         `json:"createdAt"`
    FinishedAt           *time.Time        `json:"finishedAt"`
    StoppedAt            *time.Time        `json:"stoppedAt"`
    StopReason           string            `json:"stopReason"`
    StoppedByNodeID      *string           `json:"stoppedByNodeID" extensions:"x-nullable"`
    // Counts maps each node state to how many nodes are in it.
    Counts map[string]int `json:"counts" gorm:"-"`
    // Nodes is filled by BatchGet only.
    Nodes []*BatchNode `json:"nodes,omitempty" gorm:"foreignKey:BatchID"`
}

type BatchNode struct {
    BatchID    string     `json:"batchID" gorm:"primaryKey"`
    NodeID     string     `json:"nodeID" gorm:"primaryKey;index"`
    Position   int        `json:"position"`
    Hostname   string     `json:"hostname"`
    CommandID  *string    `json:"commandID" gorm:"index" extensions:"x-nullable"`
    State      string     `json:"state" gorm:"index"`
    Message    string     `json:"message"`
    StartedAt  *time.Time `json:"startedAt"`
    // RebootingSince is when the reboot the node is waiting on was
    // scheduled: its command's completion, then its autoreset boot.
    RebootingSince *time.Time `json:"rebootingSince"`
    Deadline   *time.Time `json:"deadline"`
    FinishedAt *time.Time `json:"finishedAt"`
}
func (BatchNode) TableName() string { return "command_batch_nodes" }

type BatchFilter struct {
    GroupID string
    NodeID  string
    Limit   int // 0 means 50
    Offset  int
}

var ErrBatchNotFound = errors.New("batch not found")
var ErrBatchNotRunning = errors.New("batch is not running")

type BatchStore interface {
    // Create writes the batch and its node rows in one transaction. Every row
    // must be waiting; Phase is set to running.
    Create(ctx context.Context, b *CommandBatch, nodes []*BatchNode) error
    // Get returns the batch with Nodes ordered by Position and Counts filled.
    Get(ctx context.Context, id string) (*CommandBatch, error)
    // List returns batches newest first with Counts filled and Nodes empty,
    // and the total number matching the filter.
    List(ctx context.Context, f BatchFilter) ([]*CommandBatch, int, error)
    // BusyNodes returns the IDs among nodeIDs that have a row in a non-final state.
    BusyNodes(ctx context.Context, nodeIDs []string) ([]string, error)
    // Advance, Stop, NodeBooted, ActiveIDs, PostponeExpiredDeadlines: Tasks 2-3.
}
```

- Produces, in `internal/store/gorm`: `(*Store).BatchCreate`, `BatchGet`, `BatchList`, `BatchBusyNodes`, and `type BatchStoreAdapter struct{ S *Store }` implementing `store.BatchStore` by delegating (same pattern as `CommandStoreAdapter`).

- [ ] **Step 1: Write the failing specs** in `internal/store/gorm/batches_test.go`, `Describe("Gorm batch store")`, on a SQLite store in a temp dir (as `node_upgrade_test.go` does):
  - `It("stores a batch and returns its nodes in start order with counts")`: create a batch with 3 waiting rows at positions 2, 0, 1; `Get` returns Phase `running`, `Nodes` ordered by position, `Counts` equal to `map[string]int{"waiting": 3}`.
  - `It("returns ErrBatchNotFound for an unknown batch")`.
  - `It("lists batches newest first and filters them by group and by node")`: three batches, one with `GroupID` g1, one containing node n1; `List(BatchFilter{GroupID: "g1"})` returns that one with total 1; `List(BatchFilter{NodeID: "n1"})` likewise; `List(BatchFilter{Limit: 2})` returns two and total 3.
  - `It("reports nodes with a row that is not final as busy")`: rows in `waiting` (n1), `rebooting` (n2), `done` (n3) → `BusyNodes([n1,n2,n3,n4])` equals `[n1, n2]` in any order.
- [ ] **Step 2: Run** `go run github.com/onsi/ginkgo/v2/ginkgo -r ./internal/store/gorm` and see the new specs fail to compile.
- [ ] **Step 3: Implement** the types in `pkg/store/batch.go`, add `&store.CommandBatch{}, &store.BatchNode{}` to `AutoMigrate`, and the four methods plus the adapter. `List` filtered by node uses `id IN (SELECT batch_id FROM command_batch_nodes WHERE node_id = ?)`. Counts come from one `GROUP BY batch_id, state` query over the returned batch IDs.
- [ ] **Step 4: Run** the same command. Expected: all specs pass.
- [ ] **Step 5: Commit** `feat(store): add batch and batch node records`.

---

### Task 2: `BatchAdvance`: reconcile, fill slots, skip offline nodes, finish

**Files:**
- Modify: `internal/store/gorm/batches.go`, `internal/store/gorm/store.go` (extract `createCommands`), `internal/store/gorm/adapters.go`, `pkg/store/batch.go`
- Test: `internal/store/gorm/batches_advance_test.go`, `internal/store/gorm/postgres_batches_test.go`

**Interfaces:**
- Consumes: Task 1 types.
- Produces:

```go
// in BatchStore
// Advance brings one batch up to date at now and returns the commands it
// created, for the caller to push after the transaction committed.
Advance(ctx context.Context, batchID string, now time.Time) ([]*NodeCommand, error)
```

- `func createCommands(tx *gorm.DB, cmds []*store.NodeCommand) error` in `store.go`: the body of today's `CreateBatch` transaction, so `CreateBatch` becomes `s.db.Transaction(func(tx) error { return createCommands(tx, cmds) })`. Behaviour of `CreateBatch` does not change.

`Advance` runs in one transaction, in this order:

1. Lock the batch row: `clause.Locking{Strength: "UPDATE"}` on PostgreSQL, nothing on SQLite (follow `lockNodes`). Unknown batch → `ErrBatchNotFound`.
2. Rows whose node no longer exists and are not final → `canceled`, `node deleted`.
3. Each `running` row, against its command:
   - command missing → `canceled`, `command deleted`
   - `Failed` → `failed`, message = command result
   - `Expired` → `canceled`, `command expired`; `Canceled` → `canceled`, `command canceled`
   - `Completed` and `store.RebootsNode(cmd)` → `rebooting`, `RebootingSince = command.CompletedAt`, `Deadline = command.CompletedAt + timeout`
   - `Completed` otherwise → `done`
   - anything else → unchanged
   `rebooting` rows are not reconciled against their command here.
4. (Task 3 adds timeouts and stop-on-failure at this point.)
5. If the batch phase is `running`: let `holding` = rows in `running` or `rebooting`; `free` = all waiting rows when `Concurrency == 0`, else `Concurrency - holding`. Take up to `free` `waiting` rows in `Position` order whose node's `phase` is `store.PhaseOnline`. For each, build a `NodeCommand{ID: uuid, ManagedNodeID, Command, Args, Phase: Pending, BatchID}`; create them all with `createCommands(tx, cmds)`; set those rows `running`, `CommandID`, `StartedAt = now`.
6. If the batch is `running`, no row is `running` or `rebooting` after step 5, and `waiting` rows remain (all offline): they become `skipped`, `offline when the batch reached it`.
7. If every row is final and the phase is `running`: phase `failed` if any row is `failed` or `timed_out`, else `completed`. `FinishedAt = now` once all rows are final, whatever the phase.

Every row that becomes final gets `FinishedAt = now`.

- [ ] **Step 1: Write the failing specs** in `batches_advance_test.go`. Helpers in the file: `newBatch(nodes []*store.ManagedNode, command string, concurrency int)` creating the batch through `BatchCreate`, and `online(n)`/`offline(n)` setting the node phase with `UpdatePhase`.
  - `It("starts as many online nodes as the concurrency allows, in order")`: 3 online nodes, concurrency 2 → `Advance` returns 2 commands for positions 0 and 1; their rows are `running` with `CommandID` set; the third is `waiting`; `GetPending` for node 0 returns its command.
  - `It("starts every node when the concurrency is 0")`.
  - `It("starts the next node when a running node's command completes")`: concurrency 1, `exec`; complete the first command with `UpdateStatus(..., Completed, "")`; `Advance` → first row `done`, second `running`.
  - `It("moves a node whose upgrade completed to rebooting with its deadline")`: completed at T, timeout 600 → row `rebooting`, `RebootingSince` = T, `Deadline` = T + 10 min, and no new node is started while it reboots (concurrency 1).
  - `It("marks a node failed with its command's result")`: result `"exit status 1"` → `failed`, message `exit status 1`.
  - `It("cancels a running node whose command was deleted")` → `canceled`, `command deleted`.
  - `It("cancels the rows of a node that was deleted")` → `canceled`, `node deleted`.
  - `It("passes over an offline node and starts the next online one")`: concurrency 1, node 0 offline, node 1 online → node 1 `running`, node 0 still `waiting`.
  - `It("skips the waiting nodes once only offline nodes are left")`: concurrency 1, node 0 online and done, node 1 offline → node 1 `skipped`, `offline when the batch reached it`; batch `completed`.
  - `It("completes the batch when every node is done")` → phase `completed`, `FinishedAt` set.
  - `It("marks the upgrade lifecycle of a node when its command is created")`: after `Advance` starts an upgrade node, `NodeGetByID` shows `UpgradeState` `pending`.
- [ ] **Step 2: Write the failing PostgreSQL spec** in `postgres_batches_test.go` (uses `newPostgresStore`, skips without the DSN): `It("never starts more nodes than the concurrency when two advances run at once")`: 6 online nodes, concurrency 2, call `Advance` from two goroutines at once (use the `rendezvous` helper of `postgres_lifecycle_test.go` on the `command_batch_nodes` query callback), then count rows in `running`: exactly 2.
- [ ] **Step 3: Run** `go run github.com/onsi/ginkgo/v2/ginkgo -r ./internal/store/gorm`. Expected: compile failure on `BatchAdvance`.
- [ ] **Step 4: Implement** `createCommands`, `(*Store).BatchAdvance` and the adapter method.
- [ ] **Step 5: Run** the same command, then once with `AURORABOOT_TEST_POSTGRES_DSN` set if a server is available (`docker run --rm -d -e POSTGRES_PASSWORD=pg -p 5432:5432 postgres:17-alpine`, DSN `postgres://postgres:pg@localhost:5432/postgres?sslmode=disable`). Expected: all pass.
- [ ] **Step 6: Commit** `feat(store): advance a batch one slot at a time`.

---

### Task 3: Timeouts, stop on failure, stop by hand, registrations

**Files:**
- Modify: `internal/store/gorm/batches.go`, `internal/store/gorm/adapters.go`, `pkg/store/batch.go`
- Test: `internal/store/gorm/batches_settle_test.go`

**Interfaces:**
- Produces, in `BatchStore`:

```go
// Stop stops a running batch: phase stopped, StoppedAt now, StopReason
// reason, StoppedByNodeID nil, and every waiting row canceled with
// "batch stopped". ErrBatchNotRunning when the phase is not running.
Stop(ctx context.Context, batchID, reason string, now time.Time) error
// NodeBooted settles the node's rebooting rows when it came back: bootedAt
// is the boot time a heartbeat reports, or nil for a registration, which
// always follows a boot. It returns the IDs of the batches it touched.
NodeBooted(ctx context.Context, nodeID, bootState string, bootedAt *time.Time, now time.Time) ([]string, error)
// ActiveIDs returns the batches that are running or still have a running or
// rebooting row.
ActiveIDs(ctx context.Context) ([]string, error)
// PostponeExpiredDeadlines moves every rebooting deadline before now to now
// plus its batch's timeout.
PostponeExpiredDeadlines(ctx context.Context, now time.Time) error
```

On `*Store` these are `BatchStop`, `BatchNodeBooted`, `BatchActiveIDs` and `BatchPostponeExpiredDeadlines`, with adapter methods named as in the interface.

Additions to `Advance`, at step 4 of Task 2:

- Each `rebooting` row with `Deadline < now` → `timed_out`, message `did not come back within <N> minutes`. In the same transaction the node's lifecycle is marked failed: upgrade `upgrade_state = 'failed'` where it is `pending`; reset `reset_state = 'failed'` where it is `pending` or `in-progress`. `reboot` has no lifecycle.
- If any row became `failed` or `timed_out` in this call (from step 3 or the timeout), the batch is `running` and `StopOnFailure` is true: phase `stopped`, `StoppedAt = now`, `StoppedByNodeID` = the first such row's node (lowest `Position`), `StopReason` = `<hostname>: <message>`, and every `waiting` row → `canceled`, `batch stopped`.

`NodeBooted` rules, applied to each `rebooting` row of the node whose `RebootingSince` the boot is after (`bootedAt == nil`, or `bootedAt` after `RebootingSince + 5s`), under the same batch row lock. Other rows, and rows the boot is not after, are not touched. A call with no `rebooting` row for the node returns at once without a write, since every heartbeat makes one:

| command | boot state | result |
|---|---|---|
| upgrade | `active` | `done` |
| upgrade, reset | `passive` / `recovery` | `failed`, `fell back to passive` / `fell back to recovery` |
| reset | `autoreset` | stays `rebooting`, `RebootingSince = bootedAt` (or `now` for a registration), `Deadline = now + timeout` |
| reset | `active` | `done` |
| reboot | any | `done` |
| upgrade, reset | any other | unchanged |

Rows in `waiting` or `running` are not touched. A row that `NodeBooted` fails triggers stop-on-failure inside `NodeBooted` itself, with the same rule as `Advance`: both call one helper, `stopOnFailure(tx *gorm.DB, b *store.CommandBatch, failed []*store.BatchNode, now time.Time) error`.

- [ ] **Step 1: Write the failing specs** in `batches_settle_test.go`:
  - `It("times out a rebooting node past its deadline and fails its upgrade")`: deadline passed → `timed_out`, `did not come back within 10 minutes`, node `UpgradeState` `failed`.
  - `It("leaves a rebooting node alone before its deadline")`.
  - `It("stops the batch at the first failure and cancels the waiting nodes")`: concurrency 1, 3 nodes; fail node 0's command → phase `stopped`, `StoppedByNodeID` node 0, `StopReason` `host-0: exit status 1`, nodes 1 and 2 `canceled`, `batch stopped`, no command for them.
  - `It("keeps going after a failure when stop on failure is off")` → node 1 starts; when all final, phase `failed`.
  - `It("lets running nodes finish after the batch stopped")`: concurrency 2, node 0 fails while node 1 runs → batch `stopped`; then node 1 completes and `Advance` sets it `done`; phase stays `stopped`, `FinishedAt` set.
  - `It("stops a running batch by hand")`: `Stop(..., "stopped by the user", now)` → phase `stopped`, `StoppedByNodeID` nil, waiting rows canceled.
  - `It("refuses to stop a batch that is not running")` → `ErrBatchNotRunning`.
  - `DescribeTable("settles a rebooting node from the boot state it comes back with", ...)` with one `Entry` per row of the table above, each called with a `bootedAt` one minute after `RebootingSince`, asserting state and message.
  - `It("settles a rebooting node that registers again")`: `bootedAt` nil, `active` → `done`.
  - `It("ignores a heartbeat from the boot before the reboot")`: `bootedAt` one hour before `RebootingSince` → still `rebooting`.
  - `It("ignores the jitter of a whole-second uptime")`: `bootedAt` 2 seconds after `RebootingSince` → still `rebooting`.
  - `It("does not settle a node that comes back before its command completed")`: row `running` → `NodeBooted(..., "active", <now>, ...)` leaves it `running`.
  - `It("follows a reset through its autoreset boot")`: `autoreset` moves `RebootingSince` to that boot and restarts the deadline; a second heartbeat from the same boot changes nothing; a later `active` boot → `done`.
  - `It("stops the batch when a node comes back on a fallback")`.
  - `It("still settles a rebooting node whose completed command was cleared from the history")` (Review Focus 4): row `rebooting`, then `CommandDeleteTerminal(node)`, then `Advance` → still `rebooting`; then `NodeBooted(..., "active", <a minute after RebootingSince>, ...)` → `done`.
  - `It("postpones deadlines that passed while the server was down")`: deadline in the past, `PostponeExpiredDeadlines(now)` → deadline `now + 10 min`.
  - `It("lists running batches and stopped batches with nodes still running as active")`.
- [ ] **Step 2: Run** `go run github.com/onsi/ginkgo/v2/ginkgo -r ./internal/store/gorm`. Expected: compile failure.
- [ ] **Step 3: Implement** the four methods, the timeout and stop-on-failure steps of `Advance`, and the adapter methods.
- [ ] **Step 4: Run** the same command. Expected: all pass.
- [ ] **Step 5: Commit** `feat(store): stop batches on failure and settle nodes that reboot`.

---

### Task 4: The scheduler

**Files:**
- Create: `pkg/batches/scheduler.go`, `pkg/batches/suite_test.go`, `pkg/batches/scheduler_test.go`

**Interfaces:**
- Consumes: `store.BatchStore` (Tasks 1-3).
- Produces:

```go
package batches

type Deliver func(ctx context.Context, cmd *store.NodeCommand)

type StartRequest struct {
    Command              string
    Args                 map[string]string
    Nodes                []*store.ManagedNode
    Selection            string
    GroupID              *string
    Concurrency          int  // >= 0
    StopOnFailure        bool
    RebootTimeoutSeconds int  // > 0; callers apply the default
}

// ErrNodesBusy lists nodes that are already in a batch that has not finished with them.
type ErrNodesBusy struct{ NodeIDs []string }
func (e *ErrNodesBusy) Error() string

var ErrNoNodes = errors.New("no nodes match the selection")

type Scheduler struct{ /* store, deliver, now func() time.Time */ }

func New(bs store.BatchStore, deliver Deliver) *Scheduler
// Start creates the batch and advances it once. Nodes listed twice get one row.
func (s *Scheduler) Start(ctx context.Context, req StartRequest) (*store.CommandBatch, error)
// Advance advances one batch and delivers the commands it created. Errors are logged.
func (s *Scheduler) Advance(ctx context.Context, batchID string)
func (s *Scheduler) NodeBooted(ctx context.Context, nodeID, bootState string, bootedAt *time.Time)
func (s *Scheduler) Stop(ctx context.Context, batchID string) error // reason "stopped by the user"
// Tick advances every active batch.
func (s *Scheduler) Tick(ctx context.Context)
// Run postpones expired deadlines once, then calls Tick every interval until ctx is done.
func (s *Scheduler) Run(ctx context.Context, interval time.Duration)
// SetClock replaces time.Now, for tests.
func (s *Scheduler) SetClock(now func() time.Time)
```

`Start` returns the batch as `BatchStore.Get` returns it after the first advance. Busy nodes are checked with `BusyNodes` before creating anything.

- [ ] **Step 1: Write the failing specs** with a real SQLite store (`gormstore.New` in a temp dir) and a `Deliver` that records commands:
  - `It("creates the batch and delivers the first slot's commands")`.
  - `It("gives a node listed twice one row")` (Review Focus 3).
  - `It("refuses to start when a node is in a batch that has not finished with it")` → `*ErrNodesBusy` naming that node, no batch created.
  - `It("returns ErrNoNodes for an empty node list")`.
  - `It("delivers the next node's command after the previous one completed")` via `Advance`.
  - `It("advances every active batch on a tick")`.
  - `It("times out a rebooting node on a tick after its deadline")`, using `SetClock`.
  - `It("postpones expired deadlines when it starts running")`: a row with a past deadline; `Run` with a cancelled context after the first iteration; the row is still `rebooting` and its deadline moved (Review Focus 1).
- [ ] **Step 2: Run** `go run github.com/onsi/ginkgo/v2/ginkgo -r ./pkg/batches`. Expected: compile failure.
- [ ] **Step 3: Implement** `pkg/batches/scheduler.go`.
- [ ] **Step 4: Run** the same command. Expected: pass.
- [ ] **Step 5: Commit** `feat(batches): schedule rolling batches`.

---

### Task 5: Start batches from the API and feed the scheduler

**Files:**
- Modify: `pkg/handlers/commands.go`, `pkg/handlers/nodes.go`, `pkg/ws/handler.go`, `pkg/server/server.go`, `internal/cmd/web.go`, `internal/store/gorm/store.go`, `internal/store/gorm/adapters.go`, `pkg/store/store.go`, `pkg/store/batch.go`
- Delete: `pkg/store/batch_test.go`, `pkg/handlers/commands_batch_test.go`, `internal/store/gorm/command_batch_test.go` (keep any spec in it that covers `CreateBatch` alone by moving it to `store_test.go`), the fail-fast specs in `pkg/ws/handler_test.go`, `pkg/server/server_test.go`, `pkg/client/groups_test.go`, `pkg/auth/artifact_download_test.go`, and the "batch is canceled" spec in `postgres_lifecycle_test.go`
- Test: `test/integration/batches_test.go`

**Interfaces:**
- Consumes: `batches.Scheduler` (Task 4).
- Produces:
  - `CommandHandler` gets `func (h *CommandHandler) WithBatches(s *batches.Scheduler) *CommandHandler` and exports `func (h *CommandHandler) PushCommand(ctx context.Context, cmd *store.NodeCommand)` (today's `pushCommand`).
  - Request body of `POST /api/v1/nodes/commands` and `POST /api/v1/groups/:id/commands`, replacing `failFast`:

```go
Concurrency          int   `json:"concurrency,omitempty"`
StopOnFailure        *bool `json:"stopOnFailure,omitempty"`        // nil means true
RebootTimeoutSeconds int   `json:"rebootTimeoutSeconds,omitempty"` // 0 means 600
```

  - Both return `201` with the `store.CommandBatch` from `Scheduler.Start`.
  - `Selection` text: group endpoint `group <group name>`; bulk endpoint with `groupID` the same, with `labels` `label k=v` (keys sorted, joined with `, `), with `nodeIDs` `<n> selected nodes` (`1 selected node` for one). When several selector fields are set, join the parts with `, `.
  - `NodeHandler` gets `func (h *NodeHandler) WithBatches(s *batches.Scheduler) *NodeHandler`; `AgentHandler` gets a field `OnCommandPhase func(ctx context.Context, commandID string)`.
- Errors: concurrency `< 0` or timeout `< 0` → 400 `{"error": "concurrency and rebootTimeoutSeconds must not be negative"}`; empty selection → 400 `{"error": "no nodes match the selection"}`; busy nodes → 409 `{"error": "nodes are already in a running batch", "nodeIDs": [...]}`.

Hooks:
- After every successful command status write (agent REST, admin REST, WebSocket `command_status`), when the command has a `BatchID`: `Advance(ctx, cmd.BatchID)`. The WebSocket path calls `OnCommandPhase`, which the server wires to a function that reads the command and advances its batch.
- `NodeHandler.Register` on an existing node calls `NodeBooted(ctx, node.ID, req.BootState, nil)` after `store.ResolveReset`/`store.ResolveUpgrade`.
- The REST heartbeat (`NodeHandler.Heartbeat`) and the WebSocket heartbeat (`AgentHandler.handleHeartbeat`) call `NodeBooted(ctx, nodeID, bootState, &bootedAt)` when `store.BootedAt` finds an uptime, next to the existing `SettleAfterReboot` call. The WebSocket handler gets it through a field `OnNodeBooted func(ctx context.Context, nodeID, bootState string, bootedAt *time.Time)` wired in `server.go`.
- `NodeHandler.Delete` and `CommandHandler.Delete` call `Tick` after the delete.
- `CommandHandler.Create` (single node): for `upgrade`, `reset`, `reboot`, `BusyNodes([nodeID])` non-empty → 409 `{"error": "node is in a running batch"}`.
- `internal/cmd/web.go`: build the scheduler with `&gormstore.BatchStoreAdapter{S: store}` and `cmdHandler.PushCommand`; start `go sched.Run(ctx, 30*time.Second)` with the server's lifecycle context. Thread the scheduler and the `BatchStore` through `server.Config` (`BatchStore store.BatchStore`, `Batches *batches.Scheduler`) so `server.New` wires the handlers; when the scheduler is nil the batch endpoints answer 503.

Removals in this task: `fanOut`, `cancelBatchOnFailure`, `BatchStatus` and its route, `NodeCommand.FailFast`, `CancelPendingInBatch` (store, adapter, interface), `ListByBatch` (store, adapter, interface), anything left of `BatchOutcome`/`SummarizeBatch`/`CancelBatchAfterFailure`.

- [ ] **Step 1: Write the failing integration specs** in `test/integration/batches_test.go`, `Describe("Rolling batches")`. Nodes are registered with `registerNode` and brought online with `connectWS` (keep the connection open in the spec, read pushed frames from it):
  - `It("delivers only as many commands as the concurrency allows")`: 3 online nodes in a group, `POST /groups/:id/commands` `{"command": "exec", "args": {"cmd": "true"}, "concurrency": 1}` → 201, response `phase` `running`, `counts` `{"running": 1, "waiting": 2}`; only the first node's socket receives a command frame.
  - `It("starts the next node when the previous one reports completion over the WebSocket")`.
  - `It("stops the batch when a node reports failure")`: concurrency 1, node 0 reports `Failed` over the WebSocket; node 1's socket receives no command within 1 second, and `GET /api/v1/nodes/:node1/commands` lists none. (The batch endpoints that show the stop reason come in Task 6.)
  - `It("rejects a selection that matches no nodes")` (Review Focus 2): `POST /nodes/commands` with `{"selector": {"labels": {"none": "x"}}, "command": "reboot"}` → 400, `no nodes match the selection`.
  - `It("rejects negative settings")` → 400.
  - `It("rejects a single-node upgrade for a node in a running batch")` → 409.
  - `It("starts the next node when a rebooted node heartbeats from its new boot")`: `reboot` batch, concurrency 1, two nodes; node 0 reports Completed, then sends a WebSocket heartbeat with `bootState` `active` and `metrics.uptimeSeconds` 0; node 1's socket receives its command.
  - `It("does not start the next node on a heartbeat from before the reboot")`: as above with `uptimeSeconds` 3600; node 1's socket receives nothing within 1 second.
  - `It("starts the next node when a rebooted node registers again")`: as above, but node 0 registers again with its machine ID instead of heartbeating.
- [ ] **Step 2: Run** `go run github.com/onsi/ginkgo/v2/ginkgo -r ./test/integration`. Expected: the new specs fail (the endpoints still return a list of commands).
- [ ] **Step 3: Implement** the handler, hook, wiring and removal changes listed above.
- [ ] **Step 4: Run** `go build ./... && go vet ./...`, then `go run github.com/onsi/ginkgo/v2/ginkgo -r ./pkg ./internal ./test/integration`. Expected: all pass.
- [ ] **Step 5: Commit** `feat(commands): roll commands out in batches`.

---

### Task 6: Batch endpoints, client and API docs

**Files:**
- Create: `pkg/handlers/batches.go`, `pkg/client/batches.go`, `pkg/client/batches_test.go`
- Modify: `pkg/server/server.go`, `pkg/client/types.go`, `pkg/client/commands.go`, `pkg/client/groups.go`, `pkg/client/client.go`, `test/integration/batches_test.go`, `docs/` (regenerated)

**Interfaces:**
- Produces, HTTP (admin group):
  - `GET /api/v1/batches?groupID=&nodeID=&limit=&offset=` → `{"batches": [CommandBatch], "total": n}`
  - `GET /api/v1/batches/:id` → `CommandBatch` with `nodes`; 404 `{"error": "batch not found"}`
  - `POST /api/v1/batches/:id/stop` → 200 with the batch; 404 unknown; 409 `{"error": "batch is not running"}` (Review Focus 5)
- Produces, `pkg/client`:

```go
type Batch struct { /* JSON mirror of store.CommandBatch */ }
type BatchNode struct { /* JSON mirror of store.BatchNode */ }
type BatchList struct { Batches []Batch `json:"batches"`; Total int `json:"total"` }
type BatchListOptions struct { GroupID, NodeID string; Limit, Offset int }
type RolloutOptions struct {
    Concurrency          int   `json:"concurrency,omitempty"`
    StopOnFailure        *bool `json:"stopOnFailure,omitempty"`
    RebootTimeoutSeconds int   `json:"rebootTimeoutSeconds,omitempty"`
}
func (s *BatchesService) Get(ctx context.Context, id string) (*Batch, error)
func (s *BatchesService) List(ctx context.Context, opts BatchListOptions) (*BatchList, error)
func (s *BatchesService) Stop(ctx context.Context, id string) (*Batch, error)
// CommandsService.CreateBulk and GroupsService.SendCommand return (*Batch, error).
// BulkCommandRequest and GroupCommandRequest embed RolloutOptions in place of FailFast.
// Client gets a Batches *BatchesService field.
```

- Swagger annotations on the three handlers and the changed create handlers, in the style of `CommandHandler.Create`.

- [ ] **Step 1: Write the failing specs**: in `pkg/client/batches_test.go`, against an `httptest` server in the style of `groups_test.go`: `Get`, `List` (query string carries the options), `Stop` returning `*APIError`-style error on 409. In `test/integration/batches_test.go`: `It("reports why a batch stopped")` (fail node 0, `GET /batches/:id` shows phase `stopped`, `stoppedByNodeID`, `stopReason`, node 0 `failed`, node 1 `canceled`), `It("lists a group's batches")`, `It("stops a running batch")`, `It("refuses to stop a batch that is not running")`.
- [ ] **Step 2: Run** `go run github.com/onsi/ginkgo/v2/ginkgo -r ./pkg/client ./test/integration`. Expected: fail.
- [ ] **Step 3: Implement** the handler, routes, client types and methods.
- [ ] **Step 4: Regenerate docs** with the repo's swag command (`grep -n swag Makefile` for the target) and check `git diff --stat docs/` shows only the batch-related changes.
- [ ] **Step 5: Run** `go run github.com/onsi/ginkgo/v2/ginkgo -r ./pkg ./internal ./test/integration`. Expected: pass.
- [ ] **Step 6: Commit** `feat(api): list, inspect and stop batches`.

---

### Task 7: UI API and the Rollout section

**Files:**
- Create: `ui/src/api/batches.ts`, `ui/src/test/commandDialog.rollout.test.tsx`
- Modify: `ui/src/api/groups.ts`, `ui/src/api/nodes.ts`, `ui/src/components/CommandDialog.tsx`

**Interfaces:**
- Produces, `ui/src/api/batches.ts`:

```ts
export type BatchPhase = "running" | "completed" | "failed" | "stopped";
export type BatchNodeState = "waiting" | "running" | "rebooting" | "done" | "failed" | "timed_out" | "skipped" | "canceled";
export interface BatchNode { batchID: string; nodeID: string; position: number; hostname: string; commandID: string | null; state: BatchNodeState; message: string; startedAt: string | null; deadline: string | null; finishedAt: string | null; }
export interface Batch { id: string; command: string; args: Record<string, string>; selection: string; groupID: string | null; concurrency: number; stopOnFailure: boolean; rebootTimeoutSeconds: number; phase: BatchPhase; createdAt: string; finishedAt: string | null; stoppedAt: string | null; stopReason: string; stoppedByNodeID: string | null; counts: Partial<Record<BatchNodeState, number>>; nodes?: BatchNode[]; }
export interface Rollout { concurrency?: number; stopOnFailure: boolean; rebootTimeoutSeconds: number; }
export function listBatches(opts?: { groupID?: string; nodeID?: string; limit?: number; offset?: number }): Promise<{ batches: Batch[]; total: number }>;
export function getBatch(id: string): Promise<Batch>;
export function stopBatch(id: string): Promise<Batch>;
```

  - `sendGroupCommand(groupID, command, args, rollout?: Rollout): Promise<Batch>` and `sendBulkCommand(selector, command, args, rollout?: Rollout): Promise<Batch>`; the rollout fields are spread into the request body.
  - `CommandDialog` gets `targetCount?: number` and `onSubmit(command, args, rollout?: Rollout)`. The Rollout section renders only when `targetCount > 1`.

Rollout section copy, exactly:
- Heading `Rollout`, with `<targetCount> nodes` next to it.
- `Nodes at a time`, number input, placeholder `All at once`, help `The next node starts when one finishes. Leave empty to run on all nodes at once.`
- Checkbox `Stop on the first failure`, checked by default, help `Nodes that have not started are canceled. Nodes already running finish.`
- `Wait for a node to come back`, number input in minutes, default `10`, help `A node that has not come back by then counts as failed.`, rendered only for `upgrade`, `reset`, `reboot`.
- The submit button reads `<Label> <targetCount> nodes` (for example `Upgrade 12 nodes`) when `targetCount > 1`, and keeps today's label otherwise.

- [ ] **Step 1: Write the failing tests** in `commandDialog.rollout.test.tsx` (mock `@/api/artifacts` as `commandDialog.resetArgs.test.tsx` does):
  - `it("hides the Rollout section for a single node")`.
  - `it("sends the defaults when the user changes nothing")`: Reboot, `targetCount={3}` → `onSubmit` third argument `{ stopOnFailure: true, rebootTimeoutSeconds: 600 }` with no `concurrency`.
  - `it("sends the nodes at a time, the stop setting and the timeout in seconds")`: concurrency `2`, uncheck stop, timeout `15` → `{ concurrency: 2, stopOnFailure: false, rebootTimeoutSeconds: 900 }`.
  - `it("shows the timeout only for commands that reboot the node")`: present for Reboot, absent for Run command.
  - `it("names the node count on the submit button")`: `Reboot 3 nodes`.
- [ ] **Step 2: Run** `cd ui && npm test -- src/test/commandDialog.rollout.test.tsx`. Expected: fail.
- [ ] **Step 3: Implement** the API module and the dialog changes. Update `GroupDetail.tsx` and `Nodes.tsx` call sites to pass `targetCount` and forward `rollout`; navigation to the batch comes in Task 8.
- [ ] **Step 4: Run** `cd ui && npm test && npm run lint && npx tsc -b`. Expected: pass.
- [ ] **Step 5: Commit** `feat(ui): set the rollout of a command sent to several nodes`.

---

### Task 8: Batches page, batch page, navigation

**Files:**
- Create: `ui/src/pages/Batches.tsx`, `ui/src/pages/BatchDetail.tsx`, `ui/src/test/batchDetail.test.tsx`, `ui/src/test/batches.test.tsx`
- Modify: `ui/src/App.tsx` (routes `batches`, `batches/:id`), `ui/src/components/Layout.tsx` (sidebar item `Batches` after `Groups`, icon `ListChecks` from lucide-react), `ui/src/pages/GroupDetail.tsx`, `ui/src/pages/Nodes.tsx` (navigate to `/batches/<id>` after a successful start)

**Interfaces:**
- Consumes: `ui/src/api/batches.ts` (Task 7).

`BatchDetail` content, following the spec's UI section:
- `PageHeader` title `<Command label> · <selection>`, a phase badge, and meta `<concurrency or "All"> at a time, stop on failure|keep going on failure, wait <N> min` (the wait part only for commands that reboot).
- When `phase === "stopped"`: a danger banner `Stopped at <time>: <stopReason>`. When `stoppedByNodeID` is set, that node's row is `timed_out`, and the node's `lastHeartbeat` (from `getNode`) is after the row's `deadline`: a second line `<hostname> came back at <lastHeartbeat time>. The batch stays stopped.`
- Counts per state, then a table: Node (link to `/nodes/<id>` unless `message === "node deleted"`), State, Message, Started, Finished.
- `phase === "running"`: button `Stop batch` (confirm with `ConfirmDialog`), calls `stopBatch`.
- Finished with remaining nodes (`failed`, `timed_out`, `skipped`, `canceled`, excluding `node deleted`): button `Start a new batch for the <n> remaining nodes`. It opens `CommandDialog` with `defaultCommand` set to the batch command, the rollout prefilled from the batch, and a checkbox list of the remaining nodes (all checked); submitting calls `sendBulkCommand({ nodeIDs: checked }, command, batch.args, rollout)` and navigates to the new batch. Add a `nodeChoices?: { id: string; label: string }[]` prop to `CommandDialog` for the checkbox list, and `defaultRollout?: Rollout` and `defaultArgs?: Record<string, string>` so the command's arguments carry over.
- Refresh every 5 seconds while `phase === "running"` or any row is `running` or `rebooting`.

`Batches` page: table of `listBatches()` with Command, Selection, Phase badge, counts (`<done> done, <failed> failed, ...` listing only non-zero states), Started; rows link to the batch page; offset pagination with 50 per page.

- [ ] **Step 1: Write the failing tests** (mock `@/api/batches` and `@/api/nodes`):
  - `batchDetail.test.tsx`: `it("explains why a stopped batch stopped")`; `it("says when the node that timed out came back")`; `it("offers a new batch for the nodes that did not finish, without deleted nodes")` (button text `Start a new batch for the 2 remaining nodes` for one `failed`, one `canceled`, one `canceled` with `node deleted`, one `done`); `it("stops a running batch after confirmation")`.
  - `batches.test.tsx`: `it("lists batches with their non-zero counts and links to them")`.
- [ ] **Step 2: Run** `cd ui && npm test -- src/test/batchDetail.test.tsx src/test/batches.test.tsx`. Expected: fail.
- [ ] **Step 3: Implement** the pages, routes, sidebar item, dialog props and navigation.
- [ ] **Step 4: Run** `cd ui && npm test && npm run lint && npx tsc -b`. Expected: pass.
- [ ] **Step 5: Commit** `feat(ui): show batches and why they stopped`.

---

### Task 9: Batches on the group and node pages

**Files:**
- Modify: `ui/src/pages/GroupDetail.tsx`, `ui/src/pages/NodeDetail.tsx`
- Create: `ui/src/components/fleet/RecentBatches.tsx`, `ui/src/test/recentBatches.test.tsx`

**Interfaces:**
- Produces: `RecentBatches({ groupID?: string; nodeID?: string })`: a card titled `Batches` listing the 5 newest batches from `listBatches({ groupID, nodeID, limit: 5 })` with command, selection, phase badge and start time, each linking to its batch page, and a `View all` link to `/batches`. On the node page the entries show the batch phase, not the node's own state, because list results carry no node rows. Empty state text `No batches yet.`

- [ ] **Step 1: Write the failing test** `recentBatches.test.tsx`: `it("lists a group's newest batches and links to them")`, `it("shows an empty state")`, `it("asks for the node's batches when given a node")` (asserts `listBatches` called with `{ nodeID: "n1", limit: 5 }`).
- [ ] **Step 2: Run** `cd ui && npm test -- src/test/recentBatches.test.tsx`. Expected: fail.
- [ ] **Step 3: Implement** the component and place it on both pages.
- [ ] **Step 4: Run** `cd ui && npm test && npm run lint && npx tsc -b`. Expected: pass.
- [ ] **Step 5: Commit** `feat(ui): link groups and nodes to their batches`.

---

### Task 10: Rolling batch on real VMs

**Files:**
- Modify: `e2e/phonehome_vm_test.go`, `e2e/phonehome_api_test.go`

**Interfaces:**
- Consumes: the group command endpoint with `concurrency` (Task 5) and `GET /api/v1/batches/:id` (Task 6).
- Produces: `func (w *webServer) batch(id string) map[string]any` in `e2e/phonehome_api_test.go`; `sendGroupCommand` returns the batch ID (the endpoint now returns the batch record).

- [ ] **Step 1: Add spec 3**, `It("upgrades the nodes one at a time")`, after spec 2. Build a third artifact the way the tier builds the upgrade target, with a new marker token. Start `upgrade` to it on the group with `"concurrency": 1`. Wait (15 minutes per node, named) until the batch phase is `completed`. Assert from `batch(id)`: both node rows `done`; the second row's `startedAt` is not before the first row's `finishedAt`; each VM's boot ID changed and its marker holds the new token; both nodes report `upgradeState` `done`. Adapt specs 1 and 2 to `sendGroupCommand` returning a batch ID (read the command IDs from the batch's node rows).
- [ ] **Step 2: Run** `make test-e2e-phonehome`. Expected: 3 of 3 specs pass and nothing is left running (`docker ps -a --filter name=auroraboot-e2e-web` empty, no tier `qemu-system` or `swtpm` process). The main checkout's root-owned `auroraboot-data/` breaks the docker build context; run from a clean worktree at HEAD and remove it after.
- [ ] **Step 3: Commit** `test(e2e): upgrade the phonehome tier's nodes one at a time`.

---

### Task 11: Whole-branch check

- [ ] **Step 1:** `gofmt -l . | grep -v '^ui/'` prints nothing; `go vet ./...` passes.
- [ ] **Step 2:** `go run github.com/onsi/ginkgo/v2/ginkgo -r -p --skip-package ./e2e ./...` passes; once more with `AURORABOOT_TEST_POSTGRES_DSN` set.
- [ ] **Step 3:** `cd ui && npm test && npm run lint && npm run build` passes.
- [ ] **Step 4:** `grep -rn "FailFast\|failFast\|CancelPendingInBatch\|SummarizeBatch\|BatchOutcome\|ListByBatch\|commands/batches" --include=*.go --include=*.ts --include=*.tsx pkg internal ui/src test` prints nothing.
