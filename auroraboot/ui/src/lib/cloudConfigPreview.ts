import { parse, stringify } from "yaml";

type YamlMap = Record<string, unknown>;
type YamlValue = unknown;

export interface CloudConfigPreviewInput {
  autoInstall: boolean;
  registerAuroraBoot: boolean;
  groupName: string;
  allowedCommands: readonly string[];
  variant: string;
  kubernetesDistro: string;
  kubernetesEnabled: boolean;
  userMode: "default" | "custom" | "none";
  username: string;
  password: string;
  sshKeys: string;
  extraYAML: string;
  // Real values for the phonehome block. Optional because they load
  // asynchronously (registrationToken) or may be unavailable; fall back to
  // placeholders so the preview never blocks on them.
  registrationUrl?: string;
  registrationToken?: string;
}

const UNSAFE_MERGE_KEYS = new Set(["__proto__", "constructor", "prototype"]);

function isSafeMergeKey(key: string): boolean {
  return !UNSAFE_MERGE_KEYS.has(key);
}

// Mirrors pkg/handlers/artifacts.go mergeYAML: maps merge recursively, slices
// concatenate, scalars from extra YAML override canonical defaults.
function mergeYAML(dst: YamlMap, src: YamlMap): void {
  for (const [k, sv] of Object.entries(src)) {
    if (!isSafeMergeKey(k)) {
      continue;
    }
    if (!Object.prototype.hasOwnProperty.call(dst, k)) {
      dst[k] = sv;
      continue;
    }
    const dv = dst[k];
    if (isMap(dv) && isMap(sv)) {
      mergeYAML(dv, sv);
      continue;
    }
    if (Array.isArray(dv) && Array.isArray(sv)) {
      dst[k] = [...dv, ...sv];
      continue;
    }
    dst[k] = sv;
  }
}

function isMap(v: YamlValue): v is YamlMap {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

// Builds the Review-step cloud-config preview. Logic must stay aligned with
// buildCloudConfig in pkg/handlers/artifacts.go (canonical doc + merged extra).
export function buildCloudConfigPreview(input: CloudConfigPreviewInput): string {
  const doc = Object.create(null) as YamlMap;

  if (input.autoInstall) {
    doc.install = { auto: true, device: "auto", reboot: true };
  }

  if (input.variant === "standard" && input.kubernetesDistro) {
    if (input.kubernetesDistro === "k3s") {
      doc.k3s = { enabled: input.kubernetesEnabled };
    } else if (input.kubernetesDistro === "k0s") {
      doc.k0s = { enabled: input.kubernetesEnabled };
    }
  }

  if (input.registerAuroraBoot) {
    doc.phonehome = {
      url: input.registrationUrl || "<server-url>",
      registration_token: input.registrationToken || "<token>",
      group: input.groupName,
      allowed_commands: [...input.allowedCommands],
    };
  }

  if (input.userMode !== "none") {
    const username =
      input.userMode === "default" || !input.username.trim()
        ? "kairos"
        : input.username;
    const password =
      input.userMode === "default" || !input.password.trim()
        ? "kairos"
        : input.password;
    const sshLines = input.sshKeys
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    const stage = sshLines.length > 0 ? "network" : "initramfs";

    const userEntry: YamlMap = {
      passwd: password,
      groups: ["admin"],
    };
    if (sshLines.length > 0) {
      userEntry.ssh_authorized_keys = sshLines;
    }

    doc.stages = {
      [stage]: [{ users: { [username]: userEntry } }],
    };
  }

  const extra = input.extraYAML.trim();
  if (extra) {
    let body = extra;
    if (body.startsWith("#cloud-config")) {
      body = body.replace(/^#cloud-config\s*\n?/, "");
    }
    try {
      const extraDoc = parse(body);
      if (isMap(extraDoc)) {
        mergeYAML(doc, extraDoc);
      }
    } catch {
      // Invalid extra YAML: show canonical doc only, same as backend fallback
      // behaviour when unmarshal fails (extra is skipped).
    }
  }

  return `#cloud-config\n${stringify(doc)}`;
}

// A cloned artifact's stored cloudConfig is the full baked document, which
// includes the real phonehome url/registration_token in effect at clone
// time. That block must never re-enter the wizard as "extra" YAML: the
// backend already re-injects phonehome from live settings at build time
// (buildCloudConfig in pkg/handlers/artifacts.go), and forwarding a stale
// copy back as extra YAML would let it override those live values with
// whatever was true when the artifact was cloned.
export function stripPhonehome(yamlText: string): string {
  const trimmed = yamlText.trim();
  if (!trimmed) {
    return yamlText;
  }
  let body = trimmed;
  const hadHeader = body.startsWith("#cloud-config");
  if (hadHeader) {
    body = body.replace(/^#cloud-config\s*\n?/, "");
  }
  let doc: YamlValue;
  try {
    doc = parse(body);
  } catch {
    return yamlText;
  }
  if (!isMap(doc) || !("phonehome" in doc)) {
    return yamlText;
  }
  delete doc.phonehome;
  const rest = stringify(doc);
  return hadHeader ? `#cloud-config\n${rest}` : rest;
}
