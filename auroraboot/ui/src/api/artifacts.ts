import { apiErrorFrom, apiFetch, apiFetchText } from "./client";

/** Discriminates the build pipeline. Empty and "kairos" are the classic path. */
export type ArtifactKind = "" | "kairos";

export interface Artifact {
  id: string;
  name?: string;
  kind?: ArtifactKind;
  saved?: boolean;
  phase: string;
  message: string;
  baseImage: string;
  kairosVersion: string;
  model: string;
  arch?: string;
  variant?: string;
  "allow-insecure-registries"?: boolean;
  iso: boolean;
  cloudImage: boolean;
  netboot: boolean;
  rawDisk: boolean;
  tar: boolean;
  gce: boolean;
  vhd: boolean;
  maas: boolean;
  fips: boolean;
  trustedBoot: boolean;
  kairosInitImage?: string;
  autoInstall: boolean;
  registerAuroraBoot: boolean;
  dockerfile?: string;
  hadronBase?: string;
  hadronFirmware?: string[];
  hadronLayers?: string[];
  hadronExtra?: string;
  // Catalog extensions this artifact was built with, and the catalogs they
  // were resolved against. An empty catalog list means the build read the
  // server's default catalog.
  extensions?: string[];
  extensionsCatalogs?: string[];
  cloudConfig?: string;
  kubernetesDistro?: string;
  kubernetesVersion?: string;
  kubernetesEnabled?: boolean;
  targetGroupId?: string;
  // Uploaded overlay the artifact was built with, as returned by
  // uploadOverlayFiles.
  overlayId?: string;
  containerImage?: string;
  artifacts: string[];
  // Hierarchies the operator declared at build time. Used by the extension
  // builder's "From artifact" cross-check and by tooling that surfaces what
  // overlays an OS image is wired to accept.
  extensionHierarchies?: { sysext: string[]; confext: string[] };
  createdAt: string;
  updatedAt: string;
}

export interface CreateArtifactOutputs {
  iso: boolean;
  cloudImage: boolean;
  netboot: boolean;
  rawDisk: boolean;
  tar: boolean;
  gce: boolean;
  vhd: boolean;
  maas: boolean;
  fips: boolean;
  trustedBoot: boolean;
}

export interface CreateArtifactSigning {
  ukiKeySetId: string;
  ukiSecureBootEnroll: string;
}

export interface CreateArtifactProvisioning {
  autoInstall: boolean;
  registerAuroraBoot: boolean;
  targetGroupId: string;
  userMode?: "default" | "custom" | "none";
  username?: string;
  password?: string;
  sshKeys?: string;
  /**
   * Explicit list baked into phonehome.allowed_commands on the node.
   * Always sent by the ArtifactBuilder UI; omit/null means "use AuroraBoot's
   * safe defaults". Empty array means deny-all (observe-only node).
   */
  allowedCommands?: string[];
}

export interface CreateArtifactInput {
  name?: string;
  baseImage: string;
  kairosVersion: string;
  model: string;
  arch: string;
  variant: string;
  kubernetesDistro?: string;
  kubernetesVersion?: string;
  kubernetesEnabled?: boolean;
  /**
   * Allow pulling the base image from a registry served over plain HTTP or
   * presenting an untrusted/self-signed TLS certificate.
   */
  "allow-insecure-registries"?: boolean;
  dockerfile?: string;
  hadronBase?: string;
  hadronFirmware?: string[];
  hadronLayers?: string[];
  hadronExtra?: string;
  // Catalog extensions to place in the built ISO, as name or name@version.
  // extensionsCatalogs replaces the catalog they resolve against; leaving it
  // out means the server resolves them against its default catalog.
  extensions?: string[];
  extensionsCatalogs?: string[];
  // ID returned by uploadOverlayFiles; the server copies that overlay on top
  // of the built rootfs.
  overlayId?: string;
  kairosInitImage?: string;
  outputs: CreateArtifactOutputs;
  signing: CreateArtifactSigning;
  provisioning: CreateArtifactProvisioning;
  cloudConfig?: string;

  // Extension integration:
  // - extensionHierarchies bakes a SYSTEMD_{SYSEXT,CONFEXT}_HIERARCHIES drop-in
  //   so the OS image accepts overlays on the listed paths out of the box.
  // - bundledExtensions ride along with every upgrade to this artifact.
  extensionHierarchies?: { sysext: string[]; confext: string[] };
  bundledExtensions?: Array<{
    name: string;
    type: "sysext" | "confext";
    pinnedVersion?: string;
    order?: number;
  }>;
}

export interface SecureBootKeySet {
  id: string;
  name: string;
  hasTpmPcrKey: boolean;
  secureBootEnroll: string;
  createdAt: string;
}

export function listSecureBootKeySets(): Promise<SecureBootKeySet[]> {
  return apiFetch("/api/v1/secureboot-keys");
}

export function generateSecureBootKeys(
  name: string
): Promise<SecureBootKeySet> {
  return apiFetch("/api/v1/secureboot-keys/generate", {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export function deleteSecureBootKeySet(id: string): Promise<void> {
  return apiFetch(`/api/v1/secureboot-keys/${id}`, { method: "DELETE" });
}

// exportSecureBootKeySet fetches the tar.gz export for a key set as a Blob.
// Uses raw fetch so we can surface a binary body; apiFetch assumes JSON.
export async function exportSecureBootKeySet(id: string): Promise<{ blob: Blob; filename: string }> {
  const res = await fetch(`/api/v1/secureboot-keys/${id}/export`);
  if (!res.ok) {
    throw new Error(`API error ${res.status}: ${await res.text()}`);
  }
  const disp = res.headers.get("Content-Disposition") || "";
  const match = disp.match(/filename="?([^";]+)"?/);
  const filename = match?.[1] || `keyset-${id}.tar.gz`;
  return { blob: await res.blob(), filename };
}

// importSecureBootKeySet uploads a tar.gz produced by exportSecureBootKeySet
// and returns the new key set record. The optional `name` overrides the name
// from the archive's manifest (useful when importing into an instance that
// already has a key set with the same name).
export async function importSecureBootKeySet(file: File, name?: string): Promise<SecureBootKeySet> {
  const fd = new FormData();
  fd.append("file", file);
  const qs = name ? `?name=${encodeURIComponent(name)}` : "";
  const res = await fetch(`/api/v1/secureboot-keys/import${qs}`, {
    method: "POST",
    body: fd,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `API error ${res.status}`);
  }
  return res.json();
}

export function listArtifacts(): Promise<Artifact[]> {
  return apiFetch<Artifact[]>("/api/v1/artifacts");
}

export function getArtifact(id: string): Promise<Artifact> {
  return apiFetch<Artifact>(`/api/v1/artifacts/${id}`);
}

export function createArtifact(
  input: CreateArtifactInput
): Promise<Artifact> {
  return apiFetch<Artifact>("/api/v1/artifacts", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function getArtifactLogs(id: string): Promise<string> {
  return apiFetchText(`/api/v1/artifacts/${id}/logs`);
}

export function cancelArtifact(id: string): Promise<void> {
  return apiFetch(`/api/v1/artifacts/${id}/cancel`, { method: "POST" });
}

export function artifactDownloadUrl(id: string, filename: string): string {
  return `/api/v1/artifacts/${encodeURIComponent(id)}/download/${encodeURIComponent(filename)}`;
}

export function artifactImageUrl(id: string): string {
  return `/api/v1/artifacts/${encodeURIComponent(id)}/image`;
}

export interface DownloadLink {
  path: string;
  expiresAt: string;
}

// createDownloadLink asks for a link to one file that works without signing in
// for one hour, for pasting into a shell or a ticket.
export function createDownloadLink(id: string, filename: string): Promise<DownloadLink> {
  return apiFetch<DownloadLink>(`/api/v1/artifacts/${encodeURIComponent(id)}/download-link`, {
    method: "POST",
    body: JSON.stringify({ filename }),
  });
}

export function updateArtifact(
  id: string,
  patch: { name?: string; saved?: boolean }
): Promise<Artifact> {
  return apiFetch<Artifact>(`/api/v1/artifacts/${id}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

export function deleteArtifact(id: string): Promise<void> {
  return apiFetch(`/api/v1/artifacts/${id}`, { method: "DELETE" });
}

export function clearFailedArtifacts(): Promise<void> {
  return apiFetch("/api/v1/artifacts/failed", { method: "DELETE" });
}

// Stores the files as a rootfs overlay on the server and returns its ID, to
// be sent as CreateArtifactInput.overlayId.
export async function uploadOverlayFiles(files: FileList | File[]): Promise<string> {
  const formData = new FormData();
  for (const file of Array.from(files)) {
    formData.append("files", file);
  }
  const res = await fetch("/api/v1/artifacts/upload-overlay", {
    method: "POST",
    body: formData,
  });
  if (!res.ok) throw await apiErrorFrom(res);
  const data = await res.json();
  if (typeof data?.id !== "string" || !data.id) throw new Error("Upload returned no overlay ID");
  return data.id;
}

export interface ResolvedBundleEntry {
  name: string;
  type: "sysext" | "confext";
  version: string;
  source: string;
}

// resolveBundle returns the bundled extensions for an artifact in the shape
// the agent consumes inside the upgrade command's `extensions` arg.
export function resolveBundle(artifactId: string): Promise<ResolvedBundleEntry[]> {
  return apiFetch<ResolvedBundleEntry[]>(
    `/api/v1/artifacts/${artifactId}/bundle-resolve`,
    { method: "POST" },
  );
}

// BundleExtensionEntry is a stored bundle row, as the server keeps it. The
// bundle lives in its own table and is not part of the artifact response, so
// reading it back is a second request.
export interface BundleExtensionEntry {
  artifactId: string;
  extensionName: string;
  extensionType: string;
  pinnedVersion?: string;
  order: number;
}

// listBundleExtensions returns the raw bundle rows for an artifact. Unlike
// resolveBundle, which resolves each entry to a concrete version for the
// agent, this is the stored selection, which is what the builder needs to put
// an existing bundle back in front of the operator.
export function listBundleExtensions(
  artifactId: string,
): Promise<BundleExtensionEntry[]> {
  return apiFetch<BundleExtensionEntry[]>(
    `/api/v1/artifacts/${artifactId}/bundle-extensions`,
  );
}
