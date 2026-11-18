package gorm

import (
	"context"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// NodeStoreAdapter adapts Store to the store.NodeStore interface.
type NodeStoreAdapter struct{ S *Store }

func (a *NodeStoreAdapter) Register(ctx context.Context, node *store.ManagedNode) error {
	return a.S.Register(ctx, node)
}
func (a *NodeStoreAdapter) GetByID(ctx context.Context, id string) (*store.ManagedNode, error) {
	return a.S.NodeGetByID(ctx, id)
}
func (a *NodeStoreAdapter) GetByMachineID(ctx context.Context, machineID string) (*store.ManagedNode, error) {
	return a.S.GetByMachineID(ctx, machineID)
}
func (a *NodeStoreAdapter) GetByAPIKey(ctx context.Context, apiKey string) (*store.ManagedNode, error) {
	return a.S.GetByAPIKey(ctx, apiKey)
}
func (a *NodeStoreAdapter) List(ctx context.Context) ([]*store.ManagedNode, error) {
	return a.S.NodeList(ctx)
}
func (a *NodeStoreAdapter) ListByGroup(ctx context.Context, groupID string) ([]*store.ManagedNode, error) {
	return a.S.ListByGroup(ctx, groupID)
}
func (a *NodeStoreAdapter) ListByLabels(ctx context.Context, labels map[string]string) ([]*store.ManagedNode, error) {
	return a.S.ListByLabels(ctx, labels)
}
func (a *NodeStoreAdapter) ListBySelector(ctx context.Context, sel store.CommandSelector) ([]*store.ManagedNode, error) {
	return a.S.ListBySelector(ctx, sel)
}
func (a *NodeStoreAdapter) UpdateHeartbeat(ctx context.Context, id string, agentVersion string, osRelease map[string]string, addresses []store.NodeAddress, bootState string, hostname string, remoteIP string) error {
	return a.S.UpdateHeartbeat(ctx, id, agentVersion, osRelease, addresses, bootState, hostname, remoteIP)
}
func (a *NodeStoreAdapter) UpdatePhase(ctx context.Context, id string, phase string) error {
	return a.S.UpdatePhase(ctx, id, phase)
}
func (a *NodeStoreAdapter) SetGroup(ctx context.Context, nodeID string, groupID *string) error {
	return a.S.SetGroup(ctx, nodeID, groupID)
}
func (a *NodeStoreAdapter) SetLabels(ctx context.Context, nodeID string, labels map[string]string) error {
	return a.S.SetLabels(ctx, nodeID, labels)
}
func (a *NodeStoreAdapter) ClaimNode(ctx context.Context, groupID, claimKey string) (*store.ManagedNode, error) {
	return a.S.ClaimNode(ctx, groupID, claimKey)
}
func (a *NodeStoreAdapter) ReleaseNode(ctx context.Context, nodeID, claimKey string) (bool, error) {
	return a.S.ReleaseNode(ctx, nodeID, claimKey)
}
func (a *NodeStoreAdapter) AdvanceReset(ctx context.Context, nodeID string, fromStates []string, to string, stampLastReset bool) (bool, error) {
	return a.S.AdvanceReset(ctx, nodeID, fromStates, to, stampLastReset)
}
func (a *NodeStoreAdapter) StampResetProgress(ctx context.Context, nodeID string) (bool, error) {
	return a.S.StampResetProgress(ctx, nodeID)
}
func (a *NodeStoreAdapter) FailResetBefore(ctx context.Context, nodeID string, deadline time.Time) (bool, error) {
	return a.S.FailResetBefore(ctx, nodeID, deadline)
}
func (a *NodeStoreAdapter) AdvanceUpgrade(ctx context.Context, nodeID string, fromStates []string, to string, stampLastUpgrade bool) (bool, error) {
	return a.S.AdvanceUpgrade(ctx, nodeID, fromStates, to, stampLastUpgrade)
}
func (a *NodeStoreAdapter) SettleAfterReboot(ctx context.Context, nodeID, bootState string, bootedAt time.Time) error {
	return a.S.SettleAfterReboot(ctx, nodeID, bootState, bootedAt)
}
func (a *NodeStoreAdapter) Delete(ctx context.Context, id string) error {
	return a.S.NodeDelete(ctx, id)
}

// CommandStoreAdapter adapts Store to the store.CommandStore interface.
type CommandStoreAdapter struct{ S *Store }

func (a *CommandStoreAdapter) Create(ctx context.Context, cmd *store.NodeCommand) error {
	return a.S.CommandCreate(ctx, cmd)
}
func (a *CommandStoreAdapter) CreateBatch(ctx context.Context, cmds []*store.NodeCommand) error {
	return a.S.CreateBatch(ctx, cmds)
}
func (a *CommandStoreAdapter) GetByID(ctx context.Context, id string) (*store.NodeCommand, error) {
	return a.S.CommandGetByID(ctx, id)
}
func (a *CommandStoreAdapter) GetPending(ctx context.Context, nodeID string) ([]*store.NodeCommand, error) {
	return a.S.GetPending(ctx, nodeID)
}
func (a *CommandStoreAdapter) MarkDelivered(ctx context.Context, ids []string) error {
	return a.S.MarkDelivered(ctx, ids)
}
func (a *CommandStoreAdapter) ClaimForDelivery(ctx context.Context, id string) (bool, error) {
	return a.S.ClaimForDelivery(ctx, id)
}
func (a *CommandStoreAdapter) UpdateStatus(ctx context.Context, id string, phase string, result string) error {
	return a.S.UpdateStatus(ctx, id, phase, result)
}
func (a *CommandStoreAdapter) UpdateStatusForNode(ctx context.Context, id string, nodeID string, phase string, result string) error {
	return a.S.UpdateStatusForNode(ctx, id, nodeID, phase, result)
}
func (a *CommandStoreAdapter) ListByNode(ctx context.Context, nodeID string) ([]*store.NodeCommand, error) {
	return a.S.ListByNode(ctx, nodeID)
}
func (a *CommandStoreAdapter) ExpireBefore(ctx context.Context, nodeID string, deadline time.Time) error {
	return a.S.CommandExpireBefore(ctx, nodeID, deadline)
}

func (a *CommandStoreAdapter) Delete(ctx context.Context, id string) error {
	return a.S.CommandDelete(ctx, id)
}
func (a *CommandStoreAdapter) DeleteTerminal(ctx context.Context, nodeID string) error {
	return a.S.CommandDeleteTerminal(ctx, nodeID)
}

// BatchStoreAdapter adapts Store to the store.BatchStore interface.
type BatchStoreAdapter struct{ S *Store }

func (a *BatchStoreAdapter) Create(ctx context.Context, b *store.CommandBatch, nodes []*store.BatchNode) error {
	return a.S.BatchCreate(ctx, b, nodes)
}
func (a *BatchStoreAdapter) Get(ctx context.Context, id string) (*store.CommandBatch, error) {
	return a.S.BatchGet(ctx, id)
}
func (a *BatchStoreAdapter) List(ctx context.Context, f store.BatchFilter) ([]*store.CommandBatch, int, error) {
	return a.S.BatchList(ctx, f)
}
func (a *BatchStoreAdapter) BusyNodes(ctx context.Context, nodeIDs []string) ([]string, error) {
	return a.S.BatchBusyNodes(ctx, nodeIDs)
}
func (a *BatchStoreAdapter) Advance(ctx context.Context, batchID string, now time.Time) ([]*store.NodeCommand, error) {
	return a.S.BatchAdvance(ctx, batchID, now)
}
func (a *BatchStoreAdapter) Stop(ctx context.Context, batchID, reason string, now time.Time) error {
	return a.S.BatchStop(ctx, batchID, reason, now)
}
func (a *BatchStoreAdapter) NodeBooted(ctx context.Context, nodeID, bootState string, bootedAt *time.Time, now time.Time) ([]string, error) {
	return a.S.BatchNodeBooted(ctx, nodeID, bootState, bootedAt, now)
}
func (a *BatchStoreAdapter) ActiveIDs(ctx context.Context) ([]string, error) {
	return a.S.BatchActiveIDs(ctx)
}
func (a *BatchStoreAdapter) PostponeExpiredDeadlines(ctx context.Context, now time.Time) error {
	return a.S.BatchPostponeExpiredDeadlines(ctx, now)
}

// ArtifactStoreAdapter adapts Store to the store.ArtifactStore interface.
type ArtifactStoreAdapter struct{ S *Store }

func (a *ArtifactStoreAdapter) Create(ctx context.Context, rec *store.ArtifactRecord) error {
	return a.S.ArtifactCreate(ctx, rec)
}
func (a *ArtifactStoreAdapter) GetByID(ctx context.Context, id string) (*store.ArtifactRecord, error) {
	return a.S.ArtifactGetByID(ctx, id)
}
func (a *ArtifactStoreAdapter) List(ctx context.Context) ([]*store.ArtifactRecord, error) {
	return a.S.ArtifactList(ctx)
}
func (a *ArtifactStoreAdapter) Update(ctx context.Context, rec *store.ArtifactRecord) error {
	return a.S.ArtifactUpdate(ctx, rec)
}
func (a *ArtifactStoreAdapter) UpdatePhaseMessage(ctx context.Context, id, phase, message string) error {
	return a.S.ArtifactUpdatePhaseMessage(ctx, id, phase, message)
}
func (a *ArtifactStoreAdapter) UpdateFiles(ctx context.Context, id string, files []string) error {
	return a.S.ArtifactUpdateFiles(ctx, id, files)
}
func (a *ArtifactStoreAdapter) UpdateNameSaved(ctx context.Context, id string, name *string, saved *bool) error {
	return a.S.ArtifactUpdateNameSaved(ctx, id, name, saved)
}
func (a *ArtifactStoreAdapter) ClearUploadToken(ctx context.Context, id string) error {
	return a.S.ArtifactClearUploadToken(ctx, id)
}
func (a *ArtifactStoreAdapter) Delete(ctx context.Context, id string) error {
	return a.S.ArtifactDelete(ctx, id)
}
func (a *ArtifactStoreAdapter) GetLogs(ctx context.Context, id string) (string, error) {
	return a.S.ArtifactGetLogs(ctx, id)
}
func (a *ArtifactStoreAdapter) AppendLog(ctx context.Context, id string, text string) error {
	return a.S.ArtifactAppendLog(ctx, id, text)
}
func (a *ArtifactStoreAdapter) DeleteByPhase(ctx context.Context, phase string) error {
	return a.S.ArtifactDeleteByPhase(ctx, phase)
}

// GroupStoreAdapter adapts Store to the store.GroupStore interface.
// The Store's GroupStore methods (Create, GetByID, GetByName, List, Update, Delete)
// already have the correct signatures, so this is a thin pass-through.
type GroupStoreAdapter struct{ S *Store }

func (a *GroupStoreAdapter) Create(ctx context.Context, group *store.NodeGroup) error {
	return a.S.Create(ctx, group)
}
func (a *GroupStoreAdapter) GetByID(ctx context.Context, id string) (*store.NodeGroup, error) {
	return a.S.GetByID(ctx, id)
}
func (a *GroupStoreAdapter) GetByName(ctx context.Context, name string) (*store.NodeGroup, error) {
	return a.S.GetByName(ctx, name)
}
func (a *GroupStoreAdapter) List(ctx context.Context) ([]*store.NodeGroup, error) {
	return a.S.List(ctx)
}
func (a *GroupStoreAdapter) Update(ctx context.Context, group *store.NodeGroup) error {
	return a.S.Update(ctx, group)
}
func (a *GroupStoreAdapter) Delete(ctx context.Context, id string) error {
	return a.S.Delete(ctx, id)
}
func (a *GroupStoreAdapter) NodeCounts(ctx context.Context) (map[string]int, error) {
	return a.S.GroupNodeCounts(ctx)
}

// SecureBootKeySetStoreAdapter adapts Store to the store.SecureBootKeySetStore interface.
type SecureBootKeySetStoreAdapter struct{ S *Store }

func (a *SecureBootKeySetStoreAdapter) Create(ctx context.Context, ks *store.SecureBootKeySet) error {
	return a.S.SecureBootKeySetCreate(ctx, ks)
}
func (a *SecureBootKeySetStoreAdapter) GetByID(ctx context.Context, id string) (*store.SecureBootKeySet, error) {
	return a.S.SecureBootKeySetGetByID(ctx, id)
}
func (a *SecureBootKeySetStoreAdapter) GetByName(ctx context.Context, name string) (*store.SecureBootKeySet, error) {
	return a.S.SecureBootKeySetGetByName(ctx, name)
}
func (a *SecureBootKeySetStoreAdapter) List(ctx context.Context) ([]*store.SecureBootKeySet, error) {
	return a.S.SecureBootKeySetList(ctx)
}
func (a *SecureBootKeySetStoreAdapter) Delete(ctx context.Context, id string) error {
	return a.S.SecureBootKeySetDelete(ctx, id)
}

// BMCTargetStoreAdapter adapts Store to the store.BMCTargetStore interface.
type BMCTargetStoreAdapter struct{ S *Store }

func (a *BMCTargetStoreAdapter) Create(ctx context.Context, target *store.BMCTarget) error {
	return a.S.BMCTargetCreate(ctx, target)
}
func (a *BMCTargetStoreAdapter) GetByID(ctx context.Context, id string) (*store.BMCTarget, error) {
	return a.S.BMCTargetGetByID(ctx, id)
}
func (a *BMCTargetStoreAdapter) List(ctx context.Context) ([]*store.BMCTarget, error) {
	return a.S.BMCTargetList(ctx)
}
func (a *BMCTargetStoreAdapter) Update(ctx context.Context, target *store.BMCTarget) error {
	return a.S.BMCTargetUpdate(ctx, target)
}
func (a *BMCTargetStoreAdapter) Delete(ctx context.Context, id string) error {
	return a.S.BMCTargetDelete(ctx, id)
}

// ExtensionStoreAdapter adapts Store to the store.ExtensionStore interface.
type ExtensionStoreAdapter struct{ S *Store }

func (a *ExtensionStoreAdapter) Create(ctx context.Context, e *store.ExtensionRecord) error {
	return a.S.ExtensionCreate(ctx, e)
}
func (a *ExtensionStoreAdapter) GetByID(ctx context.Context, id string) (*store.ExtensionRecord, error) {
	return a.S.ExtensionGetByID(ctx, id)
}
func (a *ExtensionStoreAdapter) List(ctx context.Context) ([]store.ExtensionRecord, error) {
	return a.S.ExtensionList(ctx)
}
func (a *ExtensionStoreAdapter) Delete(ctx context.Context, id string) error {
	return a.S.ExtensionDelete(ctx, id)
}
func (a *ExtensionStoreAdapter) FindLatestReadyByName(ctx context.Context, extType, name string) (*store.ExtensionRecord, error) {
	return a.S.ExtensionFindLatestReadyByName(ctx, extType, name)
}
func (a *ExtensionStoreAdapter) FindByNameAndVersion(ctx context.Context, extType, name, version string) (*store.ExtensionRecord, error) {
	return a.S.ExtensionFindByNameAndVersion(ctx, extType, name, version)
}
func (a *ExtensionStoreAdapter) AppendLog(ctx context.Context, id, chunk string) error {
	return a.S.ExtensionAppendLog(ctx, id, chunk)
}

// ArtifactExtensionBundleStoreAdapter adapts Store to the
// store.ArtifactExtensionBundleStore interface.
type ArtifactExtensionBundleStoreAdapter struct{ S *Store }

func (a *ArtifactExtensionBundleStoreAdapter) ListForArtifact(ctx context.Context, artifactID string) ([]store.ArtifactExtensionBundle, error) {
	return a.S.BundleListForArtifact(ctx, artifactID)
}
func (a *ArtifactExtensionBundleStoreAdapter) ReplaceForArtifact(ctx context.Context, artifactID string, entries []store.ArtifactExtensionBundle) error {
	return a.S.BundleReplaceForArtifact(ctx, artifactID, entries)
}
func (a *ArtifactExtensionBundleStoreAdapter) ArtifactsReferencingExtension(ctx context.Context, extensionName string) ([]string, error) {
	return a.S.BundleArtifactsReferencingExtension(ctx, extensionName)
}

// NodeExtensionStoreAdapter adapts Store to the store.NodeExtensionStore interface.
type NodeExtensionStoreAdapter struct{ S *Store }

func (a *NodeExtensionStoreAdapter) Upsert(ctx context.Context, row *store.NodeExtensionRow) error {
	return a.S.NodeExtensionUpsert(ctx, row)
}
func (a *NodeExtensionStoreAdapter) ListForNode(ctx context.Context, nodeID string) ([]store.NodeExtensionRow, error) {
	return a.S.NodeExtensionListForNode(ctx, nodeID)
}
func (a *NodeExtensionStoreAdapter) ListForExtensionByName(ctx context.Context, extType, name string) ([]store.NodeExtensionRow, error) {
	return a.S.NodeExtensionListForExtensionByName(ctx, extType, name)
}
func (a *NodeExtensionStoreAdapter) DeleteByScope(ctx context.Context, nodeID, extType, name, bootState string) error {
	return a.S.NodeExtensionDeleteByScope(ctx, nodeID, extType, name, bootState)
}
func (a *NodeExtensionStoreAdapter) DeleteByName(ctx context.Context, nodeID, extType, name string) error {
	return a.S.NodeExtensionDeleteByName(ctx, nodeID, extType, name)
}

// DeploymentStoreAdapter adapts Store to the store.DeploymentStore interface.
type DeploymentStoreAdapter struct{ S *Store }

func (a *DeploymentStoreAdapter) Create(ctx context.Context, dep *store.Deployment) error {
	return a.S.DeploymentCreate(ctx, dep)
}
func (a *DeploymentStoreAdapter) GetByID(ctx context.Context, id string) (*store.Deployment, error) {
	return a.S.DeploymentGetByID(ctx, id)
}
func (a *DeploymentStoreAdapter) List(ctx context.Context) ([]*store.Deployment, error) {
	return a.S.DeploymentList(ctx)
}
func (a *DeploymentStoreAdapter) ListByArtifact(ctx context.Context, artifactID string) ([]*store.Deployment, error) {
	return a.S.DeploymentListByArtifact(ctx, artifactID)
}
func (a *DeploymentStoreAdapter) Update(ctx context.Context, dep *store.Deployment) error {
	return a.S.DeploymentUpdate(ctx, dep)
}
func (a *DeploymentStoreAdapter) Delete(ctx context.Context, id string) error {
	return a.S.DeploymentDelete(ctx, id)
}
func (a *DeploymentStoreAdapter) CASEjectState(ctx context.Context, id, from, to string) (bool, error) {
	return a.S.DeploymentCASEjectState(ctx, id, from, to)
}

// SettingsStoreAdapter adapts Store to the store.SettingsStore interface.
type SettingsStoreAdapter struct{ S *Store }

func (a *SettingsStoreAdapter) Get(ctx context.Context, key string) (string, bool, error) {
	return a.S.SettingGet(ctx, key)
}
func (a *SettingsStoreAdapter) Set(ctx context.Context, key, value string) error {
	return a.S.SettingSet(ctx, key, value)
}
func (a *SettingsStoreAdapter) GetAll(ctx context.Context) (map[string]string, error) {
	return a.S.SettingGetAll(ctx)
}

var _ store.BatchStore = (*BatchStoreAdapter)(nil)
