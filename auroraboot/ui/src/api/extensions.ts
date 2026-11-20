import { apiFetch, apiFetchText } from "./client";

export type ExtensionType = "sysext" | "confext";

export interface ExtensionSource {
  mode: "artifact" | "image" | "dockerfile";
  artifactId?: string;
  baseImage?: string;
  dockerfile?: string;
  extraSteps?: string;
}

export interface Extension {
  id: string;
  name: string;
  type: ExtensionType;
  phase: string; // Pending | Building | Ready | Error
  message: string;
  arch: string;
  version: string;
  sourceMode: ExtensionSource["mode"];
  sourceArtifactId?: string;
  sourceImage?: string;
  dockerfile?: string;
  extraSteps?: string;
  signingKeySetId?: string;
  hierarchies?: string[];
  serviceReload?: boolean;
  containerImage?: string;
  rawFilename?: string;
  // Per-extension download bearer, minted server-side at build time. Only the
  // admin-authenticated extension routes return it. It is what an install
  // command's source URL carries, so that URL never holds the admin password.
  downloadToken?: string;
  createdAt: string;
  updatedAt: string;
}

export interface CreateExtensionInput {
  name: string;
  type: ExtensionType;
  arch: string;
  version: string;
  source: ExtensionSource;
  signingKeySetId?: string;
  hierarchies?: string[];
  serviceReload?: boolean;
}

export interface ExtensionBuildStatus {
  id: string;
  phase: string;
  message: string;
  rawFile?: string;
  containerImage?: string;
}

export function listExtensions(): Promise<Extension[]> {
  return apiFetch<Extension[]>("/api/v1/extensions");
}

export function getExtension(id: string): Promise<Extension> {
  return apiFetch<Extension>(`/api/v1/extensions/${id}`);
}

export function createExtension(input: CreateExtensionInput): Promise<ExtensionBuildStatus> {
  return apiFetch<ExtensionBuildStatus>("/api/v1/extensions", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function updateExtension(id: string, patch: { name?: string }): Promise<Extension> {
  return apiFetch<Extension>(`/api/v1/extensions/${id}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

export function deleteExtension(id: string): Promise<void> {
  return apiFetch<void>(`/api/v1/extensions/${id}`, { method: "DELETE" });
}

export function cancelExtension(id: string): Promise<void> {
  return apiFetch<void>(`/api/v1/extensions/${id}/cancel`, { method: "POST" });
}

export function getExtensionLogs(id: string): Promise<string> {
  return apiFetchText(`/api/v1/extensions/${id}/logs`);
}

// extensionDownloadPath builds the download route with no credential attached.
// Encode every interpolated component: id/filename can originate from the URL
// route param (untrusted), so they must not be able to inject path or query
// separators into the request target.
export function extensionDownloadPath(id: string, filename: string): string {
  return `/api/v1/extensions/${encodeURIComponent(id)}/download/${encodeURIComponent(filename)}`;
}

// extensionDownloadUrl is the plain download path for the browser's
// <a href download> anchor; the session cookie authenticates the request.
// Anything that travels to a node must use extensionSourceUrl instead.
export function extensionDownloadUrl(id: string, filename: string): string {
  return extensionDownloadPath(id, filename);
}

// extensionSourceUrl builds the URL an install command hands to the agent. It
// carries the extension's own download token, never the admin password: the
// command is pushed to every node in the selector, persisted in the commands
// table and rendered in the dialog's preview. Returns null when the extension
// has no token, so the caller declines to send a source it knows would 401
// rather than silently falling back to an admin credential.
export function extensionSourceUrl(ext: Extension): string | null {
  if (!ext.rawFilename || !ext.downloadToken) return null;
  return `${extensionDownloadPath(ext.id, ext.rawFilename)}?token=${encodeURIComponent(ext.downloadToken)}`;
}

// NodeExtensionRow mirrors store.NodeExtensionRow on the server. Tracks
// which extensions are installed on which node, per boot scope.
export interface NodeExtensionRow {
  nodeId: string;
  name: string;
  type: ExtensionType;
  bootState: "active" | "passive" | "recovery" | "common";
  extensionId?: string;
  version: string;
  installedAt: string;
  updatedAt: string;
}

export function listExtensionsForNode(nodeId: string): Promise<NodeExtensionRow[]> {
  return apiFetch<NodeExtensionRow[]>(`/api/v1/nodes/${nodeId}/extensions`);
}

export function listNodesForExtension(extensionId: string): Promise<NodeExtensionRow[]> {
  return apiFetch<NodeExtensionRow[]>(`/api/v1/extensions/${extensionId}/nodes`);
}
