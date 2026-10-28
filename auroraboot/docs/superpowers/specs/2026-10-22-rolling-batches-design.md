# Rolling batches and batch status

Date: 2026-10-22
Issues: kairos-io/kairos#4195 (rolling batches), kairos-io/kairos#4268 (batch
status in the API and UI)
Branch: one branch for both issues, so the backend can be tested by hand
through the UI.

## Why

A command sent to many nodes at once is a batch. Today every online node of a
batch gets its command immediately. `failFast` cancels only the commands that no
node has picked up yet, which in practice means the offline nodes. A bad
artifact therefore reaches every online node of a group before the first
failure can stop anything.

The Kairos operator solves this with two settings on `NodeOp` and
`NodeOpUpgrade`: `concurrency` (how many nodes run at the same time, 0 for all)
and `stopOnFailure` (start no new nodes after a failure). AuroraBoot copies them.
The user decides the strategy: one node at a time for an untried artifact, larger
batches once it has proven itself.

The user also needs to see what a batch did, and why it stopped, after the
fact. That is #4268.

## Goals

- A batch runs at most N nodes at the same time. N is set per batch; empty or 0
  means all nodes at once.
- With stop-on-failure on, the first failed node stops the batch. Nodes that
  have not started are canceled, and so are nodes whose command was never
  delivered, along with that command. Nodes already running their command
  finish.
- A node that AuroraBoot rebooted and that does not come back within a timeout
  counts as failed.
- Every batch keeps a record of its settings, its outcome, why it stopped and
  what happened to each node. That record does not change once the batch and its
  nodes have finished, whatever the nodes do later.
- The UI lets the user set the rollout options when starting a batch, watch a
  batch, stop it, and start a new batch for the nodes that did not finish.

## Non-goals

- Using the Kairos operator as a backend for commands. AuroraBoot's commands
  target phonehome nodes; an operator-driven upgrade UI would be a separate
  feature.
- Resuming a stopped batch. The user starts a new batch for the remaining
  nodes, so every batch keeps its own history.
- Waiting for reboots AuroraBoot did not start. If an `exec` script reboots the
  node, the batch does not wait for it, the same as the operator.
- Keeping batches created before this change readable. The release notes say
  that older batches are not shown.

## Anti-goals

- A batch must never look finished or successful when a node failed. In
  particular, a node that timed out and came back later must not erase the
  reason the batch stopped.
- A held-back node must never run its command before its slot opens, through
  any delivery path (push, the agent's poll, the replay on reconnect).

## Concepts

**Batch.** A command sent to a set of nodes in one request, from a group's page,
from a label selector or from nodes picked on the Nodes page. Its node list is
fixed when it starts: nodes that join or leave a group later do not change a
running batch.

**Slot.** A node that has started and not yet settled holds a slot. A batch with
concurrency N never has more than N nodes holding a slot.

**Settled.** A node is settled when its outcome is final:

| Command | Settled as done | Settled as failed |
|---|---|---|
| `upgrade` | comes back on `active` | command reports `Failed`; comes back on `passive` or `recovery`; timeout |
| `reset` | comes back on `active` after the reset | command reports `Failed`; comes back on `passive` or `recovery`; timeout |
| `reboot` | comes back on any boot entry other than `passive` or `recovery` | command reports `Failed`; comes back on `passive` or `recovery`; timeout |
| `exec`, `apply-cloud-config`, `extension`, `upgrade-recovery` | command reports `Completed` | command reports `Failed`; node gone past its deadline |

For `upgrade`, `reset` and `reboot` the timeout clock starts when the command
reports `Completed`, which is when the agent has scheduled the reboot. The
default is 10 minutes and the user can change it per batch.

**Comes back.** An agent that holds its credentials does not register again
after a reboot; it reconnects and heartbeats (see `SettleAfterReboot` in
`internal/store/gorm/store.go`, which settles the node's own upgrade and reset
state the same way). A node comes back when either of these arrives while its
batch row is `rebooting`:

- a heartbeat whose boot time (now minus the uptime in its metrics) is after
  the row's `rebooting_since` by more than a few seconds, so the jitter of a
  whole-second uptime cannot make the boot before the reboot look like a new
  one. The heartbeat's boot state gives the outcome;
- a registration, which an agent only sends when it has no credentials, so it
  follows a boot. Its boot state gives the outcome.

Neither settles a row that is not `rebooting`, that is, before its own command
reported `Completed`, so an unrelated reboot does not count. Heartbeats without
an uptime (agents before v4.4.0-rc1) do not settle anything; such a node settles
only by registering, or times out.

A reset reboots twice: into `autoreset`, then into `active`. When the node
comes back on `autoreset`, its `rebooting_since` moves to that boot and its
deadline starts again, so the timeout applies to each reboot and not to the
whole reset.

**Skipped.** A node that is offline when the batch reaches it is not started.
When filling a slot, the scheduler takes the next waiting node that is online
and leaves offline ones waiting, so they get another chance each time a slot
frees up. When every waiting node left is offline and no node holds a slot, the
remaining nodes settle as `skipped` with "offline when the batch reached it". A
skip is not a failure and does not stop the batch.

A started node gets a deadline as well, `reboot_timeout` after it started. A
node still running its command past that deadline settles as `timed_out` with
"stopped sending heartbeats before it finished its command" when it is offline
or has sent no heartbeat for two minutes, and its command is canceled if it was
never delivered. Agents that only poll over REST are never marked offline, so
the heartbeat age is what shows that they stopped. A node that took its command
and keeps sending heartbeats (a long `exec`, for example) holds its slot until
it reports or the user stops the batch.

## Data model

Two new tables. They replace the batch information that is copied onto every
command row today.

**`command_batches`**, one row per batch:

- `id`
- `command`, `args`
- `selection`: a short description of how the nodes were picked, for display
  only, e.g. "group edge-sites", "label role=worker", "5 selected nodes".
  Nothing reads it to choose nodes.
- `group_id`: set when the batch was started from a group, so the group page
  can list its batches. Null otherwise.
- `concurrency` (0 = all), `stop_on_failure` (default true),
  `reboot_timeout` (default 10 minutes)
- `phase`: `running`, `completed`, `failed`, `stopped`
  - `completed`: every node settled as done or skipped. The counts show the
    skipped ones.
  - `failed`: every node settled and at least one failed, with stop-on-failure
    off.
  - `stopped`: stopped by a failure or by the user. Nodes still running when it
    stopped keep their own row and settle normally; the batch phase stays
    `stopped`.
- `created_at`, `finished_at`
- `stopped_at`, `stop_reason` (text), `stopped_by_node_id` (null when the user
  stopped it)

**`command_batch_nodes`**, one row per target node:

- `batch_id`, `node_id`, `position` (the order nodes are started in)
- `hostname`: copied when the batch starts, so the batch page can still name a
  node that was deleted later
- `command_id`: null until the node's slot opens
- `state`: `waiting`, `running`, `rebooting`, `done`, `failed`, `timed_out`,
  `skipped`, `canceled`
- `message`: e.g. "fell back to passive", "did not come back within 10
  minutes", "stopped sending heartbeats before it finished its command",
  "offline when the batch reached it", "batch stopped"
- `started_at`, `rebooting_since` and `deadline` (set when the node starts,
  again when it enters `rebooting`, from the command's completion time, and
  again when a resetting node comes back on `autoreset`), `finished_at`

`done`, `failed`, `timed_out`, `skipped` and `canceled` are final. A final row
never changes again.

If a node, or the command of a running row, is deleted, the row settles as
`canceled` with "node deleted" or "command deleted". A deletion is the user's
action, not a failure, so it does not stop the batch.

`NodeCommand.BatchID` stays, so a command can be traced to its batch.
`NodeCommand.FailFast` and `store.CancelBatchAfterFailure` are removed. The
undelivered commands a stopped batch cancels are the commands of its running
rows, so the batch store cancels them by ID. `SummarizeBatch` is replaced by reading
the batch record.

## Scheduling

A node's command row is created only when its slot opens. A waiting node has no
command, so the push, the agent's poll and the replay on reconnect need no
change: there is nothing for them to hand out. This is what keeps held-back
nodes from running early.

Starting a batch:

1. Resolve the target nodes, write the batch and one `waiting` row per node.
2. Fill the first N slots (all nodes when concurrency is 0): create the
   command rows the same way a single-node command is created today
   (including the upgrade/reset lifecycle marking that `CreateBatch` does),
   set the node rows to `running`, and push the commands.

Events that settle a node, then fill free slots or stop the batch:

- A command status report (REST, admin status write, WebSocket
  `command_status`). `Failed` settles the node as failed. `Completed` settles
  it as done for commands without a reboot; for `upgrade`, `reset` and
  `reboot` it moves the node to `rebooting` and sets its deadline.
- A node coming back (a heartbeat from a boot after `rebooting_since`, or a
  registration), which settles its `rebooting` rows from the boot state as in
  the table above.
- A periodic check (for example every 30 seconds) that settles every
  `rebooting` node past its deadline as `timed_out`, and every `running` node
  past its deadline that is gone, as described above. When a `rebooting` node
  times out, its upgrade or reset lifecycle state is marked failed as well,
  with the same message. If the node comes back later, its lifecycle state may become done,
  but its batch row stays `timed_out`.

After a node settles:

- If it failed and the batch has stop-on-failure on: the batch becomes
  `stopped` with the reason and the node. Every `waiting` row, and every
  `running` row whose command is still `Pending`, becomes `canceled` with
  "batch stopped", and those commands are canceled.
- Otherwise, if waiting nodes remain, start the next online ones until N nodes
  hold a slot. If no node holds a slot and every waiting node is offline, they
  settle as `skipped`.
- When every row is final, set the batch phase and `finished_at`.

Stopping by hand: the same as a stop on failure, with `stopped_by_node_id`
null and the reason "stopped by the user". Nodes whose command reached the
agent finish and settle normally.

The batch state lives in the database, so a restart of AuroraBoot picks up
where it left off: the periodic check also fills free slots of running batches,
which covers a crash between settling a node and starting the next one.

## API

- `POST /api/v1/groups/:id/commands` and `POST /api/v1/nodes/commands` accept
  `concurrency`, `stopOnFailure` and `rebootTimeoutSeconds` in place of
  `failFast`. They return the batch record instead of a list of commands.
- `GET /api/v1/batches`: newest first, paginated, filterable by `groupID` and
  `nodeID`.
- `GET /api/v1/batches/:id`: the batch with its node rows, including each
  node's hostname for display.
- `POST /api/v1/batches/:id/stop`.
- Both batch endpoints create a batch even when they match one node, so every
  command sent through them shows up in the batch history.
- `POST /api/v1/nodes/:nodeID/commands` (one node) keeps working as today and
  creates no batch. It rejects `upgrade`, `reset` and `reboot` with 409 while
  the node is in a running batch, so a single-node command cannot settle a
  batch row through the shared lifecycle state. The check and the command
  insert run under the same lock as a batch start's check and creation.
- Both 409 bodies for busy nodes are `{"error", "nodeIDs"}`.
- Starting a batch whose nodes are already in a running batch is rejected with
  409 that names them.
- `GET /api/v1/commands/batches/:batchID` is removed.
- `pkg/client` and the OpenAPI docs follow the API.

## UI

Wireframes from the brainstorm are summarized here.

**Command dialog.** When a command targets more than one node (group page, or
several nodes picked on the Nodes page), the dialog shows a "Rollout" section:

- "Nodes at a time", empty for all at once.
- "Stop on the first failure", checked by default.
- "Wait for a node to come back", 10 minutes, shown only for upgrade, reset
  and reboot.

When a batch endpoint reaches one node (a one-node group, one node picked on
the Nodes page, or one remaining node of a batch), the section shows only
"Wait for a node to come back", for upgrade, reset and reboot. The dialog sends
the rollout settings to the batch endpoints for any number of nodes, so a
repeated batch keeps its timeout.

After starting, the UI navigates to the new batch.

**Batches page** in the sidebar: every batch, newest first, with command,
selection, phase, counts per state and start time.

**Batch page:**

- Header: command and target, phase badge, settings ("2 at a time, stop on
  failure, wait 10 min").
- A banner when the batch stopped, with the time, the node and the reason. When
  the stopping node timed out and has sent a heartbeat since its deadline, the
  banner adds that it came back at that time, and the batch stays stopped. For
  other failures the banner shows only the reason.
- Counts per state.
- A table of nodes: hostname, state, message, start and finish time, and a
  link to the node.
- While running: "Stop batch". Once finished with nodes that are not `done`:
  "Start a new batch for the N remaining nodes". Remaining means `failed`,
  `timed_out`, `skipped` and `canceled`. It opens the command dialog with the
  same command, args and settings, and a list of those nodes with checkboxes,
  so the user can leave out a node that failed. Deleted nodes are not offered.

**Links in:** the group page lists its recent batches; the node page lists the
batches the node was part of.

The batch page refreshes while the batch is running.

## Acceptance on real VMs

The phonehome VM tier (`e2e/phonehome_vm_test.go`) gains a spec that runs a
batch upgrade of its two nodes with concurrency 1 and checks, from the batch
record, that the second node started only after the first came back `done`,
and that both end `done` on the new artifact. It is the test that shows the
reboot loop works end to end.

## Decided by the builder

These were not discussed and are left to the implementer, within the goals
above:

- How the batch page refreshes (polling or the existing UI WebSocket).
- How "online" is decided when filling a slot. The WebSocket hub knows which
  nodes are connected to this instance; the stored node phase works across
  instances on PostgreSQL.
- Exact table and column names, indexes, and the periodic check interval.
- Concurrency safety on PostgreSQL: settling a node and filling slots must
  happen under a lock on the batch row, so two events cannot start more than N
  nodes.
