package handlers_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// fakeNodeStore implements store.NodeStore for testing.
type fakeNodeStore struct {
	mu                 sync.Mutex
	nodes              []*store.ManagedNode
	failResetBeforeErr error
	// failResetBeforeCalls counts FailResetBefore calls so a spec can assert the
	// handler never even attempted the transition, independently of whether the
	// fake's own CAS would have refused it.
	failResetBeforeCalls int
	// stampResetProgressCalls counts StampResetProgress calls, for the same
	// reason: a delivery path that forgets to re-anchor must be visible even
	// when the node's state would make the stamp a no-op.
	stampResetProgressCalls int
}

func (f *fakeNodeStore) Register(_ context.Context, n *store.ManagedNode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodes = append(f.nodes, n)
	return nil
}

func (f *fakeNodeStore) GetByID(_ context.Context, id string) (*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeNodeStore) GetByMachineID(_ context.Context, mid string) (*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.MachineID == mid {
			return n, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeNodeStore) GetByAPIKey(_ context.Context, key string) (*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.APIKey == key {
			return n, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeNodeStore) List(_ context.Context) ([]*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nodes, nil
}

func (f *fakeNodeStore) ListByGroup(_ context.Context, groupID string) ([]*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []*store.ManagedNode
	for _, n := range f.nodes {
		if inGroup(n, groupID) {
			result = append(result, n)
		}
	}
	return result, nil
}

func (f *fakeNodeStore) ListByLabels(_ context.Context, labels map[string]string) ([]*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []*store.ManagedNode
	for _, n := range f.nodes {
		match := true
		for k, v := range labels {
			if n.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			result = append(result, n)
		}
	}
	return result, nil
}

func (f *fakeNodeStore) ListBySelector(_ context.Context, sel store.CommandSelector) ([]*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(sel.NodeIDs) > 0 {
		var result []*store.ManagedNode
		for _, n := range f.nodes {
			for _, id := range sel.NodeIDs {
				if n.ID == id {
					result = append(result, n)
				}
			}
		}
		return result, nil
	}
	if sel.GroupID != "" {
		var result []*store.ManagedNode
		for _, n := range f.nodes {
			if inGroup(n, sel.GroupID) {
				result = append(result, n)
			}
		}
		return result, nil
	}
	return f.nodes, nil
}

func (f *fakeNodeStore) UpdateHeartbeat(_ context.Context, id string, agentVersion string, osRelease map[string]string, addresses []store.NodeAddress, bootState string, hostname string, remoteIP string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID == id {
			n.AgentVersion = agentVersion
			n.OSRelease = osRelease
			// Mirror the gorm store: only overwrite when the heartbeat carries them.
			if addresses != nil {
				n.Addresses = addresses
			}
			if bootState != "" {
				n.BootState = bootState
			}
			if hostname != "" {
				n.Hostname = hostname
			}
			if remoteIP != "" {
				n.RemoteIP = remoteIP
			}
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeNodeStore) UpdatePhase(_ context.Context, id string, phase string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID == id {
			n.Phase = phase
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeNodeStore) SetGroup(_ context.Context, nodeID string, groupID *string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID == nodeID {
			n.GroupID = groupID
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeNodeStore) SetLabels(_ context.Context, nodeID string, labels map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID == nodeID {
			n.Labels = labels
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeNodeStore) ClaimNode(_ context.Context, groupID, claimKey string) (*store.ManagedNode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Idempotent replay: this key already owns a node in the group.
	for _, n := range f.nodes {
		if inGroup(n, groupID) && n.ClaimKey != nil && *n.ClaimKey == claimKey {
			return n, nil
		}
	}
	// Claim the first unclaimed node in the group.
	for _, n := range f.nodes {
		if inGroup(n, groupID) && n.ClaimKey == nil {
			key := claimKey
			now := time.Now()
			n.ClaimKey = &key
			n.ClaimedAt = &now
			return n, nil
		}
	}
	return nil, store.ErrNoClaimCapacity
}

func (f *fakeNodeStore) ReleaseNode(_ context.Context, nodeID, claimKey string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID == nodeID && n.ClaimKey != nil && *n.ClaimKey == claimKey {
			n.ClaimKey = nil
			n.ClaimedAt = nil
			return true, nil
		}
	}
	return false, nil
}

// markResetPending is the reset half of what the gorm CreateBatch does to a
// target node: a reset that is already pending or in progress keeps its state
// and request time.
func (f *fakeNodeStore) markResetPending(nodeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID == nodeID {
			if n.ResetState == store.ResetStatePending || n.ResetState == store.ResetStateInProgress {
				return nil
			}
			now := time.Now()
			n.ResetState = store.ResetStatePending
			n.ResetRequestedAt = &now
			// Mirrors the real store: a new reset never inherits the previous
			// one's progress stamp.
			n.ResetProgressAt = nil
			return nil
		}
	}
	return fmt.Errorf("not found")
}
func (f *fakeNodeStore) AdvanceReset(_ context.Context, nodeID string, fromStates []string, to string, stampLastReset bool) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID != nodeID {
			continue
		}
		for _, from := range fromStates {
			if n.ResetState == from {
				n.ResetState = to
				if stampLastReset {
					now := time.Now()
					n.LastReset = &now
				}
				// Mirrors the real store: reaching in-progress means the node
				// re-registered, which re-anchors the expiry budget.
				if to == store.ResetStateInProgress {
					now := time.Now()
					n.ResetProgressAt = &now
				}
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}
func (f *fakeNodeStore) StampResetProgress(_ context.Context, nodeID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stampResetProgressCalls++
	for _, n := range f.nodes {
		if n.ID != nodeID {
			continue
		}
		// Mirrors the real store's CAS: only an in-flight reset is re-anchored,
		// so a delivery arriving after the lifecycle resolved changes nothing.
		if n.ResetState != store.ResetStatePending && n.ResetState != store.ResetStateInProgress {
			return false, nil
		}
		now := time.Now()
		n.ResetProgressAt = &now
		return true, nil
	}
	return false, nil
}
func (f *fakeNodeStore) FailResetBefore(_ context.Context, nodeID string, deadline time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failResetBeforeCalls++
	if f.failResetBeforeErr != nil {
		return false, f.failResetBeforeErr
	}
	for _, n := range f.nodes {
		if n.ID != nodeID || n.ResetRequestedAt == nil || n.ResetRequestedAt.After(deadline) {
			continue
		}
		// Mirrors the real store's second CAS predicate: a node that re-registered
		// after the deadline has a fresh budget and must not be failed.
		if n.ResetProgressAt != nil && n.ResetProgressAt.After(deadline) {
			continue
		}
		if n.ResetState == store.ResetStatePending || n.ResetState == store.ResetStateInProgress {
			n.ResetState = store.ResetStateFailed
			return true, nil
		}
	}
	return false, nil
}

// markUpgradePending is the upgrade half of what the gorm CreateBatch does to a
// target node: an upgrade that is already pending keeps its request time.
func (f *fakeNodeStore) markUpgradePending(nodeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID == nodeID {
			if n.UpgradeState == store.UpgradeStatePending {
				return nil
			}
			now := time.Now()
			n.UpgradeState = store.UpgradeStatePending
			n.UpgradeRequestedAt = &now
			return nil
		}
	}
	return fmt.Errorf("not found")
}
func (f *fakeNodeStore) SettleAfterReboot(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

func (f *fakeNodeStore) AdvanceUpgrade(_ context.Context, nodeID string, fromStates []string, to string, stampLastUpgrade bool) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.nodes {
		if n.ID != nodeID {
			continue
		}
		for _, from := range fromStates {
			if n.UpgradeState == from {
				n.UpgradeState = to
				if stampLastUpgrade {
					now := time.Now()
					n.LastUpgrade = &now
				}
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}
func (f *fakeNodeStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, n := range f.nodes {
		if n.ID == id {
			f.nodes = append(f.nodes[:i], f.nodes[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

// fakeCommandStore implements store.CommandStore for testing.
type fakeCommandStore struct {
	mu   sync.Mutex
	cmds []*store.NodeCommand
	// nodes, when set, receives the lifecycle marking CreateBatch does in the
	// gorm store's transaction. Left nil, the fake shows whether the handler
	// marks the lifecycle on its own.
	nodes *fakeNodeStore
}

func (f *fakeCommandStore) Create(_ context.Context, cmd *store.NodeCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, cmd)
	return nil
}

func (f *fakeCommandStore) CreateBatch(ctx context.Context, cmds []*store.NodeCommand) error {
	f.mu.Lock()
	f.cmds = append(f.cmds, cmds...)
	f.mu.Unlock()
	if f.nodes == nil {
		return nil
	}
	for _, cmd := range cmds {
		switch cmd.Command {
		case store.CmdReset:
			_ = f.nodes.markResetPending(cmd.ManagedNodeID)
		case store.CmdUpgrade:
			_ = f.nodes.markUpgradePending(cmd.ManagedNodeID)
		}
	}
	return nil
}

func (f *fakeCommandStore) GetByID(_ context.Context, id string) (*store.NodeCommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, cmd := range f.cmds {
		if cmd.ID == id {
			return cmd, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeCommandStore) GetPending(_ context.Context, nodeID string) ([]*store.NodeCommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []*store.NodeCommand
	for _, cmd := range f.cmds {
		if cmd.ManagedNodeID != nodeID || cmd.Phase != store.CommandPending {
			continue
		}
		// Mirror the real query's deadline clause. Without it the fake hands
		// back commands the gorm store would never deliver, and any handler
		// that relies on an overdue command staying undelivered passes here
		// while failing in production.
		if commandExpired(cmd) {
			continue
		}
		// Return a copy, not the stored pointer: real gorm GetPending
		// materializes a fresh struct per query, so callers (e.g. concurrent
		// polls in GetCommands) must each get their own object to mutate.
		cp := *cmd
		result = append(result, &cp)
	}
	return result, nil
}

// commandExpired mirrors the `expires_at IS NULL OR expires_at > now` predicate
// the real store applies in both GetPending and ClaimForDelivery. Without it the
// fakes would hand out an expired reset command and any handler-level test of
// the expiry gate would pass whether or not the gate works.
func commandExpired(cmd *store.NodeCommand) bool {
	return cmd.ExpiresAt != nil && !cmd.ExpiresAt.After(time.Now())
}

func (f *fakeCommandStore) MarkDelivered(_ context.Context, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, cmd := range f.cmds {
		for _, id := range ids {
			if cmd.ID == id {
				cmd.Phase = store.CommandDelivered
			}
		}
	}
	return nil
}

func (f *fakeCommandStore) ClaimForDelivery(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, cmd := range f.cmds {
		if cmd.ID == id && cmd.Phase == store.CommandPending && !commandExpired(cmd) {
			cmd.Phase = store.CommandDelivered
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeCommandStore) UpdateStatus(_ context.Context, id string, phase string, result string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, cmd := range f.cmds {
		if cmd.ID == id {
			return applyFakeStatus(cmd, phase, result)
		}
	}
	return store.ErrCommandNotFound
}

// applyFakeStatus models the gorm store's terminal guard: a finished command
// keeps its phase and result, and repeating the phase it ended in is a no-op.
func applyFakeStatus(cmd *store.NodeCommand, phase, result string) error {
	if store.IsTerminalCommandPhase(cmd.Phase) {
		if cmd.Phase == phase {
			return nil
		}
		return store.ErrCommandTerminal
	}
	cmd.Phase = phase
	cmd.Result = result
	return nil
}

func (f *fakeCommandStore) UpdateStatusForNode(_ context.Context, id string, nodeID string, phase string, result string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, cmd := range f.cmds {
		if cmd.ID == id && cmd.ManagedNodeID == nodeID {
			return applyFakeStatus(cmd, phase, result)
		}
	}
	return store.ErrCommandNotFound
}

func (f *fakeCommandStore) ListByNode(_ context.Context, nodeID string) ([]*store.NodeCommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []*store.NodeCommand
	for _, cmd := range f.cmds {
		if cmd.ManagedNodeID == nodeID {
			result = append(result, cmd)
		}
	}
	return result, nil
}

func (f *fakeCommandStore) ExpireBefore(_ context.Context, nodeID string, deadline time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for _, cmd := range f.cmds {
		if cmd.ManagedNodeID != nodeID || cmd.ExpiresAt == nil || cmd.ExpiresAt.After(deadline) {
			continue
		}
		switch cmd.Phase {
		case store.CommandPending, store.CommandDelivered, store.CommandRunning:
			cmd.Phase = store.CommandExpired
			cmd.CompletedAt = &now
		}
	}
	return nil
}

func (f *fakeCommandStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, cmd := range f.cmds {
		if cmd.ID == id {
			f.cmds = append(f.cmds[:i], f.cmds[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeCommandStore) DeleteTerminal(_ context.Context, nodeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var remaining []*store.NodeCommand
	for _, cmd := range f.cmds {
		if cmd.ManagedNodeID == nodeID &&
			(cmd.Phase == store.CommandCompleted || cmd.Phase == store.CommandFailed || cmd.Phase == store.CommandExpired) {
			continue
		}
		remaining = append(remaining, cmd)
	}
	f.cmds = remaining
	return nil
}

// fakeArtifactStore implements store.ArtifactStore for testing.
type fakeArtifactStore struct {
	mu      sync.Mutex
	records []*store.ArtifactRecord
	// getErr, when set, overrides the natural GetByID result so tests can
	// exercise handler paths that must survive a transient store failure
	// (e.g. Cancel must not silently 404 on a DB blip).
	getErr    error
	createErr error
}

func (f *fakeArtifactStore) Create(_ context.Context, rec *store.ArtifactRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.records = append(f.records, rec)
	return nil
}

func (f *fakeArtifactStore) GetByID(_ context.Context, id string) (*store.ArtifactRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	for _, r := range f.records {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeArtifactStore) List(_ context.Context) ([]*store.ArtifactRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.records, nil
}

func (f *fakeArtifactStore) Update(_ context.Context, rec *store.ArtifactRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.records {
		if r.ID == rec.ID {
			f.records[i] = rec
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeArtifactStore) UpdatePhaseMessage(_ context.Context, id, phase, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.records {
		if r.ID == id {
			r.Phase = phase
			r.Message = message
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeArtifactStore) UpdateFiles(_ context.Context, id string, files []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.records {
		if r.ID == id {
			r.ArtifactFiles = files
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeArtifactStore) UpdateNameSaved(_ context.Context, id string, name *string, saved *bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.records {
		if r.ID == id {
			if name != nil {
				r.Name = *name
			}
			if saved != nil {
				r.Saved = *saved
			}
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeArtifactStore) ClearUploadToken(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.records {
		if r.ID == id {
			r.UploadToken = ""
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeArtifactStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.records {
		if r.ID == id {
			f.records = append(f.records[:i], f.records[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeArtifactStore) DeleteByPhase(_ context.Context, phase string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var remaining []*store.ArtifactRecord
	for _, r := range f.records {
		if r.Phase != phase {
			remaining = append(remaining, r)
		}
	}
	f.records = remaining
	return nil
}

func (f *fakeArtifactStore) GetLogs(_ context.Context, id string) (string, error) {
	return "", nil
}

func (f *fakeArtifactStore) AppendLog(_ context.Context, id string, text string) error {
	return nil
}

// fakeGroupStore implements store.GroupStore for testing.
type fakeGroupStore struct {
	mu       sync.Mutex
	groups   []*store.NodeGroup
	counts   map[string]int
	countErr error
}

func (f *fakeGroupStore) NodeCounts(_ context.Context) (map[string]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.countErr != nil {
		return nil, f.countErr
	}
	out := make(map[string]int, len(f.counts))
	for k, v := range f.counts {
		out[k] = v
	}
	return out, nil
}

func (f *fakeGroupStore) Create(_ context.Context, g *store.NodeGroup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groups = append(f.groups, g)
	return nil
}

func (f *fakeGroupStore) GetByID(_ context.Context, id string) (*store.NodeGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.groups {
		if g.ID == id {
			return g, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeGroupStore) GetByName(_ context.Context, name string) (*store.NodeGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.groups {
		if g.Name == name {
			return g, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeGroupStore) List(_ context.Context) ([]*store.NodeGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.groups, nil
}

func (f *fakeGroupStore) Update(_ context.Context, group *store.NodeGroup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, g := range f.groups {
		if g.ID == group.ID {
			f.groups[i] = group
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (f *fakeGroupStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, g := range f.groups {
		if g.ID == id {
			f.groups = append(f.groups[:i], f.groups[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

// fakeBuilder implements builder.ArtifactBuilder for testing.
type fakeBuilder struct {
	mu       sync.Mutex
	builds   []*builder.BuildStatus
	lastOpts builder.BuildOptions // lets tests assert on the generated cloud-config
	// buildErr, statusErr, listErr, cancelErr — when set, are returned from
	// the matching method instead of the default success behaviour. Tests use
	// them to exercise the handler's error-to-status mapping (including the
	// ErrNotSupported → 501 route) without a real builder.
	buildErr  error
	statusErr error
	listErr   error
	cancelErr error
	// cancelledIDs records every id Cancel was called with so tests can
	// assert the handler forwards cancellations to the backend, including
	// for terminal-phase artifacts where cleanup used to be skipped.
	cancelledIDs []string
}

func (f *fakeBuilder) Build(_ context.Context, opts builder.BuildOptions) (*builder.BuildStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastOpts = opts
	if f.buildErr != nil {
		// Mirror the real builder: validation failures are rejected before any
		// build state is recorded.
		return nil, f.buildErr
	}
	status := &builder.BuildStatus{
		ID:    opts.ID,
		Phase: builder.BuildPending,
	}
	f.builds = append(f.builds, status)
	return status, nil
}

func (f *fakeBuilder) Status(_ context.Context, id string) (*builder.BuildStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	for _, b := range f.builds {
		if b.ID == id {
			return b, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeBuilder) List(_ context.Context) ([]*builder.BuildStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.builds, nil
}

func (f *fakeBuilder) Cancel(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelledIDs = append(f.cancelledIDs, id)
	return f.cancelErr
}

// fakeSettingsStore implements store.SettingsStore for testing.
type fakeSettingsStore struct {
	mu     sync.Mutex
	values map[string]string
	getErr error // when set, Get returns this error (to exercise fail-closed paths)
	setErr error // when set, Set returns this error
	allErr error // when set, GetAll returns this error
}

func newFakeSettingsStore() *fakeSettingsStore {
	return &fakeSettingsStore{values: map[string]string{}}
}

func (f *fakeSettingsStore) Get(_ context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return "", false, f.getErr
	}
	v, ok := f.values[key]
	return v, ok, nil
}

func (f *fakeSettingsStore) Set(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = value
	return nil
}

func (f *fakeSettingsStore) GetAll(_ context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.allErr != nil {
		return nil, f.allErr
	}
	out := make(map[string]string, len(f.values))
	for k, v := range f.values {
		out[k] = v
	}
	return out, nil
}

// fakeSecureBootKeySetStore implements store.SecureBootKeySetStore for testing.
type fakeSecureBootKeySetStore struct {
	mu      sync.Mutex
	keySets []*store.SecureBootKeySet
	nextID  int
}

func (f *fakeSecureBootKeySetStore) Create(_ context.Context, ks *store.SecureBootKeySet) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.keySets {
		if existing.Name == ks.Name {
			return fmt.Errorf("duplicate name %q", ks.Name)
		}
	}
	f.nextID++
	ks.ID = fmt.Sprintf("ks-%d", f.nextID)
	f.keySets = append(f.keySets, ks)
	return nil
}

func (f *fakeSecureBootKeySetStore) GetByID(_ context.Context, id string) (*store.SecureBootKeySet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ks := range f.keySets {
		if ks.ID == id {
			return ks, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (f *fakeSecureBootKeySetStore) GetByName(_ context.Context, name string) (*store.SecureBootKeySet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ks := range f.keySets {
		if ks.Name == name {
			return ks, nil
		}
	}
	return nil, nil
}

func (f *fakeSecureBootKeySetStore) List(_ context.Context) ([]*store.SecureBootKeySet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keySets, nil
}

func (f *fakeSecureBootKeySetStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, ks := range f.keySets {
		if ks.ID == id {
			f.keySets = append(f.keySets[:i], f.keySets[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

// --- fakeNodeExtensionStore --------------------------------------------

type fakeNodeExtensionStore struct {
	mu   sync.Mutex
	rows []store.NodeExtensionRow
}

func newFakeNodeExtensionStore() *fakeNodeExtensionStore {
	return &fakeNodeExtensionStore{}
}

func (f *fakeNodeExtensionStore) Upsert(_ context.Context, row *store.NodeExtensionRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, r := range f.rows {
		if r.NodeID == row.NodeID && r.Name == row.Name && r.Type == row.Type && r.BootState == row.BootState {
			f.rows[i] = *row
			return nil
		}
	}
	f.rows = append(f.rows, *row)
	return nil
}

func (f *fakeNodeExtensionStore) ListForNode(_ context.Context, nodeID string) ([]store.NodeExtensionRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []store.NodeExtensionRow{}
	for _, r := range f.rows {
		if r.NodeID == nodeID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeNodeExtensionStore) ListForExtensionByName(_ context.Context, extType, name string) ([]store.NodeExtensionRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []store.NodeExtensionRow{}
	for _, r := range f.rows {
		if r.Type == extType && r.Name == name {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeNodeExtensionStore) DeleteByScope(_ context.Context, nodeID, extType, name, bootState string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.rows[:0]
	for _, r := range f.rows {
		if r.NodeID == nodeID && r.Type == extType && r.Name == name && r.BootState == bootState {
			continue
		}
		out = append(out, r)
	}
	f.rows = out
	return nil
}

func (f *fakeNodeExtensionStore) DeleteByName(_ context.Context, nodeID, extType, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.rows[:0]
	for _, r := range f.rows {
		if r.NodeID == nodeID && r.Type == extType && r.Name == name {
			continue
		}
		out = append(out, r)
	}
	f.rows = out
	return nil
}

// --- fakeExtensionBuilder -----------------------------------------------

type fakeExtensionBuilder struct {
	mu       sync.Mutex
	lastOpts builder.ExtensionBuildOptions
	buildErr error
	cancels  []string
}

func (f *fakeExtensionBuilder) Build(_ context.Context, opts builder.ExtensionBuildOptions) (*builder.ExtensionBuildStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastOpts = opts
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	return &builder.ExtensionBuildStatus{ID: opts.ID, Phase: builder.BuildPending}, nil
}
func (f *fakeExtensionBuilder) Status(_ context.Context, id string) (*builder.ExtensionBuildStatus, error) {
	return &builder.ExtensionBuildStatus{ID: id, Phase: builder.BuildReady}, nil
}
func (f *fakeExtensionBuilder) List(_ context.Context) ([]*builder.ExtensionBuildStatus, error) {
	return nil, nil
}
func (f *fakeExtensionBuilder) Cancel(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancels = append(f.cancels, id)
	return nil
}

// --- fakeExtensionStore -------------------------------------------------

type fakeExtensionStore struct {
	mu   sync.Mutex
	rows map[string]*store.ExtensionRecord
}

func newFakeExtensionStore() *fakeExtensionStore {
	return &fakeExtensionStore{rows: map[string]*store.ExtensionRecord{}}
}

func (f *fakeExtensionStore) Create(_ context.Context, r *store.ExtensionRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *r
	f.rows[r.ID] = &cp
	return nil
}
func (f *fakeExtensionStore) GetByID(_ context.Context, id string) (*store.ExtensionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rows[id]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeExtensionStore) List(context.Context) ([]store.ExtensionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.ExtensionRecord, 0, len(f.rows))
	for _, r := range f.rows {
		out = append(out, *r)
	}
	return out, nil
}
func (f *fakeExtensionStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}
func (f *fakeExtensionStore) FindLatestReadyByName(_ context.Context, extType, name string) (*store.ExtensionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var newest *store.ExtensionRecord
	for _, r := range f.rows {
		if r.Type != extType || r.Name != name || r.Phase != "Ready" {
			continue
		}
		if newest == nil || r.CreatedAt.After(newest.CreatedAt) {
			cp := *r
			newest = &cp
		}
	}
	if newest == nil {
		return nil, fmt.Errorf("not found")
	}
	return newest, nil
}
func (f *fakeExtensionStore) FindByNameAndVersion(_ context.Context, extType, name, version string) (*store.ExtensionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.Type == extType && r.Name == name && r.Version == version && r.Phase == "Ready" {
			cp := *r
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeExtensionStore) AppendLog(_ context.Context, id, chunk string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r, ok := f.rows[id]; ok {
		r.Logs += chunk
	}
	return nil
}

// --- fakeBundleStore ----------------------------------------------------

type fakeBundleStore struct {
	mu     sync.Mutex
	rowsBy map[string][]store.ArtifactExtensionBundle // keyed by artifact id
	byName map[string][]string                        // extension name -> artifact ids
}

func newFakeBundleStore() *fakeBundleStore {
	return &fakeBundleStore{
		rowsBy: map[string][]store.ArtifactExtensionBundle{},
		byName: map[string][]string{},
	}
}

func (f *fakeBundleStore) ListForArtifact(_ context.Context, artifactID string) ([]store.ArtifactExtensionBundle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.ArtifactExtensionBundle(nil), f.rowsBy[artifactID]...), nil
}
func (f *fakeBundleStore) ReplaceForArtifact(_ context.Context, artifactID string, entries []store.ArtifactExtensionBundle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, old := range f.rowsBy[artifactID] {
		f.removeNameMapLocked(old.ExtensionName, artifactID)
	}
	f.rowsBy[artifactID] = append([]store.ArtifactExtensionBundle(nil), entries...)
	for _, e := range entries {
		f.byName[e.ExtensionName] = append(f.byName[e.ExtensionName], artifactID)
	}
	return nil
}
func (f *fakeBundleStore) ArtifactsReferencingExtension(_ context.Context, name string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.byName[name]...), nil
}
func (f *fakeBundleStore) removeNameMapLocked(name, artifactID string) {
	ids := f.byName[name]
	out := ids[:0]
	for _, id := range ids {
		if id != artifactID {
			out = append(out, id)
		}
	}
	f.byName[name] = out
}

// inGroup reports whether n belongs to the group with the given id.
func inGroup(n *store.ManagedNode, groupID string) bool {
	return n.GroupID != nil && *n.GroupID == groupID
}
