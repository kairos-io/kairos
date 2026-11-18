package gorm

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"strings"

	"github.com/google/uuid"
	"github.com/kairos-io/AuroraBoot/internal/secrets"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store implements GroupStore, NodeStore, and CommandStore using GORM.
// Supports SQLite (default) and PostgreSQL.
type Store struct {
	db *gorm.DB
	// cipher encrypts/decrypts BMCTarget.Password at rest. Optional: when nil the
	// password is stored as-is (used by tests and pre-encryption setups). The web
	// server always wires one via WithCipher.
	cipher *secrets.Cipher
	// resetTimeout is the delivery expiry of a reset command a batch creates,
	// the same window the reset command handler gives a single-node reset. A
	// negative or zero value creates them without one.
	resetTimeout time.Duration
}

// WithCipher attaches a DEK-backed cipher used to encrypt the BMCTarget password
// column at rest. It returns the same Store for chaining. Passing nil is a no-op
// that leaves passwords stored as-is.
func (s *Store) WithCipher(c *secrets.Cipher) *Store {
	s.cipher = c
	return s
}

// WithResetTimeout sets the delivery expiry of the reset commands batches
// create, so an offline node does not run a reset after its reset lifecycle
// has already timed out. Zero or a negative duration creates them without one.
func (s *Store) WithResetTimeout(d time.Duration) *Store {
	s.resetTimeout = d
	return s
}

// New creates a new Store with the given DSN and auto-migrates all models.
// The DSN format determines the database driver:
//   - "postgres://..." or "host=..." → PostgreSQL
//   - anything else → SQLite (file path or ":memory:")
func New(dsn string) (*Store, error) {
	dialector := openDialector(dsn)
	// TranslateError makes GORM return its portable sentinel errors (e.g.
	// gorm.ErrDuplicatedKey, gorm.ErrRecordNotFound) instead of raw driver
	// errors, so store code can branch on them without SQLite/PostgreSQL string
	// matching. ClaimNode relies on ErrDuplicatedKey to arbitrate a concurrent
	// same-key claim against the (group_id, claim_key) unique index.
	db, err := gorm.Open(dialector, &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// SQLite-specific: enable WAL mode for concurrent access
	if !isPostgres(dsn) {
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("getting sql.DB: %w", err)
		}
		if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
			return nil, fmt.Errorf("enabling WAL mode: %w", err)
		}
		// Make concurrent writers wait-and-retry for up to 5s when the database
		// is locked, instead of failing immediately with "database is locked".
		// WAL allows concurrent readers but still serializes writers, so without
		// this any overlapping write (common under the WebSocket node manager)
		// would error out.
		if _, err := sqlDB.Exec("PRAGMA busy_timeout=5000"); err != nil {
			return nil, fmt.Errorf("setting busy_timeout: %w", err)
		}
	}

	if err := db.AutoMigrate(&store.NodeGroup{}, &store.ManagedNode{}, &store.NodeCommand{}, &store.ArtifactRecord{}, &store.SecureBootKeySet{}, &store.BMCTarget{}, &store.Deployment{}, &store.Setting{}, &store.ExtensionRecord{}, &store.ArtifactExtensionBundle{}, &store.NodeExtensionRow{}, &store.CommandBatch{}, &store.BatchNode{}); err != nil {
		return nil, fmt.Errorf("auto-migrating: %w", err)
	}

	// An ungrouped node has a NULL group_id, which the foreign key to
	// node_groups accepts. SQLite does not enforce that key, so a database
	// can hold ungrouped nodes with an empty group_id instead.
	if err := db.Model(&store.ManagedNode{}).Where("group_id = ''").
		Update("group_id", nil).Error; err != nil {
		return nil, fmt.Errorf("clearing empty group ids: %w", err)
	}

	return &Store{db: db}, nil
}

func isPostgres(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") ||
		strings.HasPrefix(dsn, "postgresql://") ||
		strings.HasPrefix(dsn, "host=")
}

func openDialector(dsn string) gorm.Dialector {
	if isPostgres(dsn) {
		return postgres.Open(dsn)
	}
	return sqlite.Open(dsn)
}

// --- GroupStore ---

func (s *Store) Create(ctx context.Context, group *store.NodeGroup) error {
	group.ID = uuid.New().String()
	return groupWriteError(s.db.WithContext(ctx).Create(group).Error)
}

func (s *Store) GetByID(ctx context.Context, id string) (*store.NodeGroup, error) {
	var g store.NodeGroup
	if err := s.db.WithContext(ctx).First(&g, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *Store) GetByName(ctx context.Context, name string) (*store.NodeGroup, error) {
	var g store.NodeGroup
	if err := s.db.WithContext(ctx).First(&g, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *Store) List(ctx context.Context) ([]*store.NodeGroup, error) {
	var groups []*store.NodeGroup
	if err := s.db.WithContext(ctx).Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

// GroupNodeCounts returns the number of nodes per group ID in one grouped
// query. Ungrouped nodes and groups with no nodes are not in the map.
func (s *Store) GroupNodeCounts(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		GroupID string
		Count   int
	}
	if err := s.db.WithContext(ctx).Model(&store.ManagedNode{}).
		Select("group_id, COUNT(*) AS count").
		Where("group_id IS NOT NULL").
		Group("group_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[r.GroupID] = r.Count
	}
	return counts, nil
}

func (s *Store) Update(ctx context.Context, group *store.NodeGroup) error {
	return groupWriteError(s.db.WithContext(ctx).Save(group).Error)
}

// groupWriteError maps a unique-name violation to store.ErrGroupNameTaken.
// The name index is the only unique constraint a caller can hit, as the ID is
// generated by the store.
func groupWriteError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return store.ErrGroupNameTaken
	}
	return err
}

// Delete removes a group and detaches any member nodes. The GORM tag on
// ManagedNode.Group sets up OnDelete:SET NULL, but SQLite doesn't enforce
// foreign keys unless PRAGMA foreign_keys=ON is set per-connection, so we
// nullify node.group_id explicitly in a transaction to behave the same on
// SQLite and Postgres.
func (s *Store) Delete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&store.ManagedNode{}).
			Where("group_id = ?", id).
			Update("group_id", nil).Error; err != nil {
			return err
		}
		return tx.Delete(&store.NodeGroup{}, "id = ?", id).Error
	})
}

// --- NodeStore ---

func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Store) Register(ctx context.Context, node *store.ManagedNode) error {
	node.ID = uuid.New().String()
	apiKey, err := generateAPIKey()
	if err != nil {
		return fmt.Errorf("generating API key: %w", err)
	}
	node.APIKey = apiKey
	node.Phase = store.PhaseRegistered
	return s.db.WithContext(ctx).Create(node).Error
}

func (s *Store) NodeGetByID(ctx context.Context, id string) (*store.ManagedNode, error) {
	var n store.ManagedNode
	if err := s.db.WithContext(ctx).Preload("Group").First(&n, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) GetByMachineID(ctx context.Context, machineID string) (*store.ManagedNode, error) {
	var n store.ManagedNode
	if err := s.db.WithContext(ctx).Preload("Group").First(&n, "machine_id = ?", machineID).Error; err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) GetByAPIKey(ctx context.Context, apiKey string) (*store.ManagedNode, error) {
	var n store.ManagedNode
	if err := s.db.WithContext(ctx).Preload("Group").First(&n, "api_key = ?", apiKey).Error; err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) NodeList(ctx context.Context) ([]*store.ManagedNode, error) {
	var nodes []*store.ManagedNode
	if err := s.db.WithContext(ctx).Preload("Group").Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

func (s *Store) ListByGroup(ctx context.Context, groupID string) ([]*store.ManagedNode, error) {
	var nodes []*store.ManagedNode
	if err := s.db.WithContext(ctx).Preload("Group").Where("group_id = ?", groupID).Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

func (s *Store) ListByLabels(ctx context.Context, labels map[string]string) ([]*store.ManagedNode, error) {
	var all []*store.ManagedNode
	if err := s.db.WithContext(ctx).Preload("Group").Find(&all).Error; err != nil {
		return nil, err
	}

	var result []*store.ManagedNode
	for _, n := range all {
		if matchLabels(n.Labels, labels) {
			result = append(result, n)
		}
	}
	return result, nil
}

func matchLabels(nodeLabels, selector map[string]string) bool {
	for k, v := range selector {
		if nodeLabels[k] != v {
			return false
		}
	}
	return true
}

func (s *Store) ListBySelector(ctx context.Context, sel store.CommandSelector) ([]*store.ManagedNode, error) {
	// Start with all nodes, applying SQL-level filters where possible.
	q := s.db.WithContext(ctx).Preload("Group")

	if sel.GroupID != "" {
		q = q.Where("group_id = ?", sel.GroupID)
	}
	if len(sel.NodeIDs) > 0 {
		q = q.Where("id IN ?", sel.NodeIDs)
	}

	var nodes []*store.ManagedNode
	if err := q.Find(&nodes).Error; err != nil {
		return nil, err
	}

	// Filter by labels in Go if needed.
	if len(sel.Labels) > 0 {
		var filtered []*store.ManagedNode
		for _, n := range nodes {
			if matchLabels(n.Labels, sel.Labels) {
				filtered = append(filtered, n)
			}
		}
		return filtered, nil
	}

	return nodes, nil
}

func (s *Store) UpdateHeartbeat(ctx context.Context, id string, agentVersion string, osRelease map[string]string, addresses []store.NodeAddress, bootState string, hostname string, remoteIP string) error {
	now := time.Now()
	n := store.ManagedNode{
		LastHeartbeat: &now,
		AgentVersion:  agentVersion,
		OSRelease:     osRelease,
		Phase:         store.PhaseOnline,
	}
	columns := []string{"last_heartbeat", "agent_version", "os_release", "phase"}
	// Addresses and boot state are only updated when the heartbeat actually
	// carries them: an older agent (or the WebSocket heartbeat path, which does
	// not collect them) omits the fields, and must not clobber values a node
	// supplied via register or a richer heartbeat. A real update — e.g. a DHCP
	// change — sends the full current list, which replaces the stored one.
	if addresses != nil {
		n.Addresses = addresses
		columns = append(columns, "addresses")
	}
	if bootState != "" {
		n.BootState = bootState
		columns = append(columns, "boot_state")
	}
	// The hostname a node registered with is the one it had at registration time,
	// which for a templated hostname (hostname: kairos-{{ trunc 4 .MachineID }})
	// is the image default, because phone-home can register before cloud-init has
	// applied the final one. Re-applying it from every heartbeat is what lets the
	// stored value catch up, here and for any later rename
	// (kairos-io/kairos#4196). Same non-empty guard as above: an agent that does
	// not report a hostname must not blank the one already on record.
	if hostname != "" {
		n.Hostname = hostname
		columns = append(columns, "hostname")
	}
	// Same non-empty guard: a caller that cannot resolve a remote IP (a unit test,
	// or a future transport that does not expose one) must not blank a value a
	// prior heartbeat recorded.
	if remoteIP != "" {
		n.RemoteIP = remoteIP
		columns = append(columns, "remote_ip")
	}
	return s.updateNodeColumns(ctx, id, &n, columns...)
}

// updateNodeColumns writes only the given columns of node id, taking their
// values from n. Writing the whole row back would also write every column the
// caller did not mean to change, as it was when the caller read it, over a
// lifecycle transition or group change another request committed meanwhile.
// The update goes through the model so the JSON serializers of OSRelease,
// Labels and Addresses apply. A node that does not exist is reported as
// gorm.ErrRecordNotFound.
func (s *Store) updateNodeColumns(ctx context.Context, id string, n *store.ManagedNode, columns ...string) error {
	res := s.db.WithContext(ctx).Model(&store.ManagedNode{}).
		Where("id = ?", id).
		Select(columns).
		Updates(n)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *Store) UpdatePhase(ctx context.Context, id string, phase string) error {
	return s.db.WithContext(ctx).Model(&store.ManagedNode{}).Where("id = ?", id).Update("phase", phase).Error
}

// SetGroup reports gorm.ErrRecordNotFound when either the node or the target
// group does not exist. SQLite does not enforce the group_id foreign key, so
// the group is looked up rather than left to the database.
func (s *Store) SetGroup(ctx context.Context, nodeID string, groupID *string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if groupID != nil {
			var count int64
			if err := tx.Model(&store.NodeGroup{}).Where("id = ?", *groupID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return gorm.ErrRecordNotFound
			}
		}
		res := tx.Model(&store.ManagedNode{}).Where("id = ?", nodeID).Update("group_id", groupID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (s *Store) SetLabels(ctx context.Context, nodeID string, labels map[string]string) error {
	return s.updateNodeColumns(ctx, nodeID, &store.ManagedNode{Labels: labels}, "labels")
}

func (s *Store) NodeDelete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("managed_node_id = ?", id).Delete(&store.NodeCommand{}).Error; err != nil {
			return err
		}
		if err := tx.Where("node_id = ?", id).Delete(&store.NodeExtensionRow{}).Error; err != nil {
			return err
		}
		return tx.Delete(&store.ManagedNode{}, "id = ?", id).Error
	})
}

// nodeClaimedByKey returns the node in groupID currently claimed by claimKey, or
// (nil, nil) when there is none. It is the idempotent-replay lookup for ClaimNode.
func (s *Store) nodeClaimedByKey(ctx context.Context, groupID, claimKey string) (*store.ManagedNode, error) {
	var n store.ManagedNode
	err := s.db.WithContext(ctx).Preload("Group").
		Where("group_id = ? AND claim_key = ?", groupID, claimKey).
		First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// ClaimNode atomically assigns one unclaimed node in groupID to claimKey.
//
// The three identifiers have distinct roles. groupID is the id of a NodeGroup —
// a pool of interchangeable nodes an operator has bucketed (e.g. by architecture
// or capability, like "arm64" or "gpu-nodes"); the caller claims from a pool, not
// a named machine. claimKey is an opaque, caller-owned label for the claim (e.g. a
// CAPI Machine's UID) — the naming logic lives entirely on the caller's side, and
// it need not be a secret. The returned node's own ID is the store-assigned
// identifier of whichever node the pool handed back.
//
// Idempotency comes first: if claimKey already owns a node in the group, that
// same node is returned without claiming a second one — so a controller that
// crashed mid-provision and reconciles again re-finds its own node.
//
// Otherwise it walks a snapshot of the group's currently-unclaimed nodes and
// tries to claim each with a conditional UPDATE (... WHERE id = ? AND claim_key
// IS NULL). Exactly one of several concurrent callers wins any given node — a
// lost race yields RowsAffected == 0 and moves to the next candidate — so two
// callers never receive the same node. This mirrors the store's existing
// compare-and-set claim idiom (ClaimForDelivery / CASEjectState) and needs no
// FOR UPDATE / SKIP LOCKED, so it is correct on both SQLite and PostgreSQL.
//
// A concurrent claim with the SAME new claimKey is caught by the (group_id,
// claim_key) unique index: the second UPDATE fails with ErrDuplicatedKey, and we
// resolve it to the node the first call claimed. When the snapshot is exhausted
// with nothing won, the group is at capacity and we return ErrNoClaimCapacity.
func (s *Store) ClaimNode(ctx context.Context, groupID, claimKey string) (*store.ManagedNode, error) {
	if existing, err := s.nodeClaimedByKey(ctx, groupID, claimKey); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	// Snapshot unclaimed candidate IDs (oldest first) so the loop is bounded.
	var candidateIDs []string
	if err := s.db.WithContext(ctx).Model(&store.ManagedNode{}).
		Where("group_id = ? AND claim_key IS NULL", groupID).
		Order("created_at asc").
		Pluck("id", &candidateIDs).Error; err != nil {
		return nil, err
	}

	now := time.Now()
	for _, id := range candidateIDs {
		res := s.db.WithContext(ctx).Model(&store.ManagedNode{}).
			Where("id = ? AND claim_key IS NULL", id).
			Updates(map[string]any{"claim_key": claimKey, "claimed_at": &now})
		if res.Error != nil {
			// A concurrent claim with this same key already took another node in
			// the group; the unique index rejected this one. Return that node.
			if errors.Is(res.Error, gorm.ErrDuplicatedKey) {
				if mine, err := s.nodeClaimedByKey(ctx, groupID, claimKey); err != nil {
					return nil, err
				} else if mine != nil {
					return mine, nil
				}
			}
			return nil, res.Error
		}
		if res.RowsAffected == 1 {
			return s.NodeGetByID(ctx, id)
		}
		// RowsAffected == 0: another caller claimed this node between the snapshot
		// and now — try the next candidate.
	}

	// Snapshot exhausted. One last idempotent check covers the case where our own
	// key won a node via a concurrent call while we were losing races here.
	if mine, err := s.nodeClaimedByKey(ctx, groupID, claimKey); err != nil {
		return nil, err
	} else if mine != nil {
		return mine, nil
	}
	return nil, store.ErrNoClaimCapacity
}

// ReleaseNode clears the claim on nodeID and returns it to the pool, but only if
// the claim is currently held by claimKey. The conditional UPDATE (... WHERE id =
// ? AND claim_key = ?) ensures callers only release their own claims: a node that
// is unclaimed or owned by a different key is left untouched and released is
// false. Setting claim_key back to NULL frees it for a future ClaimNode.
//
// The claimKey match is an accident-prevention guard, not a security boundary:
// claimKey is not a secret (any admin-authenticated caller can read it from
// GET /nodes), so this only stops well-behaved callers from releasing each
// other's claims by mistake — it is not an authorization control.
func (s *Store) ReleaseNode(ctx context.Context, nodeID, claimKey string) (bool, error) {
	res := s.db.WithContext(ctx).Model(&store.ManagedNode{}).
		Where("id = ? AND claim_key = ?", nodeID, claimKey).
		Updates(map[string]any{"claim_key": nil, "claimed_at": nil})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// AdvanceReset compare-and-sets a node's ResetState. The conditional UPDATE
// (... WHERE id = ? AND reset_state IN fromStates) means exactly one of several
// concurrent re-register resolvers performs any given transition; the losers see
// RowsAffected == 0. When stampLastReset is set, last_reset is written in the same
// statement so the "done" transition and its timestamp land atomically. This
// mirrors the store's existing CAS idiom (ClaimForDelivery / CASEjectState).
//
// The transition to in-progress also stamps reset_progress_at, because reaching
// it means the node came back reporting bootState=autoreset. That re-anchors
// the reset timeout on the last proof of liveness, which is what
// kairos-io/kairos#4287 asks for: fail a reset only when the node has not come
// back since.
//
// A transition to failed clears reset_reboot_at in the same statement: the node
// itself showed the failure, so SettleAfterReboot must not turn it into done on
// a later boot.
func (s *Store) AdvanceReset(ctx context.Context, nodeID string, fromStates []string, to string, stampLastReset bool) (bool, error) {
	updates := map[string]any{"reset_state": to}
	if stampLastReset {
		now := time.Now()
		updates["last_reset"] = &now
	}
	if to == store.ResetStateInProgress {
		now := time.Now()
		updates["reset_progress_at"] = &now
	}
	if to == store.ResetStateFailed {
		updates["reset_reboot_at"] = nil
	}
	res := s.db.WithContext(ctx).Model(&store.ManagedNode{}).
		Where("id = ? AND reset_state IN ?", nodeID, fromStates).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// StampResetProgress refreshes reset_progress_at to now for a reset that is
// still in flight. The state predicate is part of the conditional update, so a
// delivery that lands after the lifecycle already resolved cannot re-anchor a
// done or failed reset. Returns true only when a row was stamped.
func (s *Store) StampResetProgress(ctx context.Context, nodeID string) (bool, error) {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&store.ManagedNode{}).
		Where("id = ? AND reset_state IN ?",
			nodeID, []string{store.ResetStatePending, store.ResetStateInProgress}).
		Update("reset_progress_at", &now)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// FailResetBefore compare-and-sets an overdue in-flight reset to failed. Both
// timestamps participate in the conditional update so nothing that refreshed the
// budget concurrently can be failed from a stale read: a new reset request
// refreshes reset_requested_at, and either taking delivery of the reset command
// or re-registering mid-reset refreshes reset_progress_at.
func (s *Store) FailResetBefore(ctx context.Context, nodeID string, deadline time.Time) (bool, error) {
	res := s.db.WithContext(ctx).Model(&store.ManagedNode{}).
		Where("id = ? AND reset_state IN ? AND reset_requested_at <= ? AND (reset_progress_at IS NULL OR reset_progress_at <= ?)",
			nodeID, []string{store.ResetStatePending, store.ResetStateInProgress}, deadline, deadline).
		Update("reset_state", store.ResetStateFailed)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// AdvanceUpgrade compare-and-sets a node's UpgradeState with the same
// conditional UPDATE as AdvanceReset (... WHERE id = ? AND upgrade_state IN
// fromStates), so exactly one of several concurrent re-register resolvers
// performs any given transition. When stampLastUpgrade is set, last_upgrade is
// written in the same statement as the transition to done. A transition to
// failed clears upgrade_reboot_at for the same reason as in AdvanceReset.
func (s *Store) AdvanceUpgrade(ctx context.Context, nodeID string, fromStates []string, to string, stampLastUpgrade bool) (bool, error) {
	updates := map[string]any{"upgrade_state": to}
	if stampLastUpgrade {
		now := time.Now()
		updates["last_upgrade"] = &now
	}
	if to == store.UpgradeStateFailed {
		updates["upgrade_reboot_at"] = nil
	}
	res := s.db.WithContext(ctx).Model(&store.ManagedNode{}).
		Where("id = ? AND upgrade_state IN ?", nodeID, fromStates).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// SettleAfterReboot resolves a node's pending upgrade or reset from a node that
// booted at bootedAt. It locks the node row, so the check against the recorded
// reboot and the transition see the same operation: an upgrade or reset
// requested in between clears the recorded reboot and waits for the next one.
//
// A failed upgrade or reset still settles while its reboot is recorded: a
// batch timeout fails it without knowing the outcome, and the node may come
// back later. A failure the node itself showed is final, so settling as
// failed clears the recorded reboot. A command that reported Failed recorded
// no reboot in the first place.
func (s *Store) SettleAfterReboot(ctx context.Context, nodeID, bootState string, bootedAt time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockNodes(tx, []string{nodeID}); err != nil {
			return err
		}
		var n store.ManagedNode
		if err := tx.Where("id = ?", nodeID).First(&n).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		now := time.Now()
		updates := map[string]any{}
		upgradeOpen := n.UpgradeState == store.UpgradeStatePending || n.UpgradeState == store.UpgradeStateFailed
		if upgradeOpen && rebootedAfter(n.UpgradeRebootAt, bootedAt) {
			if to, ok := store.UpgradeOutcome(bootState); ok {
				updates["upgrade_state"] = to
				switch to {
				case store.UpgradeStateDone:
					updates["last_upgrade"] = &now
				case store.UpgradeStateFailed:
					updates["upgrade_reboot_at"] = nil
				}
			}
		}
		resetOpen := n.ResetState == store.ResetStatePending || n.ResetState == store.ResetStateInProgress ||
			n.ResetState == store.ResetStateFailed
		if resetOpen && rebootedAfter(n.ResetRebootAt, bootedAt) {
			if to, ok := store.ResetOutcome(n.ResetState, bootState); ok {
				updates["reset_state"] = to
				switch to {
				case store.ResetStateDone:
					updates["last_reset"] = &now
				case store.ResetStateFailed:
					updates["reset_reboot_at"] = nil
				}
				// Re-anchor the reset timeout, as AdvanceReset does for a
				// re-register (kairos-io/kairos#4287).
				if to == store.ResetStateInProgress {
					updates["reset_progress_at"] = &now
				}
			}
		}
		if len(updates) == 0 {
			return nil
		}
		return tx.Model(&store.ManagedNode{}).Where("id = ?", nodeID).Updates(updates).Error
	})
}

// rebootedAfter reports whether a node that booted at bootedAt went through
// the reboot scheduled at rebootAt.
func rebootedAfter(rebootAt *time.Time, bootedAt time.Time) bool {
	return rebootAt != nil && bootedAt.After(*rebootAt)
}

// recordReboot stamps the reboot an upgrade or reset command scheduled when it
// completed, on nodes still waiting for that operation's outcome.
func recordReboot(tx *gorm.DB, completed []store.NodeCommand) error {
	resetNodes, upgradeNodes := lifecycleNodes(completed)
	if err := lockNodes(tx, resetNodes, upgradeNodes); err != nil {
		return err
	}
	now := time.Now()
	if len(resetNodes) > 0 {
		if err := tx.Model(&store.ManagedNode{}).
			Where("id IN ? AND reset_state IN ?", resetNodes,
				[]string{store.ResetStatePending, store.ResetStateInProgress}).
			Update("reset_reboot_at", &now).Error; err != nil {
			return err
		}
	}
	if len(upgradeNodes) > 0 {
		return tx.Model(&store.ManagedNode{}).
			Where("id IN ? AND upgrade_state = ?", upgradeNodes, store.UpgradeStatePending).
			Update("upgrade_reboot_at", &now).Error
	}
	return nil
}

// --- CommandStore ---

func (s *Store) CommandCreate(ctx context.Context, cmd *store.NodeCommand) error {
	cmd.ID = uuid.New().String()
	cmd.Phase = store.CommandPending
	return s.db.WithContext(ctx).Create(cmd).Error
}

// CreateBatch inserts commands and marks their nodes' reset or upgrade
// lifecycle pending in one transaction. The rows are inserted one at a time
// inside it so each gets its own created_at, in the order the caller built
// them.
//
// reset_progress_at is cleared with it, so the new reset's timeout window is
// measured from this request and never inherits the previous reset's stamp.
//
// A node whose upgrade is already pending, or whose reset is pending or
// in-progress, is left as it is. That state and its request time belong to an
// operation still waiting for the node to reboot and re-register, and the
// resolver needs the request time of that operation.
func (s *Store) CreateBatch(ctx context.Context, cmds []*store.NodeCommand) error {
	if len(cmds) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return createCommands(tx, cmds)
	})
}

// createCommands is the body of CreateBatch, for callers that create commands
// inside a transaction of their own.
func createCommands(tx *gorm.DB, cmds []*store.NodeCommand) error {
	var resetNodes, upgradeNodes []string
	for _, cmd := range cmds {
		if cmd.ID == "" {
			cmd.ID = uuid.New().String()
		}
		if cmd.Phase == "" {
			cmd.Phase = store.CommandPending
		}
		if err := tx.Create(cmd).Error; err != nil {
			return err
		}
		switch cmd.Command {
		case store.CmdReset:
			resetNodes = append(resetNodes, cmd.ManagedNodeID)
		case store.CmdUpgrade:
			upgradeNodes = append(upgradeNodes, cmd.ManagedNodeID)
		}
	}
	if err := lockNodes(tx, resetNodes, upgradeNodes); err != nil {
		return err
	}
	now := time.Now()
	if len(resetNodes) > 0 {
		if err := tx.Model(&store.ManagedNode{}).
			Where("id IN ?", resetNodes).
			Where("reset_state IS NULL OR reset_state NOT IN ?",
				[]string{store.ResetStatePending, store.ResetStateInProgress}).
			Updates(map[string]any{
				"reset_state":        store.ResetStatePending,
				"reset_requested_at": &now,
				"reset_progress_at":  nil,
				"reset_reboot_at":    nil,
			}).Error; err != nil {
			return err
		}
	}
	if len(upgradeNodes) > 0 {
		if err := tx.Model(&store.ManagedNode{}).
			Where("id IN ?", upgradeNodes).
			Where("upgrade_state IS NULL OR upgrade_state <> ?", store.UpgradeStatePending).
			Updates(map[string]any{
				"upgrade_state":        store.UpgradeStatePending,
				"upgrade_requested_at": &now,
				"upgrade_reboot_at":    nil,
			}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CommandGetByID(ctx context.Context, id string) (*store.NodeCommand, error) {
	var cmd store.NodeCommand
	if err := s.db.WithContext(ctx).First(&cmd, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &cmd, nil
}

func (s *Store) GetPending(ctx context.Context, nodeID string) ([]*store.NodeCommand, error) {
	var cmds []*store.NodeCommand
	if err := s.db.WithContext(ctx).
		Where("managed_node_id = ? AND phase = ? AND (expires_at IS NULL OR expires_at > ?)",
			nodeID, store.CommandPending, time.Now()).
		Find(&cmds).Error; err != nil {
		return nil, err
	}
	return cmds, nil
}

func (s *Store) MarkDelivered(ctx context.Context, ids []string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&store.NodeCommand{}).Where("id IN ?", ids).Updates(map[string]any{
		"phase":        store.CommandDelivered,
		"delivered_at": &now,
	}).Error
}

// ClaimForDelivery atomically transitions a single Pending command to Delivered.
// The conditional WHERE (id = ? AND phase = Pending AND not expired) plus a
// RowsAffected check makes the claim a race-free compare-and-set: when a WS push
// and an agent poll (or two concurrent polls) both target the same command,
// exactly one UPDATE matches the still-deliverable row and the loser sees
// RowsAffected == 0. The expiry predicate is repeated here, rather than relying
// only on GetPending, so a command cannot expire between selection and claim.
func (s *Store) ClaimForDelivery(ctx context.Context, id string) (bool, error) {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&store.NodeCommand{}).
		Where("id = ? AND phase = ? AND (expires_at IS NULL OR expires_at > ?)", id, store.CommandPending, now).
		Updates(map[string]any{
			"phase":        store.CommandDelivered,
			"delivered_at": &now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// UpdateStatus records a command's phase and result. A command that does not
// exist surfaces as store.ErrCommandNotFound, one that already ended in
// another terminal phase as store.ErrCommandTerminal.
func (s *Store) UpdateStatus(ctx context.Context, id string, phase string, result string) error {
	n, err := s.updateStatus(ctx, phase, result, "id = ?", id)
	if err != nil {
		return err
	}
	if n == 0 {
		return s.unwrittenStatus(ctx, phase, "id = ?", id)
	}
	return nil
}

// UpdateStatusForNode updates a command's status scoped to its owning node.
// The WHERE clause matches both the command id and managed_node_id, so a node
// can only update commands addressed to it. GORM's Updates returns a nil error
// even when the WHERE matches zero rows, so a miss surfaces as
// store.ErrCommandNotFound, never a silent success.
func (s *Store) UpdateStatusForNode(ctx context.Context, id string, nodeID string, phase string, result string) error {
	n, err := s.updateStatus(ctx, phase, result, "id = ? AND managed_node_id = ?", id, nodeID)
	if err != nil {
		return err
	}
	if n == 0 {
		return s.unwrittenStatus(ctx, phase, "id = ? AND managed_node_id = ?", id, nodeID)
	}
	return nil
}

// unwrittenStatus explains why updateStatus wrote no row. No matching command
// is store.ErrCommandNotFound. A command that already ended in phase is a
// repeated report and succeeds without a write; one that ended in another
// terminal phase is store.ErrCommandTerminal.
func (s *Store) unwrittenStatus(ctx context.Context, phase, where string, args ...any) error {
	var cmd store.NodeCommand
	err := s.db.WithContext(ctx).Select("phase").Where(where, args...).Take(&cmd).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return store.ErrCommandNotFound
	}
	if err != nil {
		return err
	}
	if cmd.Phase == phase {
		return nil
	}
	return store.ErrCommandTerminal
}

// updateStatus writes phase and result to the commands matching where and
// returns how many it wrote. A command already in a terminal phase never
// matches, so the guard holds inside the same UPDATE that writes the row.
//
// A reset or upgrade that fails before the reboot leaves the pending state
// CreateBatch set for it with nothing to settle it: the node never comes back
// with a new boot state for the re-register resolvers to read. A Failed report
// therefore marks that state failed, in the same transaction as the command
// row, unless another command still owns it. The UPDATE runs first so the
// transaction never holds a read snapshot when it writes: under SQLite WAL a
// transaction that read first fails its write with "database is locked"
// (SQLITE_BUSY_SNAPSHOT) when another connection committed in between, and
// busy_timeout does not retry it.
func (s *Store) updateStatus(ctx context.Context, phase, result, where string, args ...any) (int, error) {
	updates := map[string]any{
		"phase":  phase,
		"result": result,
	}
	if phase == store.CommandCompleted || phase == store.CommandFailed {
		now := time.Now()
		updates["completed_at"] = &now
	}
	var updated []store.NodeCommand
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&updated).
			Clauses(clause.Returning{Columns: []clause.Column{
				{Name: "managed_node_id"}, {Name: "command"},
			}}).
			Where(where, args...).
			Where("phase NOT IN ?", store.TerminalCommandPhases).
			Updates(updates).Error; err != nil {
			return err
		}
		if phase == store.CommandCompleted {
			return recordReboot(tx, updated)
		}
		if phase != store.CommandFailed {
			return nil
		}
		resetNodes, upgradeNodes := lifecycleNodes(updated)
		if err := lockNodes(tx, resetNodes, upgradeNodes); err != nil {
			return err
		}
		if err := settleOrphanedLifecycle(tx, store.CmdReset, resetNodes,
			"reset_state", store.ResetStatePending, "reset_requested_at",
			map[string]any{"reset_state": store.ResetStateFailed}); err != nil {
			return err
		}
		return settleOrphanedLifecycle(tx, store.CmdUpgrade, upgradeNodes,
			"upgrade_state", store.UpgradeStatePending, "upgrade_requested_at",
			map[string]any{"upgrade_state": store.UpgradeStateFailed})
	})
	if err != nil {
		return 0, err
	}
	return len(updated), nil
}

func (s *Store) ListByNode(ctx context.Context, nodeID string) ([]*store.NodeCommand, error) {
	var cmds []*store.NodeCommand
	if err := s.db.WithContext(ctx).Where("managed_node_id = ?", nodeID).Find(&cmds).Error; err != nil {
		return nil, err
	}
	return cmds, nil
}

// lockNodes takes the row locks of the given nodes, in id order, before a
// transaction reads which commands own their lifecycle state.
//
// The ownership check in settleOrphanedLifecycle and the guard in CreateBatch
// are UPDATEs whose WHERE can match no row, and an UPDATE that matches no row
// locks nothing. On PostgreSQL, two transactions that each wrote a command row
// of the same node would then each read the other's command as it was before
// that write, and both could skip the node: two upgrades failing at once
// would leave it pending. Holding the node row makes the second transaction
// wait for the first to commit, and its next statement sees the first one's
// command. CreateBatch and updateStatus both write their command rows first
// and lock node rows after, in id order, so they cannot deadlock one another.
//
// SQLite has no row locks, so the statement is skipped there: the write each
// caller makes first already holds the database's single write lock.
func lockNodes(tx *gorm.DB, groups ...[]string) error {
	if tx.Dialector.Name() == "sqlite" {
		return nil
	}
	var ids []string
	for _, g := range groups {
		ids = append(ids, g...)
	}
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	var locked []string
	return tx.Model(&store.ManagedNode{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", ids).
		Order("id").
		Pluck("id", &locked).Error
}

// lifecycleNodes splits the nodes of cmds by the lifecycle their command
// drives: resets and upgrades. Other commands do not reboot the node and drive
// none.
func lifecycleNodes(cmds []store.NodeCommand) (resetNodes, upgradeNodes []string) {
	for _, cmd := range cmds {
		switch cmd.Command {
		case store.CmdReset:
			resetNodes = append(resetNodes, cmd.ManagedNodeID)
		case store.CmdUpgrade:
			upgradeNodes = append(upgradeNodes, cmd.ManagedNodeID)
		}
	}
	return resetNodes, upgradeNodes
}

// settleOrphanedLifecycle applies updates to the nodes in nodeIDs whose
// stateColumn is still pendingState and that no command of the given kind
// owns. A command in Pending, Delivered or Running owns the state, and so does
// a Completed one that finished at or after requestedColumn: the agent
// completes an upgrade or reset once it has scheduled the reboot, and the
// state stays pending until the node re-registers and the resolver settles it.
// A Completed command that finished before the request belongs to an earlier
// operation and owns nothing.
//
// On SQLite both columns hold the text the driver writes for a time.Time, with
// trailing fractional zeros trimmed and the zone offset of time.Now. Text
// comparison orders such values by time as long as the offsets match, which
// holds for timestamps the same server wrote.
func settleOrphanedLifecycle(tx *gorm.DB, command string, nodeIDs []string, stateColumn, pendingState, requestedColumn string, updates map[string]any) error {
	if len(nodeIDs) == 0 {
		return nil
	}
	owner := tx.Model(&store.NodeCommand{}).Select("1").
		Where("node_commands.managed_node_id = managed_nodes.id AND node_commands.command = ?"+
			" AND (node_commands.phase IN ?"+
			" OR (node_commands.phase = ? AND node_commands.completed_at >= managed_nodes."+requestedColumn+"))",
			command,
			[]string{store.CommandPending, store.CommandDelivered, store.CommandRunning},
			store.CommandCompleted)
	return tx.Model(&store.ManagedNode{}).
		Where("id IN ?", nodeIDs).
		Where(stateColumn+" = ?", pendingState).
		Where("NOT EXISTS (?)", owner).
		Updates(updates).Error
}

func (s *Store) CommandDelete(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Delete(&store.NodeCommand{}, "id = ?", id)
	if result.RowsAffected == 0 {
		return fmt.Errorf("command not found")
	}
	return result.Error
}

func (s *Store) CommandDeleteTerminal(ctx context.Context, nodeID string) error {
	// Canceled and Expired are history exactly like Completed and Failed: the
	// node will never run the command, so a row left behind would sit in the
	// node's list forever after the operator asked for it to be cleared.
	return s.db.WithContext(ctx).Where(
		"managed_node_id = ? AND phase IN ?",
		nodeID, []string{store.CommandCompleted, store.CommandFailed, store.CommandExpired, store.CommandCanceled},
	).Delete(&store.NodeCommand{}).Error
}

// CommandExpireBefore transitions this node's overdue commands to Expired.
//
// The WHERE is the complement of the one GetPending uses to hide them: a
// non-null expires_at that has passed. Restricting it to the three
// non-terminal phases is what makes the UPDATE idempotent and safe against a
// late agent report — a command the node did finish keeps its real outcome,
// and re-running the statement rewrites nothing. completed_at is stamped for
// the same reason Completed and Failed stamp it: the row has stopped moving.
func (s *Store) CommandExpireBefore(ctx context.Context, nodeID string, deadline time.Time) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&store.NodeCommand{}).
		Where("managed_node_id = ? AND expires_at IS NOT NULL AND expires_at <= ? AND phase IN ?",
			nodeID, deadline, []string{store.CommandPending, store.CommandDelivered, store.CommandRunning}).
		Updates(map[string]any{
			"phase":        store.CommandExpired,
			"completed_at": &now,
		}).Error
}

// --- ArtifactStore ---

func (s *Store) ArtifactCreate(ctx context.Context, rec *store.ArtifactRecord) error {
	if rec.Phase == "" {
		rec.Phase = store.ArtifactPending
	}
	return s.db.WithContext(ctx).Create(rec).Error
}

func (s *Store) ArtifactGetByID(ctx context.Context, id string) (*store.ArtifactRecord, error) {
	var rec store.ArtifactRecord
	if err := s.db.WithContext(ctx).First(&rec, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *Store) ArtifactList(ctx context.Context) ([]*store.ArtifactRecord, error) {
	var records []*store.ArtifactRecord
	if err := s.db.WithContext(ctx).Omit("Logs").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Store) ArtifactUpdate(ctx context.Context, rec *store.ArtifactRecord) error {
	// Omit the logs column so a caller's stale in-memory rec.Logs never
	// clobbers concurrent AppendLog appends. AppendLog is the sole writer of
	// that column; every other field remains updatable via this method.
	return s.db.WithContext(ctx).Omit("logs").Save(rec).Error
}

// ArtifactUpdatePhaseMessage writes only the phase and message columns for the
// row with id. It avoids the read-modify-write race that ArtifactUpdate has
// against ArtifactAppendLog: Save rewrites every column from the in-memory
// record, so a log line appended between a caller's GetByID and its Update is
// clobbered by the stale logs snapshot. This method touches only the two
// columns callers actually want to change.
func (s *Store) ArtifactUpdatePhaseMessage(ctx context.Context, id, phase, message string) error {
	return s.db.WithContext(ctx).Model(&store.ArtifactRecord{}).Where("id = ?", id).
		Updates(map[string]interface{}{"phase": phase, "message": message}).Error
}

// ArtifactUpdateFiles writes only the artifact_files column for the row with
// id. Upload calls this instead of ArtifactUpdate so its snapshotted
// phase/message do not race back over watchCRPhase's phase writes. Select
// forces the write even when files is empty (zero-value struct fields are
// otherwise skipped by GORM's Updates on a struct), and it engages the JSON
// serializer registered on the ArtifactFiles field.
func (s *Store) ArtifactUpdateFiles(ctx context.Context, id string, files []string) error {
	return s.db.WithContext(ctx).Model(&store.ArtifactRecord{}).Where("id = ?", id).
		Select("ArtifactFiles").
		Updates(store.ArtifactRecord{ArtifactFiles: files}).Error
}

// ArtifactUpdateNameSaved writes only the name and saved columns for the row
// with id, skipping the ones that are nil. A map is used so an empty name or a
// false saved flag is still written. Nothing is written when both are nil.
func (s *Store) ArtifactUpdateNameSaved(ctx context.Context, id string, name *string, saved *bool) error {
	cols := map[string]interface{}{}
	if name != nil {
		cols["name"] = *name
	}
	if saved != nil {
		cols["saved"] = *saved
	}
	if len(cols) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&store.ArtifactRecord{}).Where("id = ?", id).
		Updates(cols).Error
}

// ArtifactClearUploadToken zeroes the upload_token column for the row with id.
// watchCRPhase calls this on the terminal transition so a leaked token cannot
// be replayed against Upload after the build has finished. Select forces the
// write even though the empty-string value is a zero for the field.
func (s *Store) ArtifactClearUploadToken(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Model(&store.ArtifactRecord{}).Where("id = ?", id).
		Select("UploadToken").
		Updates(store.ArtifactRecord{UploadToken: ""}).Error
}

func (s *Store) ArtifactDelete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("artifact_id = ?", id).Delete(&store.ArtifactExtensionBundle{}).Error; err != nil {
			return err
		}
		return tx.Delete(&store.ArtifactRecord{}, "id = ?", id).Error
	})
}

func (s *Store) ArtifactGetLogs(ctx context.Context, id string) (string, error) {
	var rec store.ArtifactRecord
	if err := s.db.WithContext(ctx).Select("logs").First(&rec, "id = ?", id).Error; err != nil {
		return "", err
	}
	return rec.Logs, nil
}

func (s *Store) ArtifactDeleteByPhase(ctx context.Context, phase string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := tx.Model(&store.ArtifactRecord{}).Select("id").Where("phase = ?", phase)
		if err := tx.Where("artifact_id IN (?)", ids).Delete(&store.ArtifactExtensionBundle{}).Error; err != nil {
			return err
		}
		return tx.Where("phase = ?", phase).Delete(&store.ArtifactRecord{}).Error
	})
}

func (s *Store) ArtifactAppendLog(ctx context.Context, id string, text string) error {
	return s.db.WithContext(ctx).Model(&store.ArtifactRecord{}).Where("id = ?", id).
		Update("logs", cappedLogAppend(text)).Error
}

// --- SecureBootKeySetStore ---

func (s *Store) SecureBootKeySetCreate(ctx context.Context, ks *store.SecureBootKeySet) error {
	ks.ID = uuid.New().String()
	return s.db.WithContext(ctx).Create(ks).Error
}

func (s *Store) SecureBootKeySetGetByID(ctx context.Context, id string) (*store.SecureBootKeySet, error) {
	var ks store.SecureBootKeySet
	if err := s.db.WithContext(ctx).First(&ks, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &ks, nil
}

func (s *Store) SecureBootKeySetGetByName(ctx context.Context, name string) (*store.SecureBootKeySet, error) {
	var ks store.SecureBootKeySet
	if err := s.db.WithContext(ctx).First(&ks, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &ks, nil
}

func (s *Store) SecureBootKeySetList(ctx context.Context) ([]*store.SecureBootKeySet, error) {
	var sets []*store.SecureBootKeySet
	if err := s.db.WithContext(ctx).Find(&sets).Error; err != nil {
		return nil, err
	}
	return sets, nil
}

func (s *Store) SecureBootKeySetDelete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&store.SecureBootKeySet{}, "id = ?", id).Error
}

// --- BMCTargetStore ---

func (s *Store) BMCTargetCreate(ctx context.Context, target *store.BMCTarget) error {
	target.ID = uuid.New().String()
	// Encrypt the password before it touches the DB. Operate on a copy so the
	// caller's in-memory plaintext (echoed back in the API response) is untouched.
	row := *target
	if err := s.encryptPassword(&row); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Create(&row).Error
}

func (s *Store) BMCTargetGetByID(ctx context.Context, id string) (*store.BMCTarget, error) {
	var t store.BMCTarget
	if err := s.db.WithContext(ctx).First(&t, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if err := s.decryptPassword(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) BMCTargetList(ctx context.Context) ([]*store.BMCTarget, error) {
	var targets []*store.BMCTarget
	if err := s.db.WithContext(ctx).Find(&targets).Error; err != nil {
		return nil, err
	}
	for _, t := range targets {
		if err := s.decryptPassword(t); err != nil {
			return nil, err
		}
	}
	return targets, nil
}

func (s *Store) BMCTargetUpdate(ctx context.Context, target *store.BMCTarget) error {
	row := *target
	if err := s.encryptPassword(&row); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Save(&row).Error
}

// encryptPassword replaces target.Password with its AES-256-GCM ciphertext when a
// cipher is configured. Empty passwords stay empty (no encrypted-empty garbage).
func (s *Store) encryptPassword(target *store.BMCTarget) error {
	if s.cipher == nil || target.Password == "" {
		return nil
	}
	enc, err := s.cipher.Encrypt(target.Password)
	if err != nil {
		return fmt.Errorf("encrypting BMC password: %w", err)
	}
	target.Password = enc
	return nil
}

// decryptPassword replaces the stored ciphertext with the plaintext password. If
// decryption fails it assumes a legacy plaintext value written before encryption
// was enabled and leaves it untouched (fresh-DB deployments never hit this path).
func (s *Store) decryptPassword(target *store.BMCTarget) error {
	if s.cipher == nil || target.Password == "" {
		return nil
	}
	plain, err := s.cipher.Decrypt(target.Password)
	if err != nil {
		// Tolerate a pre-encryption plaintext row rather than failing reads.
		return nil
	}
	target.Password = plain
	return nil
}

func (s *Store) BMCTargetDelete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&store.BMCTarget{}, "id = ?", id).Error
}

// --- DeploymentStore ---

func (s *Store) DeploymentCreate(ctx context.Context, dep *store.Deployment) error {
	dep.ID = uuid.New().String()
	return s.db.WithContext(ctx).Create(dep).Error
}

func (s *Store) DeploymentGetByID(ctx context.Context, id string) (*store.Deployment, error) {
	var d store.Deployment
	if err := s.db.WithContext(ctx).First(&d, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) DeploymentList(ctx context.Context) ([]*store.Deployment, error) {
	var deps []*store.Deployment
	if err := s.db.WithContext(ctx).Find(&deps).Error; err != nil {
		return nil, err
	}
	return deps, nil
}

func (s *Store) DeploymentListByArtifact(ctx context.Context, artifactID string) ([]*store.Deployment, error) {
	var deps []*store.Deployment
	if err := s.db.WithContext(ctx).Where("artifact_id = ?", artifactID).Find(&deps).Error; err != nil {
		return nil, err
	}
	return deps, nil
}

func (s *Store) DeploymentUpdate(ctx context.Context, dep *store.Deployment) error {
	return s.db.WithContext(ctx).Save(dep).Error
}

func (s *Store) DeploymentDelete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&store.Deployment{}, "id = ?", id).Error
}

// DeploymentCASEjectState atomically transitions eject_state from `from` to `to`.
// The conditional WHERE (id = ? AND eject_state = from) plus a RowsAffected check
// makes the claim a race-free compare-and-set: when an auto eject-on-phone-home and
// a manual finalize both target the same deployment, exactly one UPDATE matches the
// still-`from` row and the loser sees RowsAffected == 0. Mirrors ClaimForDelivery.
func (s *Store) DeploymentCASEjectState(ctx context.Context, id, from, to string) (bool, error) {
	res := s.db.WithContext(ctx).Model(&store.Deployment{}).
		Where("id = ? AND eject_state = ?", id, from).
		Update("eject_state", to)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// --- SettingsStore ---

func (s *Store) SettingGet(ctx context.Context, key string) (string, bool, error) {
	var setting store.Setting
	err := s.db.WithContext(ctx).First(&setting, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return setting.Value, true, nil
}

// SettingSet upserts key→value. The OnConflict clause updates the existing row in
// place (rather than failing on the primary-key collision), so callers get
// last-write-wins semantics on a repeated Set.
func (s *Store) SettingSet(ctx context.Context, key, value string) error {
	setting := store.Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		UpdateAll: true,
	}).Create(&setting).Error
}

func (s *Store) SettingGetAll(ctx context.Context) (map[string]string, error) {
	var settings []store.Setting
	if err := s.db.WithContext(ctx).Find(&settings).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(settings))
	for _, setting := range settings {
		out[setting.Key] = setting.Value
	}
	return out, nil
}

// --- ExtensionStore ---

func (s *Store) ExtensionCreate(ctx context.Context, e *store.ExtensionRecord) error {
	return s.db.WithContext(ctx).Save(e).Error
}

func (s *Store) ExtensionGetByID(ctx context.Context, id string) (*store.ExtensionRecord, error) {
	var rec store.ExtensionRecord
	if err := s.db.WithContext(ctx).First(&rec, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *Store) ExtensionList(ctx context.Context) ([]store.ExtensionRecord, error) {
	var out []store.ExtensionRecord
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ExtensionDelete(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Delete(&store.ExtensionRecord{}, "id = ?", id).Error
}

func (s *Store) ExtensionFindLatestReadyByName(ctx context.Context, extType, name string) (*store.ExtensionRecord, error) {
	var rec store.ExtensionRecord
	q := s.db.WithContext(ctx).
		Where("type = ? AND name = ? AND phase = ?", extType, name, "Ready").
		Order("created_at DESC").Limit(1)
	if err := q.First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *Store) ExtensionFindByNameAndVersion(ctx context.Context, extType, name, version string) (*store.ExtensionRecord, error) {
	var rec store.ExtensionRecord
	q := s.db.WithContext(ctx).
		Where("type = ? AND name = ? AND version = ? AND phase = ?", extType, name, version, "Ready").
		Limit(1)
	if err := q.First(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *Store) ExtensionAppendLog(ctx context.Context, id, chunk string) error {
	return s.db.WithContext(ctx).
		Model(&store.ExtensionRecord{}).
		Where("id = ?", id).
		Update("logs", cappedLogAppend(chunk)).Error
}

// --- ArtifactExtensionBundleStore ---

func (s *Store) BundleListForArtifact(ctx context.Context, artifactID string) ([]store.ArtifactExtensionBundle, error) {
	var out []store.ArtifactExtensionBundle
	err := s.db.WithContext(ctx).
		Where("artifact_id = ?", artifactID).
		Order(`"order" ASC`).
		Find(&out).Error
	return out, err
}

func (s *Store) BundleReplaceForArtifact(ctx context.Context, artifactID string, entries []store.ArtifactExtensionBundle) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("artifact_id = ?", artifactID).Delete(&store.ArtifactExtensionBundle{}).Error; err != nil {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		for i := range entries {
			entries[i].ArtifactID = artifactID
		}
		return tx.Create(&entries).Error
	})
}

func (s *Store) BundleArtifactsReferencingExtension(ctx context.Context, extensionName string) ([]string, error) {
	var ids []string
	err := s.db.WithContext(ctx).
		Model(&store.ArtifactExtensionBundle{}).
		Where("extension_name = ?", extensionName).
		Distinct("artifact_id").
		Pluck("artifact_id", &ids).Error
	return ids, err
}

// --- NodeExtensionStore ---

func (s *Store) NodeExtensionUpsert(ctx context.Context, row *store.NodeExtensionRow) error {
	row.UpdatedAt = time.Now().UTC()
	if row.InstalledAt.IsZero() {
		row.InstalledAt = row.UpdatedAt
	}
	return s.db.WithContext(ctx).Save(row).Error
}

func (s *Store) NodeExtensionListForNode(ctx context.Context, nodeID string) ([]store.NodeExtensionRow, error) {
	var out []store.NodeExtensionRow
	err := s.db.WithContext(ctx).Where("node_id = ?", nodeID).Find(&out).Error
	return out, err
}

func (s *Store) NodeExtensionListForExtensionByName(ctx context.Context, extType, name string) ([]store.NodeExtensionRow, error) {
	var out []store.NodeExtensionRow
	err := s.db.WithContext(ctx).Where("type = ? AND name = ?", extType, name).Find(&out).Error
	return out, err
}

func (s *Store) NodeExtensionDeleteByScope(ctx context.Context, nodeID, extType, name, bootState string) error {
	return s.db.WithContext(ctx).Where(
		"node_id = ? AND type = ? AND name = ? AND boot_state = ?",
		nodeID, extType, name, bootState,
	).Delete(&store.NodeExtensionRow{}).Error
}

func (s *Store) NodeExtensionDeleteByName(ctx context.Context, nodeID, extType, name string) error {
	return s.db.WithContext(ctx).Where(
		"node_id = ? AND type = ? AND name = ?",
		nodeID, extType, name,
	).Delete(&store.NodeExtensionRow{}).Error
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("getting underlying DB: %w", err)
	}
	return sqlDB.Close()
}
