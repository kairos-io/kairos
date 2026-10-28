// Shared serialization for the "auroraboot build config" portable format
// used by the Artifact Builder (export current form state) and the Artifact
// Detail page (export an already-built artifact). A single kind/version pair
// keeps exports from both paths importable back into the Builder.

import type { Artifact, CreateArtifactInput, SecureBootKeySet } from "@/api/artifacts";
import type { Group } from "@/api/groups";

export const BUILD_CONFIG_KIND = "auroraboot.build-config";
export const BUILD_CONFIG_VERSION = 1;

export type UserMode = "default" | "custom" | "none";

// Phonehome command catalogue. Keep the order stable — the UI renders two
// rows of checkboxes in this exact sequence. `unregister` is in the safe
// set so the Decommission UI works out of the box: it's a self-destruct
// of the management link, not privilege escalation. Mirrors the
// kairos-agent's DefaultAllowedCommands and the backend's
// phonehomeSafeDefaults — keep all three lists aligned.
export const PHONEHOME_SAFE_DEFAULTS: readonly string[] = [
  "upgrade",
  "upgrade-recovery",
  "reboot",
  "unregister",
];

// `extension` installs or removes a systemd system/config extension, so it
// ships code to the node and belongs in the destructive set rather than the
// safe defaults. It has to be listed here to be reachable at all: an operator
// can only tick commands this catalogue renders, and the agent refuses any
// command absent from the baked phonehome.allowed_commands, so without this
// entry the Install Extension dialog sends a command no node will run.
export const PHONEHOME_DESTRUCTIVE_COMMANDS: readonly string[] = [
  "exec",
  "reset",
  "apply-cloud-config",
  "extension",
];

export const PHONEHOME_ALL_COMMANDS: readonly string[] = [
  ...PHONEHOME_SAFE_DEFAULTS,
  ...PHONEHOME_DESTRUCTIVE_COMMANDS,
];

export interface BuildConfigPayload {
  version: typeof BUILD_CONFIG_VERSION;
  kind: typeof BUILD_CONFIG_KIND;
  exportedAt: string;
  name?: string;
  buildMode: "image" | "dockerfile";
  source: {
    baseImage: string;
    kairosVersion: string;
    arch: string;
    model: string;
    variant: string;
    kubernetesDistro?: string;
    kubernetesVersion?: string;
    kubernetesEnabled?: boolean;
    kairosInitImage?: string;
    "allow-insecure-registries"?: boolean;
  };
  dockerfile?: string;
  overlayId?: string;
  outputs: CreateArtifactInput["outputs"];
  signing: {
    ukiKeySetName?: string;
    ukiSecureBootEnroll?: string;
  };
  provisioning: {
    autoInstall: boolean;
    registerAuroraBoot: boolean;
    targetGroupName?: string;
    userMode: UserMode;
    username?: string;
    sshKeys?: string;
    /**
     * Explicit phonehome.allowed_commands list. Always present in fresh
     * exports. Legacy exports may omit it; re-import substitutes the safe
     * defaults in that case.
     */
    allowedCommands?: string[];
  };
  advancedCloudConfig?: string;
}

// Builds a payload from ArtifactBuilder's current live form state.
export function payloadFromBuilder(args: {
  form: CreateArtifactInput;
  buildMode: "image" | "dockerfile";
  groups: Group[];
  keySets: SecureBootKeySet[];
  userMode: UserMode;
  username: string;
  sshKeys: string;
  advancedConfig: string;
}): BuildConfigPayload {
  const { form, buildMode, groups, keySets, userMode, username, sshKeys, advancedConfig } = args;
  const groupName = groups.find((g) => g.id === form.provisioning.targetGroupId)?.name;
  const keySetName = keySets.find((k) => k.id === form.signing.ukiKeySetId)?.name;
  return {
    version: BUILD_CONFIG_VERSION,
    kind: BUILD_CONFIG_KIND,
    exportedAt: new Date().toISOString(),
    name: form.name || undefined,
    buildMode,
    source: {
      baseImage: form.baseImage,
      kairosVersion: form.kairosVersion,
      arch: form.arch,
      model: form.model,
      variant: form.variant,
      kubernetesDistro: form.variant === "standard" ? form.kubernetesDistro || undefined : undefined,
      kubernetesVersion: form.variant === "standard" ? form.kubernetesVersion || undefined : undefined,
      kubernetesEnabled: form.variant === "standard" ? form.kubernetesEnabled ?? true : undefined,
      kairosInitImage: form.kairosInitImage || undefined,
      "allow-insecure-registries":
        buildMode === "image" ? form["allow-insecure-registries"] || undefined : undefined,
    },
    dockerfile: buildMode === "dockerfile" ? form.dockerfile : undefined,
    overlayId: form.overlayId || undefined,
    outputs: { ...form.outputs },
    signing: {
      ukiKeySetName: keySetName,
      ukiSecureBootEnroll: form.signing.ukiSecureBootEnroll || undefined,
    },
    provisioning: {
      autoInstall: form.provisioning.autoInstall,
      registerAuroraBoot: form.provisioning.registerAuroraBoot,
      targetGroupName: groupName,
      userMode,
      username: userMode === "custom" ? username : undefined,
      sshKeys: userMode !== "none" && sshKeys.trim() ? sshKeys : undefined,
      // Verbatim from the form — no "default omitted" compression; the UI
      // state is authoritative and survives the round-trip as-is.
      allowedCommands: [...(form.provisioning.allowedCommands ?? PHONEHOME_SAFE_DEFAULTS)],
    },
    advancedCloudConfig: advancedConfig || undefined,
  };
}

// Builds a payload from an already-built Artifact record. Signing is not
// stored on the artifact (resolved at build time), so the export carries no
// key reference — on re-import the user picks one again.
export function payloadFromArtifact(artifact: Artifact, groups: Group[]): BuildConfigPayload {
  const groupName = artifact.targetGroupId
    ? groups.find((g) => g.id === artifact.targetGroupId)?.name
    : undefined;
  const buildMode: "image" | "dockerfile" = artifact.dockerfile ? "dockerfile" : "image";
  return {
    version: BUILD_CONFIG_VERSION,
    kind: BUILD_CONFIG_KIND,
    exportedAt: new Date().toISOString(),
    name: artifact.name,
    buildMode,
    source: {
      baseImage: artifact.baseImage,
      kairosVersion: artifact.kairosVersion,
      arch: artifact.arch || "amd64",
      model: artifact.model || "generic",
      variant: artifact.variant || "core",
      kubernetesDistro: artifact.kubernetesDistro || undefined,
      kubernetesVersion: artifact.kubernetesVersion || undefined,
      kubernetesEnabled:
        artifact.variant === "standard" ? artifact.kubernetesEnabled ?? true : undefined,
      kairosInitImage: artifact.kairosInitImage || undefined,
      "allow-insecure-registries":
        buildMode === "image" ? artifact["allow-insecure-registries"] || undefined : undefined,
    },
    dockerfile: buildMode === "dockerfile" ? artifact.dockerfile : undefined,
    overlayId: artifact.overlayId || undefined,
    outputs: {
      iso: artifact.iso,
      cloudImage: artifact.cloudImage,
      netboot: artifact.netboot,
      rawDisk: artifact.rawDisk ?? false,
      tar: artifact.tar ?? false,
      gce: artifact.gce ?? false,
      vhd: artifact.vhd ?? false,
      maas: artifact.maas ?? false,
      fips: artifact.fips,
      trustedBoot: artifact.trustedBoot,
    },
    signing: {},
    provisioning: {
      autoInstall: artifact.autoInstall ?? true,
      registerAuroraBoot: artifact.registerAuroraBoot ?? true,
      targetGroupName: groupName,
      userMode: "default",
      // The Artifact record doesn't carry the baked-in allowed list (the
      // backend strips it before storing), so exports from an already-built
      // artifact use the UI's safe defaults on re-import.
      allowedCommands: [...PHONEHOME_SAFE_DEFAULTS],
    },
    advancedCloudConfig: artifact.cloudConfig || undefined,
  };
}

// Shape used to type an imported build-config JSON before it's been fully
// validated. Every nested object is Partial to accept legacy exports that
// omit newer fields; top-level string fields are optional to survive
// user-edited files.
export interface ImportedBuildConfig {
  name?: string;
  buildMode?: string;
  dockerfile?: string;
  overlayId?: string;
  advancedCloudConfig?: string;
  source?: Partial<BuildConfigPayload["source"]>;
  provisioning?: Partial<BuildConfigPayload["provisioning"]>;
  outputs?: Partial<BuildConfigPayload["outputs"]>;
  signing?: Partial<BuildConfigPayload["signing"]>;
}

// Runtime guard for imported JSON. A user-edited config can put non-string
// values where the type says string; those would then flow into controlled
// React inputs and break rendering. This function keeps only the values that
// match the declared shape, dropping the rest to undefined. Callers are
// expected to have already checked `kind` and `version`.
export function sanitizeImportedBuildConfig(
  raw: Record<string, unknown>,
): ImportedBuildConfig {
  const str = (v: unknown): string | undefined =>
    typeof v === "string" ? v : undefined;
  const obj = <T>(v: unknown): Partial<T> | undefined =>
    typeof v === "object" && v !== null && !Array.isArray(v)
      ? (v as Partial<T>)
      : undefined;
  return {
    name: str(raw.name),
    buildMode: str(raw.buildMode),
    dockerfile: str(raw.dockerfile),
    overlayId: str(raw.overlayId),
    advancedCloudConfig: str(raw.advancedCloudConfig),
    source: obj<BuildConfigPayload["source"]>(raw.source),
    provisioning: obj<BuildConfigPayload["provisioning"]>(raw.provisioning),
    outputs: obj<BuildConfigPayload["outputs"]>(raw.outputs),
    signing: obj<BuildConfigPayload["signing"]>(raw.signing),
  };
}

// Triggers a browser download of the given payload as a JSON file.
// Returns the chosen filename so callers can surface it in a toast.
export function downloadBuildConfig(payload: BuildConfigPayload): string {
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  const slug = (payload.name || "build-config").trim().toLowerCase().replace(/[^a-z0-9-]+/g, "-");
  const filename = `${slug || "build-config"}.auroraboot.json`;
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
  return filename;
}
