import { apiFetch } from "./client";
import type { Batch, Rollout } from "./batches";

export interface Node {
  id: string;
  hostname: string;
  machineID: string;
  // null for a node outside any group.
  groupID: string | null;
  group?: { id: string; name: string };
  labels: Record<string, string>;
  phase: string;
  osRelease: Record<string, string> | null;
  agentVersion: string;
  lastHeartbeat: string | null;
  // The IP the server observed the node connect from at register or heartbeat
  // time. Server-side, not agent-reported, so it tracks a DHCP/NAT change even
  // when the agent sends no addresses at all.
  remoteIP?: string;
  // Addresses the agent reported for its interfaces.
  addresses?: { type: string; address: string }[];
  // Which image the node booted: "active", "passive" or "recovery".
  bootState?: string;
  resetState?: string;
  lastReset?: string | null;
  upgradeState?: string;
  lastUpgrade?: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface NodeListParams {
  group_id?: string;  // query param name
  label?: string;
  phase?: string;
}

export interface CommandResult {
  id: string;
  node_id: string;
  command: string;
  args: Record<string, unknown>;
  status: string;
  result?: string;
  created_at: string;
  updated_at: string;
}

export function listNodes(params?: NodeListParams): Promise<Node[]> {
  const query = new URLSearchParams();
  if (params?.group_id) query.set("group", params.group_id);
  if (params?.label) query.set("label", params.label.replace("=", ":"));
  if (params?.phase) query.set("phase", params.phase);
  const qs = query.toString();
  return apiFetch<Node[]>(`/api/v1/nodes${qs ? `?${qs}` : ""}`);
}

export function getNode(id: string): Promise<Node> {
  return apiFetch<Node>(`/api/v1/nodes/${id}`);
}

export function deleteNode(id: string): Promise<void> {
  return apiFetch<void>(`/api/v1/nodes/${id}`, { method: "DELETE" });
}

// Response from POST /api/v1/nodes/:id/decommission. When nodeOnline is
// true, commandID identifies the `unregister` command the UI should watch
// via the UI WebSocket; when false, the UI should fall back to showing the
// manual `kairos-agent phone-home uninstall` instructions.
export interface DecommissionResult {
  commandID: string;
  nodeOnline: boolean;
}

export function decommissionNode(id: string): Promise<DecommissionResult> {
  return apiFetch<DecommissionResult>(`/api/v1/nodes/${id}/decommission`, {
    method: "POST",
  });
}

// setLabels replaces a node's labels. It resolves with nothing: the endpoint
// answers `{"status":"ok"}`, not the updated node, so a caller that wants the
// new state has to re-read the node. Declaring a `Node` here is what let a
// `{status: "ok"}` reach the page as if it were one.
export function setLabels(
  id: string,
  labels: Record<string, string>
): Promise<void> {
  return apiFetch<void>(`/api/v1/nodes/${id}/labels`, {
    method: "PUT",
    body: JSON.stringify({ labels }),
  });
}

// setGroup assigns a node to a group, or takes it out of its group when groupID
// is null. Same contract as setLabels: no node comes back, re-read to see the
// change.
export function setGroup(id: string, groupID: string | null): Promise<void> {
  return apiFetch<void>(`/api/v1/nodes/${id}/group`, {
    method: "PUT",
    body: JSON.stringify({ groupID }),
  });
}

export function sendCommand(
  nodeID: string,
  command: string,
  args?: Record<string, unknown>
): Promise<CommandResult> {
  return apiFetch<CommandResult>(`/api/v1/nodes/${nodeID}/commands`, {
    method: "POST",
    body: JSON.stringify({ command, args }),
  });
}

export function sendBulkCommand(
  selector: { groupID?: string; labels?: Record<string, string>; nodeIDs?: string[] },
  command: string,
  args?: Record<string, unknown>,
  rollout?: Rollout
): Promise<Batch> {
  return apiFetch<Batch>(`/api/v1/nodes/commands`, {
    method: "POST",
    body: JSON.stringify({ selector, command, args, ...rollout }),
  });
}
