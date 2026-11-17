package gorm

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kairos-io/AuroraBoot/pkg/store"
)

const defaultBatchListLimit = 50

// BatchCreate writes the batch and its node rows in one transaction.
func (s *Store) BatchCreate(ctx context.Context, b *store.CommandBatch, nodes []*store.BatchNode) error {
	for _, n := range nodes {
		if n.State != store.BatchNodeWaiting && n.State != "" {
			return fmt.Errorf("batch node %s must start waiting, got %q", n.NodeID, n.State)
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		b.Phase = store.BatchRunning
		// Nodes are written through their own table below, with the batch ID
		// set from the batch.
		rec := *b
		rec.Nodes = nil
		if err := tx.Create(&rec).Error; err != nil {
			return err
		}
		b.CreatedAt = rec.CreatedAt
		for _, n := range nodes {
			n.BatchID = b.ID
			n.State = store.BatchNodeWaiting
		}
		if len(nodes) > 0 {
			if err := tx.Create(&nodes).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// BatchGet returns a batch with its nodes ordered by position and its counts.
func (s *Store) BatchGet(ctx context.Context, id string) (*store.CommandBatch, error) {
	var b store.CommandBatch
	if err := s.db.WithContext(ctx).First(&b, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, store.ErrBatchNotFound
		}
		return nil, err
	}
	var nodes []*store.BatchNode
	if err := s.db.WithContext(ctx).Where("batch_id = ?", id).Order("position ASC").Find(&nodes).Error; err != nil {
		return nil, err
	}
	b.Nodes = nodes
	b.Counts = map[string]int{}
	for _, n := range nodes {
		b.Counts[n.State]++
	}
	return &b, nil
}

// BatchList returns batches newest first, without nodes but with the
// hostnames of their first nodes, and the total number matching the filter.
func (s *Store) BatchList(ctx context.Context, f store.BatchFilter) ([]*store.CommandBatch, int, error) {
	q := s.db.WithContext(ctx).Model(&store.CommandBatch{})
	if f.GroupID != "" {
		q = q.Where("group_id = ?", f.GroupID)
	}
	if f.NodeID != "" {
		q = q.Where("id IN (SELECT batch_id FROM command_batch_nodes WHERE node_id = ?)", f.NodeID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultBatchListLimit
	}
	var batches []*store.CommandBatch
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(f.Offset).Find(&batches).Error; err != nil {
		return nil, 0, err
	}
	if err := s.fillBatchCounts(ctx, batches); err != nil {
		return nil, 0, err
	}
	if err := s.fillBatchHostnames(ctx, batches); err != nil {
		return nil, 0, err
	}
	return batches, int(total), nil
}

func (s *Store) fillBatchCounts(ctx context.Context, batches []*store.CommandBatch) error {
	byID := make(map[string]*store.CommandBatch, len(batches))
	ids := make([]string, 0, len(batches))
	for _, b := range batches {
		b.Counts = map[string]int{}
		byID[b.ID] = b
		ids = append(ids, b.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	var rows []struct {
		BatchID string
		State   string
		N       int
	}
	if err := s.db.WithContext(ctx).Model(&store.BatchNode{}).
		Select("batch_id, state, COUNT(*) AS n").
		Where("batch_id IN ?", ids).
		Group("batch_id, state").
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		byID[r.BatchID].Counts[r.State] = r.N
	}
	return nil
}

// fillBatchHostnames sets the hostnames of each batch's first nodes. Positions
// count from 0 in start order, so the first nodes are those below the limit.
func (s *Store) fillBatchHostnames(ctx context.Context, batches []*store.CommandBatch) error {
	byID := make(map[string]*store.CommandBatch, len(batches))
	ids := make([]string, 0, len(batches))
	for _, b := range batches {
		byID[b.ID] = b
		ids = append(ids, b.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	var rows []struct {
		BatchID  string
		Hostname string
	}
	if err := s.db.WithContext(ctx).Model(&store.BatchNode{}).
		Select("batch_id, hostname").
		Where("batch_id IN ? AND position < ?", ids, store.BatchListHostnames).
		Order("position").
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		byID[r.BatchID].Hostnames = append(byID[r.BatchID].Hostnames, r.Hostname)
	}
	return nil
}

// BatchBusyNodes returns the IDs among nodeIDs that have a row in a non-final state.
func (s *Store) BatchBusyNodes(ctx context.Context, nodeIDs []string) ([]string, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	var busy []string
	err := s.db.WithContext(ctx).Model(&store.BatchNode{}).
		Where("node_id IN ?", nodeIDs).
		Where("state NOT IN ?", []string{
			store.BatchNodeDone, store.BatchNodeFailed, store.BatchNodeTimedOut,
			store.BatchNodeSkipped, store.BatchNodeCanceled,
		}).
		Distinct().Pluck("node_id", &busy).Error
	return busy, err
}

// BatchAdvance brings one batch up to date at now, in one transaction: it
// settles node rows from their nodes and commands, times out rebooting nodes
// past their deadline, stops the batch on a failure when it asks for that,
// starts waiting online nodes in free slots, skips the waiting nodes once
// only offline ones are left, and finishes the batch when every row is final.
// It returns the commands it created, for the caller to push once the
// transaction has committed.
//
// lockBatch runs first, so two advances of the same batch run one after the
// other and the second sees the slots the first one filled.
func (s *Store) BatchAdvance(ctx context.Context, batchID string, now time.Time) ([]*store.NodeCommand, error) {
	var created []*store.NodeCommand
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		b, err := lockBatch(tx, batchID)
		if err != nil {
			return err
		}
		rows, err := batchRows(tx, batchID)
		if err != nil {
			return err
		}
		a := newBatchAdvance(b, rows, now)
		a.resetTimeout = s.resetTimeout
		if err := a.load(tx); err != nil {
			return err
		}
		a.cancelDeletedNodes()
		a.reconcileRunning()
		if err := a.timeOut(tx); err != nil {
			return err
		}
		if err := a.stopOnFailure(tx); err != nil {
			return err
		}
		if b.Phase == store.BatchRunning {
			cmds := a.fillSlots()
			if err := createCommands(tx, cmds); err != nil {
				return err
			}
			created = cmds
			a.skipOffline()
		}
		if err := a.save(tx); err != nil {
			return err
		}
		return a.finish(tx)
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// lockBatch takes the lock that serializes advances of one batch and returns
// the batch row.
//
// On PostgreSQL it reads the row with a row lock. On SQLite its first
// statement is a no-op UPDATE of the row, which takes the database write lock
// before the transaction reads anything. Under WAL a transaction that reads
// first holds a snapshot, and if another connection commits (a status report,
// a heartbeat, another advance) before the transaction writes, the write fails
// at once with "database is locked" (SQLITE_BUSY_SNAPSHOT) and busy_timeout
// does not retry it. Writing first makes a concurrent writer only wait.
func lockBatch(tx *gorm.DB, id string) (*store.CommandBatch, error) {
	q := tx
	if tx.Dialector.Name() == "sqlite" {
		res := tx.Exec("UPDATE command_batches SET id = id WHERE id = ?", id)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			return nil, store.ErrBatchNotFound
		}
	} else {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var b store.CommandBatch
	if err := q.First(&b, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, store.ErrBatchNotFound
		}
		return nil, err
	}
	return &b, nil
}

func batchRows(tx *gorm.DB, batchID string) ([]*store.BatchNode, error) {
	var rows []*store.BatchNode
	err := tx.Where("batch_id = ?", batchID).Order("position ASC").Find(&rows).Error
	return rows, err
}

// batchAdvance holds what one change to a batch read and the rows it changed.
// BatchAdvance, BatchStop and BatchNodeBooted all work through it, so a batch
// stops and finishes the same way whichever of them changed it.
type batchAdvance struct {
	batch *store.CommandBatch
	rows  []*store.BatchNode
	now   time.Time
	// resetTimeout is the delivery expiry of the reset commands fillSlots
	// creates; zero or negative creates them without one.
	resetTimeout time.Duration
	// nodePhase maps each existing node of the batch to its phase.
	nodePhase map[string]string
	// lastHeard maps each existing node of the batch to its last heartbeat,
	// nil when it never sent one.
	lastHeard map[string]*time.Time
	// commands maps the command ID of each running row to its command.
	commands map[string]*store.NodeCommand
	changed  map[*store.BatchNode]bool
	// failed lists the rows that became failed or timed_out in this change.
	failed []*store.BatchNode
}

func newBatchAdvance(b *store.CommandBatch, rows []*store.BatchNode, now time.Time) *batchAdvance {
	return &batchAdvance{batch: b, rows: rows, now: now, changed: map[*store.BatchNode]bool{}}
}

func (a *batchAdvance) load(tx *gorm.DB) error {
	nodeIDs := make([]string, 0, len(a.rows))
	var commandIDs []string
	for _, row := range a.rows {
		nodeIDs = append(nodeIDs, row.NodeID)
		if row.State == store.BatchNodeRunning && row.CommandID != nil {
			commandIDs = append(commandIDs, *row.CommandID)
		}
	}
	a.nodePhase = map[string]string{}
	a.lastHeard = map[string]*time.Time{}
	if len(nodeIDs) > 0 {
		var nodes []struct {
			ID            string
			Phase         string
			LastHeartbeat *time.Time
		}
		if err := tx.Model(&store.ManagedNode{}).Select("id, phase, last_heartbeat").
			Where("id IN ?", nodeIDs).Scan(&nodes).Error; err != nil {
			return err
		}
		for _, n := range nodes {
			a.nodePhase[n.ID] = n.Phase
			a.lastHeard[n.ID] = n.LastHeartbeat
		}
	}
	a.commands = map[string]*store.NodeCommand{}
	if len(commandIDs) > 0 {
		var cmds []*store.NodeCommand
		if err := tx.Where("id IN ?", commandIDs).Find(&cmds).Error; err != nil {
			return err
		}
		for _, c := range cmds {
			a.commands[c.ID] = c
		}
	}
	return nil
}

func (a *batchAdvance) settle(row *store.BatchNode, state, message string) {
	row.State = state
	row.Message = message
	now := a.now
	row.FinishedAt = &now
	a.changed[row] = true
	if state == store.BatchNodeFailed || state == store.BatchNodeTimedOut {
		a.failed = append(a.failed, row)
	}
}

func (a *batchAdvance) save(tx *gorm.DB) error {
	for _, row := range a.rows {
		if !a.changed[row] {
			continue
		}
		if err := tx.Save(row).Error; err != nil {
			return err
		}
	}
	return nil
}

func (a *batchAdvance) cancelDeletedNodes() {
	for _, row := range a.rows {
		if store.IsFinalBatchNodeState(row.State) {
			continue
		}
		if _, ok := a.nodePhase[row.NodeID]; !ok {
			a.settle(row, store.BatchNodeCanceled, "node deleted")
		}
	}
}

// reconcileRunning settles each running row from its command. A rebooting row
// waits on its node coming back, which its command cannot report.
func (a *batchAdvance) reconcileRunning() {
	for _, row := range a.rows {
		if row.State != store.BatchNodeRunning {
			continue
		}
		var cmd *store.NodeCommand
		if row.CommandID != nil {
			cmd = a.commands[*row.CommandID]
		}
		if cmd == nil {
			a.settle(row, store.BatchNodeCanceled, "command deleted")
			continue
		}
		switch cmd.Phase {
		case store.CommandFailed:
			a.settle(row, store.BatchNodeFailed, cmd.Result)
		case store.CommandExpired:
			a.settle(row, store.BatchNodeCanceled, "command expired")
		case store.CommandCanceled:
			a.settle(row, store.BatchNodeCanceled, "command canceled")
		case store.CommandCompleted:
			if !store.RebootsNode(cmd.Command) {
				a.settle(row, store.BatchNodeDone, "")
				continue
			}
			since := a.now
			if cmd.CompletedAt != nil {
				since = *cmd.CompletedAt
			}
			// A deadline already past here means the advance after the
			// command completed did not run in time, because AuroraBoot was
			// down. Startup postponed the started row's deadline for that
			// downtime, and the node could not report coming back in it
			// either, so the postponed deadline stands.
			deadline := since.Add(a.rebootTimeout())
			if deadline.Before(a.now) && row.Deadline != nil && row.Deadline.After(deadline) {
				deadline = *row.Deadline
			}
			row.State = store.BatchNodeRebooting
			row.RebootingSince = &since
			row.Deadline = &deadline
			a.changed[row] = true
		}
	}
}

func (a *batchAdvance) rebootTimeout() time.Duration {
	return rebootTimeout(a.batch.RebootTimeoutSeconds)
}

func rebootTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = store.DefaultRebootTimeoutSeconds
	}
	if seconds > store.MaxRebootTimeoutSeconds {
		seconds = store.MaxRebootTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

// nodeSilentAfter is how long a node can go without a heartbeat before a
// batch counts it as gone. Agents send one every 30 seconds by default.
const nodeSilentAfter = 2 * time.Minute

// gone reports whether the node of row is offline or has not sent a heartbeat
// for nodeSilentAfter. Agents that only poll over REST never go offline, so
// the heartbeat age is what shows that they stopped.
func (a *batchAdvance) gone(row *store.BatchNode) bool {
	if a.nodePhase[row.NodeID] != store.PhaseOnline {
		return true
	}
	heard := a.lastHeard[row.NodeID]
	return heard == nil || heard.Before(a.now.Add(-nodeSilentAfter))
}

// timeOut settles the rows past their deadline as timed_out.
//
// A running row times out only when its node is also gone, because a node
// that still sends heartbeats can be busy with a long command. Its command is
// canceled if it was never delivered; a delivered one is left to report on
// its own if the node comes back. Once the batch is stopped, a running row
// times out at its deadline even while its node sends heartbeats, so a
// command that never reports does not hold the node forever.
//
// A rebooting row times out at its deadline, and the upgrade or reset of its
// node is marked failed. The node's recorded reboot stays in place, so
// SettleAfterReboot can still move that state to done or failed when the node
// comes back later; the batch row stays timed_out.
func (a *batchAdvance) timeOut(tx *gorm.DB) error {
	timeout := a.rebootTimeout()
	minutes := int((timeout + time.Minute - 1) / time.Minute)
	message := fmt.Sprintf("did not come back within %d minutes", minutes)
	var nodeIDs, commandIDs []string
	for _, row := range a.rows {
		if row.Deadline == nil || !row.Deadline.Before(a.now) {
			continue
		}
		switch {
		case row.State == store.BatchNodeRunning && a.gone(row):
			a.settle(row, store.BatchNodeTimedOut, "stopped sending heartbeats before it finished its command")
			if row.CommandID != nil {
				commandIDs = append(commandIDs, *row.CommandID)
			}
		case row.State == store.BatchNodeRunning && a.batch.Phase == store.BatchStopped:
			a.settle(row, store.BatchNodeTimedOut, "did not finish its command after the batch stopped")
		case row.State == store.BatchNodeRebooting:
			a.settle(row, store.BatchNodeTimedOut, message)
			nodeIDs = append(nodeIDs, row.NodeID)
		}
	}
	if _, err := cancelPendingCommands(tx, commandIDs, "node stopped sending heartbeats", a.now); err != nil {
		return err
	}
	if len(nodeIDs) == 0 {
		return nil
	}
	switch a.batch.Command {
	case store.CmdUpgrade:
		if err := lockNodes(tx, nodeIDs); err != nil {
			return err
		}
		return tx.Model(&store.ManagedNode{}).
			Where("id IN ? AND upgrade_state = ?", nodeIDs, store.UpgradeStatePending).
			Update("upgrade_state", store.UpgradeStateFailed).Error
	case store.CmdReset:
		if err := lockNodes(tx, nodeIDs); err != nil {
			return err
		}
		return tx.Model(&store.ManagedNode{}).
			Where("id IN ? AND reset_state IN ?", nodeIDs,
				[]string{store.ResetStatePending, store.ResetStateInProgress}).
			Update("reset_state", store.ResetStateFailed).Error
	}
	return nil
}

// stopOnFailure stops a running batch with stop on failure on when any row
// failed in this change, naming the failed row with the lowest position.
func (a *batchAdvance) stopOnFailure(tx *gorm.DB) error {
	b := a.batch
	if b.Phase != store.BatchRunning || !b.StopOnFailure || len(a.failed) == 0 {
		return nil
	}
	first := a.failed[0]
	for _, row := range a.failed[1:] {
		if row.Position < first.Position {
			first = row
		}
	}
	nodeID := first.NodeID
	message := first.Message
	if message == "" {
		message = "command failed"
	}
	return a.stop(tx, first.Hostname+": "+message, &nodeID)
}

// stop moves the batch to stopped. It cancels the waiting rows, and the
// running rows whose command was never delivered along with that command, so
// no node starts the command after the batch stopped. Rows whose command
// reached the agent, and rebooting rows, keep going and settle on their own;
// a running row gets at least the batch's timeout from now to report before
// timeOut settles it. The rows it changes are written by save.
func (a *batchAdvance) stop(tx *gorm.DB, reason string, byNodeID *string) error {
	b := a.batch
	if err := tx.Model(&store.CommandBatch{}).Where("id = ?", b.ID).Updates(map[string]any{
		"phase":              store.BatchStopped,
		"stopped_at":         a.now,
		"stop_reason":        reason,
		"stopped_by_node_id": byNodeID,
	}).Error; err != nil {
		return err
	}
	now := a.now
	b.Phase = store.BatchStopped
	b.StoppedAt = &now
	b.StopReason = reason
	b.StoppedByNodeID = byNodeID

	var commandIDs []string
	for _, row := range a.rows {
		if row.State == store.BatchNodeRunning && row.CommandID != nil {
			commandIDs = append(commandIDs, *row.CommandID)
		}
	}
	canceled, err := cancelPendingCommands(tx, commandIDs, batchStoppedMessage, a.now)
	if err != nil {
		return err
	}
	grace := a.now.Add(a.rebootTimeout())
	for _, row := range a.rows {
		undelivered := row.State == store.BatchNodeRunning && row.CommandID != nil && canceled[*row.CommandID]
		switch {
		case row.State == store.BatchNodeWaiting || undelivered:
			a.settle(row, store.BatchNodeCanceled, batchStoppedMessage)
		case row.State == store.BatchNodeRunning && (row.Deadline == nil || row.Deadline.Before(grace)):
			row.Deadline = &grace
			a.changed[row] = true
		}
	}
	return nil
}

// cancelPendingCommands moves the commands among ids that are still Pending
// to Canceled, clears the lifecycle state they left pending on their nodes,
// and returns the IDs it canceled.
//
// The UPDATE carries `phase = Pending` for the same reason ClaimForDelivery
// does: a command another goroutine is claiming for delivery right now is won
// by exactly one of the two. A claim that lands first leaves no Pending row to
// cancel, so the command is delivered and reports its own result; a cancel
// that lands first leaves no Pending row for the claim's compare-and-set, so
// it is never pushed.
//
// A canceled reset or upgrade never runs, so nothing would resolve the pending
// state createCommands set for it, and the node's next unrelated active boot
// would report the operation done. settleOrphanedLifecycle clears that state
// unless another command still owns it.
func cancelPendingCommands(tx *gorm.DB, ids []string, reason string, now time.Time) (map[string]bool, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var canceled []store.NodeCommand
	if err := tx.Model(&canceled).
		Clauses(clause.Returning{Columns: []clause.Column{
			{Name: "id"}, {Name: "managed_node_id"}, {Name: "command"},
		}}).
		Where("id IN ? AND phase = ?", ids, store.CommandPending).
		Updates(map[string]any{
			"phase":        store.CommandCanceled,
			"result":       reason,
			"completed_at": now,
		}).Error; err != nil {
		return nil, err
	}
	resetNodes, upgradeNodes := lifecycleNodes(canceled)
	if err := lockNodes(tx, resetNodes, upgradeNodes); err != nil {
		return nil, err
	}
	if err := settleOrphanedLifecycle(tx, store.CmdReset, resetNodes,
		"reset_state", store.ResetStatePending, "reset_requested_at",
		map[string]any{"reset_state": "", "reset_requested_at": nil}); err != nil {
		return nil, err
	}
	if err := settleOrphanedLifecycle(tx, store.CmdUpgrade, upgradeNodes,
		"upgrade_state", store.UpgradeStatePending, "upgrade_requested_at",
		map[string]any{"upgrade_state": "", "upgrade_requested_at": nil}); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(canceled))
	for _, c := range canceled {
		out[c.ID] = true
	}
	return out, nil
}

// nodeBooted settles a rebooting row from the boot state its node came back
// with, and reports whether it changed the row. A node that comes back on
// passive or recovery fell back from a broken active image. A reboot is done
// on any other boot state, including one the agent does not report. A reset
// boots into autoreset first; the row keeps waiting for the next boot with a
// fresh deadline.
func (a *batchAdvance) nodeBooted(row *store.BatchNode, bootState string, bootedAt *time.Time) bool {
	if a.batch.Command == store.CmdReboot {
		if bootState == store.BootStatePassive || bootState == store.BootStateRecovery {
			a.settle(row, store.BatchNodeFailed, "fell back to "+bootState)
		} else {
			a.settle(row, store.BatchNodeDone, "")
		}
		return true
	}
	if a.batch.Command != store.CmdUpgrade && a.batch.Command != store.CmdReset {
		return false
	}
	switch bootState {
	case store.BootStateActive:
		a.settle(row, store.BatchNodeDone, "")
		return true
	case store.BootStatePassive, store.BootStateRecovery:
		a.settle(row, store.BatchNodeFailed, "fell back to "+bootState)
		return true
	case store.BootStateAutoReset:
		if a.batch.Command != store.CmdReset {
			return false
		}
		since := a.now
		if bootedAt != nil {
			since = *bootedAt
		}
		deadline := a.now.Add(a.rebootTimeout())
		row.RebootingSince = &since
		row.Deadline = &deadline
		a.changed[row] = true
		return true
	}
	return false
}

// holding counts the rows that hold a slot.
func (a *batchAdvance) holding() int {
	n := 0
	for _, row := range a.rows {
		if row.State == store.BatchNodeRunning || row.State == store.BatchNodeRebooting {
			n++
		}
	}
	return n
}

// fillSlots moves the first waiting rows whose node is online to running, as
// many as there are free slots, and returns the commands to create for them.
// Offline nodes stay waiting so a later slot can still start them.
func (a *batchAdvance) fillSlots() []*store.NodeCommand {
	free := a.batch.Concurrency - a.holding()
	if a.batch.Concurrency == 0 {
		free = len(a.rows)
	}
	var cmds []*store.NodeCommand
	for _, row := range a.rows {
		if free <= 0 {
			break
		}
		if row.State != store.BatchNodeWaiting || a.nodePhase[row.NodeID] != store.PhaseOnline {
			continue
		}
		cmd := &store.NodeCommand{
			ID:            uuid.New().String(),
			ManagedNodeID: row.NodeID,
			Command:       a.batch.Command,
			Args:          maps.Clone(a.batch.Args),
			Phase:         store.CommandPending,
			BatchID:       a.batch.ID,
		}
		if cmd.Command == store.CmdReset && a.resetTimeout > 0 {
			expires := a.now.Add(a.resetTimeout)
			cmd.ExpiresAt = &expires
		}
		cmds = append(cmds, cmd)
		row.State = store.BatchNodeRunning
		row.CommandID = &cmd.ID
		now := a.now
		deadline := now.Add(a.rebootTimeout())
		row.StartedAt = &now
		row.Deadline = &deadline
		a.changed[row] = true
		free--
	}
	return cmds
}

// skipOffline settles the waiting rows as skipped when no row holds a slot,
// which after fillSlots means every waiting node is offline.
func (a *batchAdvance) skipOffline() {
	if a.holding() > 0 {
		return
	}
	for _, row := range a.rows {
		if row.State == store.BatchNodeWaiting {
			a.settle(row, store.BatchNodeSkipped, "offline when the batch reached it")
		}
	}
}

// finish sets the outcome of a running batch and its finish time once every
// row is final. A stopped batch keeps its phase and gets its finish time when
// its last running node settles.
func (a *batchAdvance) finish(tx *gorm.DB) error {
	failed := false
	for _, row := range a.rows {
		if !store.IsFinalBatchNodeState(row.State) {
			return nil
		}
		if row.State == store.BatchNodeFailed || row.State == store.BatchNodeTimedOut {
			failed = true
		}
	}
	updates := map[string]any{}
	if a.batch.Phase == store.BatchRunning {
		updates["phase"] = store.BatchCompleted
		if failed {
			updates["phase"] = store.BatchFailed
		}
	}
	if a.batch.FinishedAt == nil {
		updates["finished_at"] = a.now
	}
	if len(updates) == 0 {
		return nil
	}
	return tx.Model(&store.CommandBatch{}).Where("id = ?", a.batch.ID).Updates(updates).Error
}

const batchStoppedMessage = "batch stopped"

// BatchStop stops a running batch by hand, as batchAdvance.stop describes.
// ErrBatchNotRunning when the batch is not running.
func (s *Store) BatchStop(ctx context.Context, batchID, reason string, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		b, err := lockBatch(tx, batchID)
		if err != nil {
			return err
		}
		if b.Phase != store.BatchRunning {
			return store.ErrBatchNotRunning
		}
		rows, err := batchRows(tx, batchID)
		if err != nil {
			return err
		}
		a := newBatchAdvance(b, rows, now)
		if err := a.stop(tx, reason, nil); err != nil {
			return err
		}
		if err := a.save(tx); err != nil {
			return err
		}
		return a.finish(tx)
	})
}

// bootSettles reports whether a node that booted at bootedAt went through the
// reboot its row has waited on since since. A nil bootedAt is a registration,
// which always follows a boot. The margin keeps the boot before the reboot,
// computed from a whole-second uptime, from passing as a new one.
func bootSettles(since, bootedAt *time.Time) bool {
	if bootedAt == nil || since == nil {
		return true
	}
	return bootedAt.After(since.Add(5 * time.Second))
}

// BatchNodeBooted settles the rebooting rows of a node that came back and
// returns the IDs of the batches it changed.
//
// Every heartbeat calls it, so it first looks for a row the boot settles with
// a plain read and returns without opening a write transaction when there is
// none. The batches are then locked in ID order, the same order for every
// caller, and each row is checked again under its batch lock.
func (s *Store) BatchNodeBooted(ctx context.Context, nodeID, bootState string, bootedAt *time.Time, now time.Time) ([]string, error) {
	var candidates []*store.BatchNode
	if err := s.db.WithContext(ctx).
		Where("node_id = ? AND state = ?", nodeID, store.BatchNodeRebooting).
		Order("batch_id ASC").Find(&candidates).Error; err != nil {
		return nil, err
	}
	var batchIDs []string
	for _, c := range candidates {
		if bootSettles(c.RebootingSince, bootedAt) {
			batchIDs = append(batchIDs, c.BatchID)
		}
	}
	if len(batchIDs) == 0 {
		return nil, nil
	}
	var touched []string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		touched = nil
		for _, batchID := range batchIDs {
			changed, err := nodeBootedInBatch(tx, batchID, nodeID, bootState, bootedAt, now)
			if err != nil {
				return err
			}
			if changed {
				touched = append(touched, batchID)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return touched, nil
}

func nodeBootedInBatch(tx *gorm.DB, batchID, nodeID, bootState string, bootedAt *time.Time, now time.Time) (bool, error) {
	b, err := lockBatch(tx, batchID)
	if err != nil {
		return false, err
	}
	rows, err := batchRows(tx, batchID)
	if err != nil {
		return false, err
	}
	a := newBatchAdvance(b, rows, now)
	var row *store.BatchNode
	for _, r := range rows {
		if r.NodeID == nodeID {
			row = r
		}
	}
	if row == nil || row.State != store.BatchNodeRebooting || !bootSettles(row.RebootingSince, bootedAt) {
		return false, nil
	}
	if !a.nodeBooted(row, bootState, bootedAt) {
		return false, nil
	}
	if err := a.stopOnFailure(tx); err != nil {
		return false, err
	}
	if err := a.save(tx); err != nil {
		return false, err
	}
	return true, a.finish(tx)
}

// BatchActiveIDs returns the batches that are running or still have a node
// running or rebooting, oldest first.
func (s *Store) BatchActiveIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := s.db.WithContext(ctx).Model(&store.CommandBatch{}).
		Where("phase = ? OR id IN (SELECT batch_id FROM command_batch_nodes WHERE state IN ?)",
			store.BatchRunning, []string{store.BatchNodeRunning, store.BatchNodeRebooting}).
		Order("created_at ASC, id ASC").
		Pluck("id", &ids).Error
	return ids, err
}

// BatchPostponeExpiredDeadlines gives every running or rebooting row whose
// deadline passed before now a new deadline of now plus its batch's timeout.
// A node cannot report its command or its boot, nor send heartbeats, while
// AuroraBoot is down, so the time it was down does not count against the node.
//
// Deadlines are compared in Go: SQLite stores times as text, so a comparison
// in SQL depends on how each value was formatted.
func (s *Store) BatchPostponeExpiredDeadlines(ctx context.Context, now time.Time) error {
	inFlight := []string{store.BatchNodeRunning, store.BatchNodeRebooting}
	var waiting []*store.BatchNode
	if err := s.db.WithContext(ctx).Where("state IN ?", inFlight).
		Order("batch_id ASC").Find(&waiting).Error; err != nil {
		return err
	}
	var batchIDs []string
	for _, row := range waiting {
		if row.Deadline != nil && row.Deadline.Before(now) && !slices.Contains(batchIDs, row.BatchID) {
			batchIDs = append(batchIDs, row.BatchID)
		}
	}
	for _, batchID := range batchIDs {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			b, err := lockBatch(tx, batchID)
			if err != nil {
				return err
			}
			rows, err := batchRows(tx, batchID)
			if err != nil {
				return err
			}
			deadline := now.Add(rebootTimeout(b.RebootTimeoutSeconds))
			for _, row := range rows {
				if !slices.Contains(inFlight, row.State) || row.Deadline == nil || !row.Deadline.Before(now) {
					continue
				}
				if err := tx.Model(&store.BatchNode{}).
					Where("batch_id = ? AND node_id = ?", row.BatchID, row.NodeID).
					Update("deadline", deadline).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
