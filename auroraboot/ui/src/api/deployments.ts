import { apiFetch, apiFetchText } from "./client";

// EjectState is the finalize/eject lifecycle of a deployment. "" means the
// deployment is not armed for eject (policy off); the rest track the auto/manual
// eject through to a terminal ejected / eject-failed.
export type EjectState =
  | ""
  | "pending"
  | "ejecting"
  | "ejected"
  | "eject-failed";

export interface Deployment {
  id: string;
  artifactId: string;
  method: string;
  status: string;
  message: string;
  bmcTargetId: string;
  progress: number;
  startedAt: string;
  completedAt?: string;
  // --- Eject / finalize (P5). Present only on Redfish deployments armed for eject.
  nodeId?: string;
  ejectPolicy?: "off" | "on-phone-home" | "manual";
  ejectState?: EjectState;
  ejectError?: string;
  ejectedAt?: string;
}

export interface BMCTarget {
  id: string;
  name: string;
  endpoint: string;
  vendor: string;
  username: string;
  verifySSL: boolean;
  // systemId optionally pins the target ComputerSystem by its Redfish Id. Leave
  // blank for single-system BMCs; required when the BMC exposes more than one,
  // mirroring the CLI's --system-id.
  systemId?: string;
  // imageUrl optionally overrides the global default image URL for this BMC (the
  // HTTP(S) URL the BMC pulls the ISO from). Blank to use the global default.
  imageUrl?: string;
  nodeId?: string;
  // ejectAfterInstall is the durable default eject policy for this BMC: when true,
  // AuroraBoot ejects the virtual media (and boots to disk) once the freshly
  // installed node phones home, breaking the install loop. Default false (opt-in).
  ejectAfterInstall?: boolean;
  // ejectPowerCycle, when true, makes the eject for this BMC power the machine off
  // before ejecting and back on after, for BMCs/emulators that do not apply a live
  // eject. Default false (in-place eject), correct for hardware that ejects live.
  ejectPowerCycle?: boolean;
  // --- Status cache (server-owned, read-only). Populated by inspect / ping /
  // refresh-all; never sent on create/update. "" means "unknown" (never checked).
  lastStatus?: "" | "reachable" | "unreachable";
  lastError?: string;
  lastInspectAt?: string;
  lastPingAt?: string;
  lastModel?: string;
  lastManufacturer?: string;
  lastSerial?: string;
  lastMemoryGiB?: number;
  lastCpuCount?: number;
  lastFeatures?: string[];
  createdAt: string;
}

// BMCStatus is the per-target reachability payload returned by the ping
// (GET /bmc-targets/:id/status) and refresh-all endpoints. `id` is present only in
// the refresh-all array (the single-target ping omits it; it is implicit in the URL).
export interface BMCStatus {
  id?: string;
  status: "reachable" | "unreachable";
  lastPingAt?: string;
  error?: string;
}

// InspectResult mirrors the server's inspectResponse (POST
// /api/v1/bmc-targets/:id/inspect): the hardware facts AuroraBoot read off a
// saved BMC target, used to drive the deploy pre-flight gate.
export interface InspectResult {
  memoryGiB: number;
  processorCount: number;
  model: string;
  manufacturer: string;
  serialNumber: string;
  supportedFeatures: string[];
}

// DeployProgress is the live deploy-step event broadcast over the UI WebSocket
// ({"type":"deploy-progress",...}). It carries the same step/percent the server
// records on the Deployment row so the Deployments page can update in real time.
export interface DeployProgress {
  deploymentId: string;
  status: string;
  progress: number;
  step: string;
  message: string;
}

export interface NetbootStatus {
  running: boolean;
  artifactId: string;
  /** Bind address, always the wildcard. Display advertisedAddress instead. */
  address: string;
  /** Host clients should reach the netboot server on. */
  advertisedAddress?: string;
  port: string;
}

export const listDeployments = () =>
  apiFetch<Deployment[]>("/api/v1/deployments");

export const getDeployment = (id: string) =>
  apiFetch<Deployment>(`/api/v1/deployments/${id}`);

export const listBMCTargets = () =>
  apiFetch<BMCTarget[]>("/api/v1/bmc-targets");

export const createBMCTarget = (t: {
  name: string;
  endpoint: string;
  vendor: string;
  username: string;
  password: string;
  verifySSL: boolean;
  systemId?: string;
  imageUrl?: string;
  ejectAfterInstall?: boolean;
  ejectPowerCycle?: boolean;
}) => apiFetch<BMCTarget>("/api/v1/bmc-targets", { method: "POST", body: JSON.stringify(t) });

// updateBMCTarget mirrors createBMCTarget but PUTs to an existing target. Leave
// `password` unset (or empty) to keep the stored credential — the backend treats
// a blank password as "no change".
export const updateBMCTarget = (
  id: string,
  t: {
    name: string;
    endpoint: string;
    vendor: string;
    username: string;
    password?: string;
    verifySSL: boolean;
    systemId?: string;
    imageUrl?: string;
    ejectAfterInstall?: boolean;
    ejectPowerCycle?: boolean;
  }
) =>
  apiFetch<BMCTarget>(`/api/v1/bmc-targets/${id}`, {
    method: "PUT",
    body: JSON.stringify(t),
  });

export const deleteBMCTarget = (id: string) =>
  apiFetch(`/api/v1/bmc-targets/${id}`, { method: "DELETE" });

// finalizeDeployment ejects the media and best-effort boots to disk for one
// deployment, regardless of its eject policy (operator override). Returns the
// updated deployment with the new EjectState.
export const finalizeDeployment = (id: string) =>
  apiFetch<Deployment>(`/api/v1/deployments/${id}/finalize`, { method: "POST" });

// ejectBMCTarget is the deployment-less operator escape hatch: eject the virtual
// media of a BMC directly with no eject-state bookkeeping.
export const ejectBMCTarget = (id: string) =>
  apiFetch<{ status: string }>(`/api/v1/bmc-targets/${id}/eject`, { method: "POST" });

export const inspectHardware = (id: string) =>
  apiFetch<InspectResult>(`/api/v1/bmc-targets/${id}/inspect`, { method: "POST" });

// pingBMCTarget runs a session-free reachability check against one BMC's Redfish
// ServiceRoot and returns the freshly-persisted status. It never creates a BMC
// session, so it is cheap to call on demand.
export const pingBMCTarget = (id: string) =>
  apiFetch<BMCStatus>(`/api/v1/bmc-targets/${id}/status`);

// refreshAllBMCTargets pings every saved BMC sequentially (throttled, single
// in-flight server-side) and returns the per-target results. A concurrent call
// while one is already running is rejected by the server with 409.
export const refreshAllBMCTargets = () =>
  apiFetch<BMCStatus[]>("/api/v1/bmc-targets/refresh-all", { method: "POST" });

export const deployRedfish = (
  artifactId: string,
  body: {
    bmcTargetId?: string;
    endpoint?: string;
    username?: string;
    password?: string;
    vendor?: string;
    verifySSL?: boolean;
    systemId?: string;
    ejectAfterInstall?: boolean;
  }
) =>
  apiFetch<Deployment>(`/api/v1/artifacts/${artifactId}/deploy/redfish`, {
    method: "POST",
    body: JSON.stringify(body),
  });

export const getNetbootStatus = () =>
  apiFetch<NetbootStatus>("/api/v1/netboot/status");

export const startNetboot = (artifactId: string) =>
  apiFetch("/api/v1/netboot/start", {
    method: "POST",
    body: JSON.stringify({ artifactId }),
  });

// getNetbootLogs fetches a snapshot of the current (or most recent) PXE
// session's captured output. For live updates, subscribe to the UI
// WebSocket and filter {type: "netboot-log"} envelopes.
export const getNetbootLogs = (): Promise<string> =>
  apiFetchText("/api/v1/netboot/logs");

export const stopNetboot = () =>
  apiFetch("/api/v1/netboot/stop", { method: "POST" });
