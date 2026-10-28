import React, { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import {
  createArtifact,
  getArtifact,
  listBundleExtensions,
  listSecureBootKeySets,
  uploadOverlayFiles,
  type CreateArtifactInput,
  type CreateArtifactOutputs,
  type SecureBootKeySet,
} from "@/api/artifacts";
import { listGroups, type Group } from "@/api/groups";
import { listExtensions, type Extension } from "@/api/extensions";
import { getExtensionCatalogSettings, getRegistrationToken } from "@/api/settings";
import { HierarchyChipInput } from "@/components/HierarchyChipInput";
import { ExtensionTypeChip } from "@/components/ExtensionTypeChip";
import {
  PHONEHOME_SAFE_DEFAULTS,
  PHONEHOME_DESTRUCTIVE_COMMANDS,
} from "@/lib/buildConfig";
import { buildCloudConfigPreview, stripPhonehome } from "@/lib/cloudConfigPreview";
import { renderHadronMiddleContent } from "@/lib/hadronContent";
import {
  DEFAULT_EXTENSIONS_CATALOG,
  SUGGESTED_EXTENSION_CATALOGS,
  extensionCatalogChoices,
  isHadronBaseImage,
  LATEST_VERSION,
  catalogExtensionsForArch,
  fetchCatalogExtensions,
  parseExtensionSelection,
  serializeExtensionSelection,
  type CatalogExtensionItem,
} from "@/lib/catalogExtensions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { PageHeader } from "@/components/PageHeader";
import { WizardShell, type WizardStep } from "@/components/wizard/WizardShell";
import { BuildSummary, type BuildSummaryData } from "@/components/wizard/BuildSummary";
import { SegmentedControl } from "@/components/wizard/SegmentedControl";
import { CommandPresets } from "@/components/wizard/CommandPresets";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  Upload,
  X,
  Loader2,
  Check,
  Disc3,
  Wifi,
  ShieldCheck,
  HardDrive,
  Cloud,
  CloudCog,
  Package,
  Download,
  FileUp,
  Cpu,
  Server,
  CircuitBoard,
  Layers,
  MoreHorizontal,
} from "lucide-react";
import { InfoTooltip } from "@/components/InfoTooltip";
import { KubernetesReleasePicker } from "@/components/KubernetesReleasePicker";
import { toast } from "@/hooks/useToast";
import {
  BUILD_CONFIG_KIND,
  BUILD_CONFIG_VERSION,
  downloadBuildConfig,
  payloadFromBuilder,
  sanitizeImportedBuildConfig,
} from "@/lib/buildConfig";
import { PRODUCT_NAME } from "@/lib/branding";

type OutputField = "iso" | "netboot" | "rawDisk" | "cloudImage" | "gce" | "vhd" | "maas" | "tar";
type OutputTone = "install" | "disk" | "archive";
type OutputCardDef = {
  field: OutputField;
  label: string;
  desc: string;
  icon: React.ComponentType<{ className?: string }>;
};

// Tone classes for the ambient "Outputs" chip row under the step indicator.
// Kept in sync with ArtifactDetail.tsx's outputCategories tones so a build
// looks the same wherever you see it.
const OUTPUT_TONE_CLASSES: Record<OutputTone, string> = {
  install: "border-primary/30 bg-primary/10 text-primary",
  disk: "border-sky-500/30 bg-sky-500/10 text-sky-700",
  archive: "border-border bg-muted/60 text-foreground",
};

// A Trusted Boot image has no GRUB and no separate initrd: it boots from a
// signed UKI that the build packs the whole rootfs into. Netboot and the disk
// images are built around GRUB and the initrd, so the image ships only as its
// installer ISO, which the server builds as the UKI ISO, and as a tarball.
const TRUSTED_BOOT_OUTPUTS: OutputField[] = ["iso", "tar"];

// withBootModeOutputs keeps the outputs the selected boot mode can build.
// Every path that sets outputs from outside the Output step goes through it,
// so a clone or an import still sends a request the server takes.
function withBootModeOutputs(outputs: CreateArtifactOutputs): CreateArtifactOutputs {
  const next = { ...outputs };
  if (!next.trustedBoot) return next;
  for (const group of OUTPUT_GROUPS) {
    for (const { field } of group.items) {
      if (!TRUSTED_BOOT_OUTPUTS.includes(field)) next[field] = false;
    }
  }
  return next;
}

const OUTPUT_GROUPS: { title: string; tone: OutputTone; items: OutputCardDef[] }[] = [
  {
    title: "Install Media",
    tone: "install",
    items: [
      { field: "iso", label: "ISO", desc: "Bootable installer image", icon: Disc3 },
      { field: "netboot", label: "Netboot", desc: "PXE network boot artifacts", icon: Wifi },
    ],
  },
  {
    title: "Disk Images",
    tone: "disk",
    items: [
      { field: "rawDisk", label: "Raw Disk", desc: "Flat .raw disk image", icon: HardDrive },
      { field: "cloudImage", label: "Cloud Image", desc: "Generic cloud disk", icon: Cloud },
      { field: "gce", label: "Google Cloud", desc: "GCE-compatible image", icon: CloudCog },
      { field: "vhd", label: "Azure (VHD)", desc: "Azure VHD image", icon: CloudCog },
      { field: "maas", label: "MAAS", desc: "MAAS-deployable image (ddgz)", icon: Server },
    ],
  },
  {
    title: "Archives",
    tone: "archive",
    items: [
      { field: "tar", label: "TAR", desc: "OCI container tarball", icon: Package },
    ],
  },
];

type ArchDef = {
  value: "amd64" | "arm64";
  label: string;
  desc: string;
  icon: React.ComponentType<{ className?: string }>;
};

const ARCHES: ArchDef[] = [
  {
    value: "amd64",
    label: "AMD64",
    desc: "Intel / AMD 64-bit. VMs, servers, most laptops.",
    icon: Cpu,
  },
  {
    value: "arm64",
    label: "ARM64",
    desc: "64-bit ARM. Raspberry Pi, Nvidia Jetson, Apple Silicon.",
    icon: Cpu,
  },
];

type ModelDef = {
  value: string;
  label: string;
  desc: string;
  archs: Array<"amd64" | "arm64">;
  icon: React.ComponentType<{ className?: string }>;
};

const MODELS: ModelDef[] = [
  {
    value: "generic",
    label: "Generic",
    desc: "VMs, generic boards, cloud instances.",
    archs: ["amd64", "arm64"],
    icon: Server,
  },
  {
    value: "rpi3",
    label: "Raspberry Pi 3",
    desc: "Raspberry Pi 3 boards.",
    archs: ["arm64"],
    icon: CircuitBoard,
  },
  {
    value: "rpi4",
    label: "Raspberry Pi 4",
    desc: "Raspberry Pi 4 boards.",
    archs: ["arm64"],
    icon: CircuitBoard,
  },
  {
    value: "nvidia-agx-orin",
    label: "Nvidia AGX Orin",
    desc: "Nvidia Jetson AGX Orin developer kits.",
    archs: ["arm64"],
    icon: Cpu,
  },
  {
    value: "nvidia-orin-nx",
    label: "Nvidia Orin NX",
    desc: "Nvidia Jetson Orin NX modules.",
    archs: ["arm64"],
    icon: Cpu,
  },
];

function modelsForArch(arch: string): ModelDef[] {
  return MODELS.filter((m) => m.archs.includes(arch as "amd64" | "arm64"));
}

type VariantDef = {
  value: "core" | "standard";
  label: string;
  desc: string;
  icon: React.ComponentType<{ className?: string }>;
};

const VARIANTS: VariantDef[] = [
  {
    value: "core",
    label: "Core",
    desc: "Minimal OS, cloud-init only. Bring your own workloads.",
    icon: Package,
  },
  {
    value: "standard",
    label: "Standard",
    desc: "OS bundled with a Kubernetes distribution (K3s or K0s).",
    icon: Layers,
  },
];

interface BuildTemplate {
  name: string;
  description: string;
  values: Partial<CreateArtifactInput>;
}

// renovate: datasource=docker depName=ghcr.io/kairos-io/hadron extractVersion=^(?<version>v\d+\.\d+\.\d+)
const HADRON_VERSION = "v0.6.4";
// renovate: datasource=github-releases depName=kairos-io/kairos
const KAIROS_VERSION = "v4.5.0";
// renovate: datasource=docker depName=ubuntu
const UBUNTU_VERSION = "26.04";
// renovate: datasource=docker depName=fedora
const FEDORA_VERSION = "44";
// renovate: datasource=docker depName=opensuse/leap
const OPENSUSE_LEAP_VERSION = "16.1";
// renovate: datasource=docker depName=debian
const DEBIAN_VERSION = "13";
// renovate: datasource=docker depName=alpine
const ALPINE_VERSION = "3.24";
// renovate: datasource=docker depName=rockylinux
const ROCKYLINUX_VERSION = "9";

// Displayed name for the Hadron template tile. Kept as a named const so
// call sites that branch on this template (composer expander, compose-on-Next,
// clone rehydration) stay greppable.
const HADRON_TEMPLATE_NAME = `Hadron ${HADRON_VERSION}`;

const DEFAULT_HADRON_BASE = `ghcr.io/kairos-io/hadron:${HADRON_VERSION}`;

// Hadron compose panel catalogs are fetched live from upstream (same sources
// the deleted backend catalog talked to): GitHub releases for hadron-firmware,
// the pages releases.json for hadron-layers. Both endpoints send permissive
// CORS headers, so the browser can hit them directly and we don't need a
// server-side proxy.
type HadronFirmwareItem = { name: string; image: string; version: string; releaseTag: string };
type HadronLayerItem = { name: string; title?: string; description?: string; image: string; latest?: string };

const HADRON_FIRMWARE_URL = "https://kairos-io.github.io/hadron-firmware/data.json";
// The composer's software-layer list and the build-time extension picker read
// the same published index, so the URL lives in one place.
const HADRON_LAYERS_URL = DEFAULT_EXTENSIONS_CATALOG;
const HADRON_RELEASES_URL = "https://api.github.com/repos/kairos-io/hadron/releases?per_page=30";
const HADRON_CUSTOM_TAG_SENTINEL = "__custom__";

// fetchHadronBaseTags returns the ghcr.io/kairos-io/hadron tags an operator
// can pick from. We call the GitHub Releases API (CORS-open) rather than
// ghcr.io's v2 tag list (which requires an anon-token dance and is not
// CORS-friendly). Draft / prerelease tags are filtered so the dropdown only
// shows shipped versions; `main` is prepended so a fresh-tree build is still
// one click away.
async function fetchHadronBaseTags(): Promise<string[]> {
  const resp = await fetch(HADRON_RELEASES_URL, {
    headers: { Accept: "application/vnd.github+json" },
  });
  if (!resp.ok) throw new Error(`hadron releases: ${resp.status}`);
  const releases = (await resp.json()) as Array<{
    tag_name: string;
    draft?: boolean;
    prerelease?: boolean;
  }>;
  const tags = releases
    .filter((r) => !r.draft && !r.prerelease)
    .map((r) => r.tag_name);
  return ["main", ...tags];
}

// fetchHadronFirmware reads the pages-published data.json that hadron-firmware
// keeps in sync with each release. Payload shape:
//   { releases: [ { tag, images: { <name>: "<ref>", ... } }, ... ] }
// Every image already carries the tag baked into the ref string so we don't
// have to reassemble ghcr paths ourselves. We keep the newest release only
// (releases[0]) — older tags are still selectable via the custom-ref input.
async function fetchHadronFirmware(): Promise<HadronFirmwareItem[]> {
  const resp = await fetch(HADRON_FIRMWARE_URL);
  if (!resp.ok) throw new Error(`firmware data: ${resp.status}`);
  const payload = (await resp.json()) as {
    releases?: Array<{
      tag: string;
      prerelease?: boolean;
      images?: Record<string, string>;
    }>;
  };
  const releases = (payload.releases ?? []).filter((r) => !r.prerelease);
  if (releases.length === 0) return [];
  // data.json emits releases newest-first, but sort defensively so we don't
  // depend on the publisher's ordering.
  releases.sort((a, b) => (a.tag > b.tag ? -1 : a.tag < b.tag ? 1 : 0));
  const latest = releases[0];
  const items: HadronFirmwareItem[] = [];
  for (const [name, ref] of Object.entries(latest.images ?? {})) {
    // Ref shape is `<image>:<version>` — split on the LAST `:` so registry
    // ports (host:5000/...) don't confuse the parser.
    const colon = ref.lastIndexOf(":");
    const image = colon > 0 ? ref.slice(0, colon) : ref;
    const version = colon > 0 ? ref.slice(colon + 1) : latest.tag;
    items.push({ name, image, version, releaseTag: latest.tag });
  }
  items.sort((a, b) => (a.name < b.name ? -1 : 1));
  return items;
}

async function fetchHadronLayers(): Promise<HadronLayerItem[]> {
  const resp = await fetch(HADRON_LAYERS_URL);
  if (!resp.ok) throw new Error(`layers releases: ${resp.status}`);
  const payload = (await resp.json()) as {
    layers?: Array<{
      name: string;
      title?: string;
      description?: string;
      image: string;
      latest?: string;
    }>;
  };
  const items = (payload.layers ?? []).map((l) => ({
    name: l.name,
    title: l.title,
    description: l.description,
    image: l.image,
    latest: l.latest,
  }));
  items.sort((a, b) => (a.name < b.name ? -1 : 1));
  return items;
}

const TEMPLATES: BuildTemplate[] = [
  {
    name: HADRON_TEMPLATE_NAME,
    description: "Kairos edge OS, auto-kairosified, ISO",
    values: {
      baseImage: DEFAULT_HADRON_BASE,
      kairosVersion: KAIROS_VERSION,
      model: "generic",
      arch: "amd64",
      variant: "core",
      outputs: { iso: true, cloudImage: false, netboot: false, rawDisk: false, tar: false, gce: false, vhd: false, maas: false, fips: false, trustedBoot: false },
    },
  },
  {
    name: `Ubuntu ${UBUNTU_VERSION}`,
    description: "Ubuntu base, auto-kairosified, ISO",
    values: {
      baseImage: `ubuntu:${UBUNTU_VERSION}`,
      kairosVersion: KAIROS_VERSION,
      model: "generic",
      arch: "amd64",
      variant: "core",
      outputs: { iso: true, cloudImage: false, netboot: false, rawDisk: false, tar: false, gce: false, vhd: false, maas: false, fips: false, trustedBoot: false },
    },
  },
  {
    name: `Fedora ${FEDORA_VERSION}`,
    description: "Fedora base, auto-kairosified, ISO",
    values: {
      baseImage: `fedora:${FEDORA_VERSION}`,
      kairosVersion: KAIROS_VERSION,
      model: "generic",
      arch: "amd64",
      variant: "core",
      outputs: { iso: true, cloudImage: false, netboot: false, rawDisk: false, tar: false, gce: false, vhd: false, maas: false, fips: false, trustedBoot: false },
    },
  },
  {
    name: `openSUSE Leap ${OPENSUSE_LEAP_VERSION}`,
    description: "openSUSE Leap base, auto-kairosified, ISO",
    values: {
      baseImage: `opensuse/leap:${OPENSUSE_LEAP_VERSION}`,
      kairosVersion: KAIROS_VERSION,
      model: "generic",
      arch: "amd64",
      variant: "core",
      outputs: { iso: true, cloudImage: false, netboot: false, rawDisk: false, tar: false, gce: false, vhd: false, maas: false, fips: false, trustedBoot: false },
    },
  },
  {
    name: "Debian 12",
    description: "Debian Bookworm base, auto-kairosified, ISO",
    values: {
      baseImage: `debian:${DEBIAN_VERSION}`,
      kairosVersion: KAIROS_VERSION,
      model: "generic",
      arch: "amd64",
      variant: "core",
      outputs: { iso: true, cloudImage: false, netboot: false, rawDisk: false, tar: false, gce: false, vhd: false, maas: false, fips: false, trustedBoot: false },
    },
  },
  {
    name: "Alpine 3.21",
    description: "Alpine Linux base, auto-kairosified, ISO",
    values: {
      baseImage: `alpine:${ALPINE_VERSION}`,
      kairosVersion: KAIROS_VERSION,
      model: "generic",
      arch: "amd64",
      variant: "core",
      outputs: { iso: true, cloudImage: false, netboot: false, rawDisk: false, tar: false, gce: false, vhd: false, maas: false, fips: false, trustedBoot: false },
    },
  },
  {
    name: "Rocky Linux 9",
    description: "Rocky Linux 9 base, auto-kairosified, ISO",
    values: {
      baseImage: `rockylinux:${ROCKYLINUX_VERSION}`,
      kairosVersion: KAIROS_VERSION,
      model: "generic",
      arch: "amd64",
      variant: "core",
      outputs: { iso: true, cloudImage: false, netboot: false, rawDisk: false, tar: false, gce: false, vhd: false, maas: false, fips: false, trustedBoot: false },
    },
  },
  {
    name: "Custom",
    description: "Start from scratch",
    values: {},
  },
];


const EMPTY_OUTPUTS = {
  iso: false,
  cloudImage: false,
  netboot: false,
  rawDisk: false,
  tar: false,
  gce: false,
  vhd: false,
  maas: false,
  fips: false,
  trustedBoot: false,
};

const EMPTY_SIGNING = {
  ukiKeySetId: "",
  ukiSecureBootEnroll: "if-safe",
};

const EMPTY_PROVISIONING = {
  autoInstall: true,
  registerAuroraBoot: true,
  targetGroupId: "",
  // Start with the safe-default command set pre-selected; the UI always
  // submits this list verbatim so the baked cloud-config is authoritative.
  allowedCommands: [...PHONEHOME_SAFE_DEFAULTS] as string[],
};

// Default artifact version when the user doesn't provide one. This value
// ends up in the image as /etc/kairos-release's KAIROS_RELEASE and is
// what auroraboot compares against when deciding whether a node needs an
// upgrade, so we give brand-new builds a sensible starting version
// instead of leaving the field blank.
const DEFAULT_ARTIFACT_VERSION = "v1.0";

const EMPTY_FORM: CreateArtifactInput = {
  name: "",
  baseImage: "",
  kairosVersion: DEFAULT_ARTIFACT_VERSION,
  model: "generic",
  arch: "amd64",
  variant: "core",
  kubernetesDistro: "",
  kubernetesVersion: "",
  kubernetesEnabled: true,
  "allow-insecure-registries": false,
  dockerfile: "",
  overlayId: "",
  kairosInitImage: "",
  outputs: { ...EMPTY_OUTPUTS },
  signing: { ...EMPTY_SIGNING },
  provisioning: { ...EMPTY_PROVISIONING },
  cloudConfig: "",
  extensionHierarchies: { sysext: [], confext: [] },
  bundledExtensions: [],
};

type UserMode = "default" | "custom" | "none";

// FieldError pairs a validation error with the form field and wizard step
// it belongs to, so we can jump the user straight to the offending input
// instead of just showing a red list at the top of the page.
// The wizard's steps, in order. Each one covers a single topic.
// eslint-disable-next-line react-refresh/only-export-components -- the step keys are part of the page contract
export const BUILDER_STEPS = ["base", "system", "extensions", "access", "output", "review"] as const;
export type BuilderStep = (typeof BUILDER_STEPS)[number];

const STEP_LABELS: Record<BuilderStep, string> = {
  base: "Base",
  system: "System",
  extensions: "Extensions",
  access: "Access",
  output: "Output",
  review: "Review",
};

type FieldError = { field: string; step: BuilderStep; message: string };

// Per-command descriptions surfaced below each checkbox. Kept short so the
// row matches the "Auto-install" / "Register" rhythm in the parent card —
// a single line of secondary copy, not a paragraph.
const COMMAND_DESCRIPTIONS: Record<string, string> = {
  upgrade: "Pull a new OS image and reboot into it.",
  "upgrade-recovery": "Update the recovery partition without rebooting.",
  reboot: "Reboot the node on demand.",
  unregister: "Stop the phone-home service and clean up its files (used by Delete).",
  exec: "Run arbitrary shell commands on the node.",
  reset: "Factory-reset the node, wiping persistent data.",
  "apply-cloud-config": "Write a new cloud-config to the OEM partition.",
};

// AllowedCommandsPicker renders phonehome.allowed_commands as two stacked
// groups of checkboxes. Safe commands are listed first at the card's baseline
// rhythm; the destructive group sits below a divider so operators have to
// cross a visible boundary before enabling anything that can wipe or own a
// node. The list is submitted verbatim — an empty selection is valid
// (observe-only) but we flag it because it's almost always a mistake.
function AllowedCommandsPicker({
  value,
  onChange,
}: {
  value: string[];
  onChange: (next: string[]) => void;
}) {
  const set = new Set(value);
  const toggle = (cmd: string) => {
    const next = new Set(set);
    if (next.has(cmd)) {
      next.delete(cmd);
    } else {
      next.add(cmd);
    }
    // Preserve the canonical order (safe → destructive) so the emitted YAML
    // is stable regardless of click order.
    const ordered = [
      ...PHONEHOME_SAFE_DEFAULTS,
      ...PHONEHOME_DESTRUCTIVE_COMMANDS,
    ].filter((c) => next.has(c));
    onChange(ordered);
  };

  const commandRow = (cmd: string) => (
    <div key={cmd}>
      <label className="flex items-center gap-2 text-sm font-medium">
        <input
          type="checkbox"
          checked={set.has(cmd)}
          onChange={() => toggle(cmd)}
          className="rounded border-input"
        />
        <code className="font-mono">{cmd}</code>
      </label>
      <p className="text-xs text-muted-foreground mt-1 ml-6">
        {COMMAND_DESCRIPTIONS[cmd]}
      </p>
    </div>
  );

  return (
    <div>
      <div className="space-y-3">
        {PHONEHOME_SAFE_DEFAULTS.map(commandRow)}
      </div>

      <div className="mt-4 pt-3 border-t border-border/60 space-y-3">
        <p className="text-xs text-muted-foreground">
          Destructive — opt in per fleet.
        </p>
        {PHONEHOME_DESTRUCTIVE_COMMANDS.map(commandRow)}
      </div>

      {value.length === 0 && (
        <div className="mt-3 rounded-md border border-amber-400/60 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-400/40 dark:bg-amber-950/60 dark:text-amber-100">
          No commands selected — the node will still register and send
          heartbeats, but no remote management will be possible. If you
          don&apos;t need any management, untick{" "}
          <strong>Register with {PRODUCT_NAME}</strong> instead.
        </div>
      )}
    </div>
  );
}

export function ArtifactBuilder() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [groups, setGroups] = useState<Group[]>([]);
  const [keySets, setKeySets] = useState<SecureBootKeySet[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState("");
  const [buildMode, setBuildMode] = useState<"image" | "dockerfile">("image");
  const [form, setForm] = useState<CreateArtifactInput>({ ...EMPTY_FORM, outputs: { ...EMPTY_OUTPUTS }, signing: { ...EMPTY_SIGNING }, provisioning: { ...EMPTY_PROVISIONING } });
  const [cloneSource, setCloneSource] = useState("");
  // A clone reads its source's bundle in a second request. When that read
  // fails the form cannot tell an empty bundle from an unknown one, so the
  // failure is recorded and shown rather than rendered as "no extensions".
  const [cloneBundleUnavailable, setCloneBundleUnavailable] = useState(false);
  const [customModel, setCustomModel] = useState(false);
  // Steps whose errors are on screen: a step joins once Next was tried on it,
  // and "all" follows a submit attempt. The messages themselves are computed
  // from the current state on every render, so an error goes away as soon as
  // its field is valid again.
  const [shownErrorSteps, setShownErrorSteps] = useState<Set<BuilderStep> | "all">(() => new Set());
  // What the create handler answered when it refused the build. The form's own
  // errors are recomputed from state, but this one comes from the server and
  // nothing on screen would otherwise say the build was rejected. Cleared on
  // the next submit, so a corrected build does not carry the old message.
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [overlayFiles, setOverlayFiles] = useState<string[]>([]);
  const [overlayUploading, setOverlayUploading] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const overlayInputRef = useRef<HTMLInputElement>(null);
  const [step, setStep] = useState<BuilderStep>("base");
  // The furthest step reached, so the stepper can jump forward again to a
  // step the user already saw.
  const [maxReached, setMaxReached] = useState(0);

  // Hadron composer state. Firmware, layers, and the extra-Dockerfile
  // textarea populate an optional advanced expander on the System step. When any of
  // those three are non-empty, the Next handler stitches them into a
  // Dockerfile and switches the build to Dockerfile mode; otherwise the
  // template-prefilled Hadron base image runs the normal image build path.
  const [hadronFirmware, setHadronFirmware] = useState<string[]>([]);
  const [hadronLayers, setHadronLayers] = useState<string[]>([]);
  const [hadronExtra, setHadronExtra] = useState("");
  const [firmwareDraft, setFirmwareDraft] = useState("");
  const [layerDraft, setLayerDraft] = useState("");
  const [firmwareQuery, setFirmwareQuery] = useState("");
  const [layerQuery, setLayerQuery] = useState("");
  const [firmwareCatalog, setFirmwareCatalog] = useState<HadronFirmwareItem[]>([]);
  const [layersCatalog, setLayersCatalog] = useState<HadronLayerItem[]>([]);
  const [firmwareCatalogState, setFirmwareCatalogState] = useState<"idle" | "loading" | "ready" | "error">("idle");
  const [layersCatalogState, setLayersCatalogState] = useState<"idle" | "loading" | "ready" | "error">("idle");
  // Catalog extensions materialized into the built ISO. The catalog in force
  // is derived, not stored: the flavor decides it, because the default
  // catalog publishes Hadron extensions only and a configured catalog is
  // per-flavor. Once the operator picks or types one
  // (extensionsCatalogTouched), that override wins and a flavor change no
  // longer replaces it. Deriving rather than syncing is what makes the
  // catalog right on every step, including a stepper jump from Base to
  // Review that never re-opens Extensions.
  const [extensionsCatalogOverride, setExtensionsCatalogOverride] = useState("");
  const [extensionsCatalogTouched, setExtensionsCatalogTouched] = useState(false);
  // The catalogs given with `web --extensions-catalog` and saved in
  // Settings, in that order.
  const [configuredCatalogs, setConfiguredCatalogs] = useState<string[]>([]);
  const [extensionsCatalogItems, setExtensionsCatalogItems] = useState<CatalogExtensionItem[]>([]);
  const [extensionsCatalogLoadState, setExtensionsCatalogLoadState] = useState<"idle" | "loading" | "ready" | "error">("idle");
  // The catalog those entries were read from. When the flavor moves the
  // catalog on, they describe the old one, so they are stale and no effect
  // is needed to clear them.
  const [loadedCatalogURL, setLoadedCatalogURL] = useState("");
  // name -> version, where LATEST_VERSION means "whatever the catalog calls
  // latest at build time".
  const [selectedExtensions, setSelectedExtensions] = useState<Record<string, string>>({});
  // Hadron base image: either an official release tag (picked from a dropdown
  // populated by the GH releases API) or a fully custom ref string typed by
  // the operator. hadronBaseTag holds the dropdown value; hadronBaseCustom
  // holds the free-form string. The effective ref is derived at render time.
  // Default to the Renovate-tracked HADRON_VERSION so the composer's derived
  // ref matches the template prefill from the start.
  const [hadronBaseTags, setHadronBaseTags] = useState<string[]>([HADRON_VERSION]);
  const [hadronBaseTag, setHadronBaseTag] = useState<string>(HADRON_VERSION);
  const [hadronBaseCustom, setHadronBaseCustom] = useState<string>("");
  // Expander default is closed. The advanced composer opens on demand from
  // the System step; clone rehydration flips it open when the source artifact carried
  // firmware / layers / extra content.
  const [hadronAdvancedOpen, setHadronAdvancedOpen] = useState(false);

  // Lazy-load the upstream catalogs the first time the Hadron panel opens.
  // Both live on public endpoints with permissive CORS, so no backend proxy
  // needed. Kept lazy so the wizard's other paths don't pay the network cost.
  // Called from the event handlers that transition the template into Hadron,
  // rather than an effect, to keep the "start loading" state update off the
  // synchronous render path.
  function startHadronCatalogs() {
    if (firmwareCatalogState === "idle") {
      setFirmwareCatalogState("loading");
      fetchHadronFirmware()
        .then((items) => {
          setFirmwareCatalog(items);
          setFirmwareCatalogState("ready");
        })
        .catch(() => setFirmwareCatalogState("error"));
    }
    if (layersCatalogState === "idle") {
      setLayersCatalogState("loading");
      fetchHadronLayers()
        .then((items) => {
          setLayersCatalog(items);
          setLayersCatalogState("ready");
        })
        .catch(() => setLayersCatalogState("error"));
    }
  }

  // loadExtensionsCatalog reads the catalog at `url`, which is also what a
  // build with no catalog of its own reads, so the list the operator picks
  // from is the list the build resolves against. An unreadable catalog is
  // surfaced rather than swallowed: silently showing an empty picker would
  // read as "this catalog publishes nothing".
  function loadExtensionsCatalog(url: string) {
    const source = url.trim();
    if (source === "") {
      setExtensionsCatalogLoadState("error");
      return;
    }
    setLoadedCatalogURL(source);
    setExtensionsCatalogLoadState("loading");
    fetchCatalogExtensions(source)
      .then((items) => {
        setExtensionsCatalogItems(items);
        setExtensionsCatalogLoadState("ready");
      })
      .catch(() => {
        setExtensionsCatalogItems([]);
        setExtensionsCatalogLoadState("error");
      });
  }

  // goToStep is the one way the wizard changes step, so reaching Extensions
  // can start the catalog fetch. Driven from the navigation handlers rather
  // than an effect, for the same reason startHadronCatalogs is: it keeps the
  // "start loading" state update off the synchronous render path.
  function goToStep(next: BuilderStep) {
    setStep(next);
    setMaxReached((prev) => Math.max(prev, BUILDER_STEPS.indexOf(next)));
    if (next !== "extensions") return;
    if (extensionsCatalogState === "idle" && extensionsCatalog.trim() !== "") {
      loadExtensionsCatalog(extensionsCatalog);
    }
  }

  // pickExtensionsCatalog is an explicit operator choice: it sticks across
  // flavor changes and is read right away.
  function pickExtensionsCatalog(url: string) {
    setExtensionsCatalogOverride(url);
    setExtensionsCatalogTouched(true);
    loadExtensionsCatalog(url);
  }

  // Fetch the hadron release tag list once the Hadron template is picked.
  // Silent-fallback keeps HADRON_VERSION selectable when the API is rate
  // limited. We splice HADRON_VERSION into the fetched list so the current
  // Renovate-tracked release is always pickable even if the API misses it.
  useEffect(() => {
    if (selectedTemplate !== HADRON_TEMPLATE_NAME) return;
    fetchHadronBaseTags()
      .then((tags) => {
        const merged = tags.includes(HADRON_VERSION) ? tags : [...tags, HADRON_VERSION];
        if (merged.length > 0) setHadronBaseTags(merged);
      })
      .catch(() => {});
  }, [selectedTemplate]);

  // Effective base image ref resolved from the dropdown + custom fields.
  // Custom ref wins when the sentinel is selected; otherwise the dropdown
  // value maps onto the well-known ghcr.io path.
  const hadronBase =
    hadronBaseTag === HADRON_CUSTOM_TAG_SENTINEL
      ? hadronBaseCustom.trim()
      : hadronBaseTag
        ? `ghcr.io/kairos-io/hadron:${hadronBaseTag}`
        : "";

  // The default catalog publishes Hadron extensions only, so which catalogs
  // the Extensions step offers depends on the flavor. A Custom build from a
  // Hadron image counts as Hadron.
  const hadronBuild =
    selectedTemplate === HADRON_TEMPLATE_NAME || isHadronBaseImage(form.baseImage);
  const catalogChoices = extensionCatalogChoices(hadronBuild, configuredCatalogs);
  // The catalog in force. An override the operator picked or typed wins;
  // otherwise it is the flavor's own catalog, so changing the flavor moves
  // it wherever that change was made. Reading the catalog stays lazy in
  // goToStep, so nothing is fetched before the Extensions step is opened.
  const extensionsCatalog = extensionsCatalogTouched
    ? extensionsCatalogOverride
    : (catalogChoices[0] ?? "");
  // Entries read from a catalog that is no longer in force describe the
  // previous flavor, so they are not offered and the step counts as unread.
  const catalogEntriesAreCurrent = loadedCatalogURL === extensionsCatalog.trim();
  const extensionsCatalogState = catalogEntriesAreCurrent ? extensionsCatalogLoadState : "idle";

  // The catalog entries that publish an artifact for the architecture being
  // built, and the request list derived from the operator's picks.
  const availableExtensions = useMemo(
    () =>
      catalogEntriesAreCurrent ? catalogExtensionsForArch(extensionsCatalogItems, form.arch) : [],
    [catalogEntriesAreCurrent, extensionsCatalogItems, form.arch],
  );
  // Serialized over the selection itself, not over the visible list: changing
  // the architecture must not quietly drop a pick. The card reports the
  // conflict instead, via unavailableExtensions below.
  const selectedExtensionNames = serializeExtensionSelection(
    selectedExtensions,
    Object.keys(selectedExtensions).sort(),
  );
  // With no catalog there is nothing to resolve the picks against, so they
  // are left out of the build rather than resolved against the Hadron
  // default. The card says so.
  const catalogMissing = extensionsCatalog.trim() === "";
  const unavailableExtensions = Object.keys(selectedExtensions)
    .filter((name) => {
      const item = availableExtensions.find((i) => i.name === name);
      if (!item) return extensionsCatalogState === "ready";
      const version = selectedExtensions[name];
      return version !== LATEST_VERSION && !item.versions.some((v) => v.version === version);
    })
    .sort();

  // Wrapper setters that also push the new ref into form.baseImage. The
  // composer's version selector is the only place users edit the Hadron base
  // ref, so syncing at the setter keeps form state authoritative for the
  // plain (non-composed) build path without an effect-driven cascade.
  function updateHadronBaseTag(next: string) {
    setHadronBaseTag(next);
    if (buildMode === "dockerfile") return;
    const ref =
      next === HADRON_CUSTOM_TAG_SENTINEL
        ? hadronBaseCustom.trim()
        : next
          ? `ghcr.io/kairos-io/hadron:${next}`
          : "";
    if (!ref) return;
    setForm((prev) => (prev.baseImage === ref ? prev : { ...prev, baseImage: ref }));
  }
  function updateHadronBaseCustom(next: string) {
    setHadronBaseCustom(next);
    if (hadronBaseTag !== HADRON_CUSTOM_TAG_SENTINEL) return;
    if (buildMode === "dockerfile") return;
    const ref = next.trim();
    if (!ref) return;
    setForm((prev) => (prev.baseImage === ref ? prev : { ...prev, baseImage: ref }));
  }

  // Refs to the fields validation can complain about. Populated via
  // bindRef(key). Non-focusable entries (e.g. the outputs container) are
  // scrolled into view but not focused.
  const fieldRefs = useRef<Record<string, HTMLElement | null>>({});
  const bindRef = useCallback(
    (key: string) => (el: HTMLElement | null) => {
      fieldRefs.current[key] = el;
    },
    [],
  );
  // When non-null, the effect below scrolls + focuses this field after the
  // next render. Cleared back to null in the effect so it fires exactly
  // once per focus request.
  const [focusTarget, setFocusTarget] = useState<string | null>(null);

  // User/SSH config state
  const [userMode, setUserMode] = useState<UserMode>("default");
  const [username, setUsername] = useState("kairos");
  const [password, setPassword] = useState("kairos");
  const [sshKeys, setSshKeys] = useState("");

  // Advanced cloud-config
  const [advancedConfig, setAdvancedConfig] = useState("");
  const [showAdvanced, setShowAdvanced] = useState(false);
  // The preview content the "View full" toggle was opened on. The preview
  // shows in full only while its content still matches, so a changed
  // preview always starts collapsed again.
  const [expandedCloudConfigPreview, setExpandedCloudConfigPreview] = useState<string | null>(null);

  // Real phonehome values for the Review-step preview, same source Import.tsx
  // uses for its curl command. Kept separate from the submitted form: the
  // backend re-injects these from live settings at build time, so the
  // preview only needs to read them, never send them.
  const [registrationToken, setRegistrationToken] = useState("");

  const importInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    listGroups().then(setGroups).catch(() => {});
    listSecureBootKeySets().then(setKeySets).catch(() => {});
    getRegistrationToken().then((t) => setRegistrationToken(t.registrationToken)).catch(() => {});
    getExtensionCatalogSettings()
      .then((c) => setConfiguredCatalogs([...c.launch, ...c.saved]))
      .catch(() => {});
  }, []);

  // After focusFirstError queues a focusTarget and setStep has re-rendered
  // the new step, scroll the field into view and focus it. We clear
  // focusTarget after firing so the effect doesn't re-run on unrelated
  // renders (e.g. when the user types in the field we just focused).
  useEffect(() => {
    if (!focusTarget) return;
    const el = fieldRefs.current[focusTarget];
    if (!el) return;
    // Optional call: jsdom, used by the tests, has no scrollIntoView.
    el.scrollIntoView?.({ block: "center", behavior: "smooth" });
    if (typeof (el as HTMLElement).focus === "function") {
      (el as HTMLElement).focus({ preventScroll: true });
    }
    setFocusTarget(null);
  }, [step, focusTarget]);

  // --- Export / Import build config -------------------------------------------------
  // The exported JSON is self-contained and portable: groups and secure-boot
  // key sets are referenced by *name*, not by local database ID, so a config
  // shared between two auroraboot instances can find its referents by name.
  // Raw secrets (manual UKI key/cert paths, advanced cloud-config passwords)
  // are intentionally included as-is because users need them on the target
  // instance too; if you're sharing a config more widely, strip them first.
  function handleExportConfig() {
    const payload = payloadFromBuilder({
      form,
      buildMode,
      groups,
      keySets,
      userMode,
      username,
      sshKeys,
      advancedConfig,
    });
    const filename = downloadBuildConfig(payload);
    toast(`Exported ${filename}`, "success");
  }

  function handleImportClick() {
    importInputRef.current?.click();
  }

  async function handleImportFile(ev: React.ChangeEvent<HTMLInputElement>) {
    const file = ev.target.files?.[0];
    // Reset the input so the same file can be re-selected later.
    if (ev.target) ev.target.value = "";
    if (!file) return;

    let raw: unknown;
    try {
      raw = JSON.parse(await file.text());
    } catch {
      toast("Import failed: not valid JSON", "error");
      return;
    }
    if (typeof raw !== "object" || raw === null) {
      toast(`Import failed: not a ${PRODUCT_NAME} build config`, "error");
      return;
    }
    const r = raw as Record<string, unknown>;
    if (r.kind !== BUILD_CONFIG_KIND) {
      toast(`Import failed: not a ${PRODUCT_NAME} build config`, "error");
      return;
    }
    if (r.version !== BUILD_CONFIG_VERSION) {
      toast(`Import failed: unsupported version ${r.version}`, "error");
      return;
    }
    const parsed = sanitizeImportedBuildConfig(r);

    const src = parsed.source ?? {};
    const prov = parsed.provisioning ?? {};
    const out = parsed.outputs ?? {};
    const sign = parsed.signing ?? {};

    const resolvedGroupId = prov.targetGroupName
      ? groups.find((g) => g.name === prov.targetGroupName)?.id || ""
      : "";
    const resolvedKeySetId = sign.ukiKeySetName
      ? keySets.find((k) => k.name === sign.ukiKeySetName)?.id || ""
      : "";

    // The listed file names describe an upload made in this form; the
    // imported config names its overlay by ID only.
    setOverlayFiles([]);
    setForm({
      ...EMPTY_FORM,
      name: parsed.name || "",
      baseImage: src.baseImage || "",
      kairosVersion: src.kairosVersion || "",
      arch: src.arch || "amd64",
      model: src.model || "generic",
      variant: src.variant || "core",
      kubernetesDistro: src.kubernetesDistro || "",
      kubernetesVersion: src.kubernetesVersion || "",
      kubernetesEnabled: src.kubernetesEnabled ?? true,
      "allow-insecure-registries": src["allow-insecure-registries"] ?? false,
      dockerfile: parsed.dockerfile || "",
      overlayId: parsed.overlayId || "",
      kairosInitImage: src.kairosInitImage || "",
      outputs: withBootModeOutputs({ ...EMPTY_OUTPUTS, ...out }),
      signing: {
        ...EMPTY_SIGNING,
        ukiKeySetId: resolvedKeySetId,
        ukiSecureBootEnroll: sign.ukiSecureBootEnroll || "if-safe",
      },
      provisioning: {
        ...EMPTY_PROVISIONING,
        autoInstall: prov.autoInstall ?? true,
        registerAuroraBoot: prov.registerAuroraBoot ?? true,
        targetGroupId: resolvedGroupId,
        // Legacy exports may omit allowedCommands; fall back to safe defaults.
        allowedCommands: prov.allowedCommands
          ? [...prov.allowedCommands]
          : [...PHONEHOME_SAFE_DEFAULTS],
      },
    });
    setBuildMode(parsed.buildMode === "dockerfile" ? "dockerfile" : "image");
    // Reveal the Image Source / Dockerfile card on the Base step. Without
    // this the card stays hidden (it only shows when a template is picked or
    // a clone is loaded), so the imported baseImage/dockerfile looks like it
    // didn't load even though the form state is correct.
    setSelectedTemplate("Custom");
    setCustomModel(false);
    setUserMode((prov.userMode as UserMode) || "default");
    setUsername(prov.username || "kairos");
    setPassword("kairos");
    setSshKeys(prov.sshKeys || "");
    setAdvancedConfig(parsed.advancedCloudConfig || "");
    setShowAdvanced(Boolean(parsed.advancedCloudConfig));
    setStep("base");

    const warnings: string[] = [];
    if (prov.targetGroupName && !resolvedGroupId) {
      warnings.push(`group "${prov.targetGroupName}" not found`);
    }
    if (sign.ukiKeySetName && !resolvedKeySetId) {
      warnings.push(`key set "${sign.ukiKeySetName}" not found`);
    }
    if (warnings.length > 0) {
      toast(`Imported with warnings: ${warnings.join("; ")}`, "info");
    } else {
      toast("Config imported", "success");
    }
  }

  // Clone pre-fill
  useEffect(() => {
    const cloneId = searchParams.get("clone");
    if (cloneId) {
      getArtifact(cloneId).then((a) => {
        setCloneSource(a.name || a.id.slice(0, 8));

        // Extensions are restored for both branches below: a clone that
        // silently dropped them would rebuild an ISO missing the extensions
        // the operator cloned it for.
        if (a.extensions && a.extensions.length > 0) {
          setSelectedExtensions(parseExtensionSelection(a.extensions));
        }
        if (a.extensionsCatalogs && a.extensionsCatalogs.length > 0) {
          setExtensionsCatalogOverride(a.extensionsCatalogs[0]);
          setExtensionsCatalogTouched(true);
        } else if (a.extensions && a.extensions.length > 0) {
          // A build that sent no catalog was resolved against the default
          // one, whatever its flavor, so the rebuild reads it too.
          setExtensionsCatalogOverride(DEFAULT_EXTENSIONS_CATALOG);
          setExtensionsCatalogTouched(true);
        }

        // The bundle is stored in its own table, so it is not on the artifact
        // response and takes a second read. Applied with a functional update
        // because both branches below replace the whole form synchronously;
        // this callback is a promise continuation, so it always runs after
        // them and merges into the form they built rather than racing it.
        //
        // A bundle that cannot be read must not cost the operator the clone,
        // so the clone goes on. It must not read as an empty bundle either:
        // that is indistinguishable from a source that had none, and the
        // operator would start a build believing nothing was lost.
        listBundleExtensions(cloneId)
          .then((entries) => {
            if (entries.length === 0) return;
            const bundled = entries
              .slice()
              .sort((x, y) => x.order - y.order)
              .map((e) => ({
                name: e.extensionName,
                type: e.extensionType === "confext" ? ("confext" as const) : ("sysext" as const),
                pinnedVersion: e.pinnedVersion || undefined,
                order: e.order,
              }));
            setForm((prev) => ({ ...prev, bundledExtensions: bundled }));
          })
          .catch(() => {
            setCloneBundleUnavailable(true);
            toast(
              "Could not read the bundled extensions of the artifact being cloned. They are not carried over.",
              "error",
            );
          });

        // Hadron branch: restore the composer state and land on Source so the
        // operator can edit firmware / layers / base before rebuilding. Auto-
        // opens the advanced expander when the source artifact carried
        // composition inputs so the user sees the state they cloned from.
        if (a.hadronBase) {
          const HADRON_PREFIX = "ghcr.io/kairos-io/hadron:";
          setSelectedTemplate(HADRON_TEMPLATE_NAME);
          startHadronCatalogs();
          if (a.hadronBase.startsWith(HADRON_PREFIX)) {
            setHadronBaseTag(a.hadronBase.slice(HADRON_PREFIX.length));
            setHadronBaseCustom("");
          } else {
            setHadronBaseTag(HADRON_CUSTOM_TAG_SENTINEL);
            setHadronBaseCustom(a.hadronBase);
          }
          const firmware = a.hadronFirmware || [];
          const layers = a.hadronLayers || [];
          const extra = a.hadronExtra || "";
          setHadronFirmware(firmware);
          setHadronLayers(layers);
          setHadronExtra(extra);
          const hasComposition =
            firmware.length > 0 || layers.length > 0 || extra.trim() !== "";
          setHadronAdvancedOpen(hasComposition);
          setForm({
            ...EMPTY_FORM,
            name: `Copy of ${a.name || a.id.slice(0, 8)}`,
            // Composed clones ship a Dockerfile so baseImage stays blank;
            // peer clones use the plain hadronBase as their base image.
            baseImage: hasComposition ? "" : a.hadronBase,
            kairosVersion: a.kairosVersion,
            model: a.model,
            arch: a.arch || "amd64",
            variant: a.variant || "core",
            kubernetesDistro: a.kubernetesDistro || "",
            kubernetesVersion: a.kubernetesVersion || "",
            kubernetesEnabled: a.variant === "standard" ? a.kubernetesEnabled ?? true : true,
            kairosInitImage: a.kairosInitImage || "",
            overlayId: a.overlayId || "",
            outputs: withBootModeOutputs({
              iso: a.iso,
              cloudImage: a.cloudImage,
              netboot: a.netboot,
              rawDisk: a.rawDisk ?? false,
              tar: a.tar ?? false,
              gce: a.gce ?? false,
              vhd: a.vhd ?? false,
              maas: a.maas ?? false,
              fips: a.fips,
              trustedBoot: a.trustedBoot,
            }),
            signing: { ...EMPTY_SIGNING },
            // Already on the artifact response, so the clone has this in hand.
            // Dropping it rebuilt an image that refuses overlays on the very
            // paths the source artifact was built to accept.
            extensionHierarchies: {
              sysext: a.extensionHierarchies?.sysext ?? [],
              confext: a.extensionHierarchies?.confext ?? [],
            },
            provisioning: {
              autoInstall: a.autoInstall ?? true,
              registerAuroraBoot: a.registerAuroraBoot ?? true,
              targetGroupId: a.targetGroupId || "",
              allowedCommands: [...PHONEHOME_SAFE_DEFAULTS],
            },
          });
          if (a.cloudConfig) {
            setAdvancedConfig(stripPhonehome(a.cloudConfig));
            setShowAdvanced(true);
            setUserMode("none");
          }
          // Land on System so the Hadron composer expander is visible with
          // the cloned state ready to edit. A clone carries a full
          // configuration, so every step can be opened from the stepper.
          setStep("system");
          setMaxReached(BUILDER_STEPS.length - 1);
          return;
        }

        setForm({
          ...EMPTY_FORM,
          name: `Copy of ${a.name || a.id.slice(0, 8)}`,
          baseImage: a.baseImage,
          kairosVersion: a.kairosVersion,
          model: a.model,
          arch: a.arch || "amd64",
          variant: a.variant || "core",
          kubernetesDistro: a.kubernetesDistro || "",
          kubernetesVersion: a.kubernetesVersion || "",
          kubernetesEnabled: a.variant === "standard" ? a.kubernetesEnabled ?? true : true,
          "allow-insecure-registries": a["allow-insecure-registries"] ?? false,
          dockerfile: a.dockerfile || "",
          kairosInitImage: a.kairosInitImage || "",
          overlayId: a.overlayId || "",
          outputs: withBootModeOutputs({
            iso: a.iso,
            cloudImage: a.cloudImage,
            netboot: a.netboot,
            rawDisk: a.rawDisk ?? false,
            tar: a.tar ?? false,
            gce: a.gce ?? false,
            vhd: a.vhd ?? false,
            maas: a.maas ?? false,
            fips: a.fips,
            trustedBoot: a.trustedBoot,
          }),
          signing: { ...EMPTY_SIGNING },
          // Same restore as the Hadron branch above: the hierarchies are on
          // the artifact response and are the clone's to carry over.
          extensionHierarchies: {
            sysext: a.extensionHierarchies?.sysext ?? [],
            confext: a.extensionHierarchies?.confext ?? [],
          },
          provisioning: {
            autoInstall: a.autoInstall ?? true,
            registerAuroraBoot: a.registerAuroraBoot ?? true,
            targetGroupId: a.targetGroupId || "",
            // Cloned artifacts start with the safe default list — the artifact
            // record doesn't persist allowedCommands, only the baked YAML does.
            allowedCommands: [...PHONEHOME_SAFE_DEFAULTS],
          },
        });
        if (a.dockerfile) setBuildMode("dockerfile");
        if (a.cloudConfig) {
          setAdvancedConfig(stripPhonehome(a.cloudConfig));
          setShowAdvanced(true);
          setUserMode("none");
        }
        setStep("review");
        setMaxReached(BUILDER_STEPS.length - 1);
      }).catch(() => {
        // Without this the form silently stays an empty "new artifact" one,
        // which looks like the clone link was never followed.
        toast("Could not read the artifact to clone.", "error");
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams]);

  // uploadOverlay stores the files on the server and attaches the overlay ID
  // it returns to the build.
  async function uploadOverlay(files: File[]) {
    if (files.length === 0) return;
    setOverlayUploading(true);
    try {
      const id = await uploadOverlayFiles(files);
      update("overlayId", id);
      setOverlayFiles(files.map((f) => f.name));
    } catch (err) {
      toast(`Overlay upload failed: ${(err as Error).message}`, "error");
    } finally {
      setOverlayUploading(false);
    }
  }

  function update(field: keyof CreateArtifactInput, value: unknown) {
    setForm((prev) => ({ ...prev, [field]: value }));
  }

  function updateOutput(field: keyof typeof EMPTY_OUTPUTS, value: boolean) {
    setForm((prev) => ({ ...prev, outputs: { ...prev.outputs, [field]: value } }));
  }

  function setTrustedBoot(trustedBoot: boolean) {
    setForm((prev) => ({ ...prev, outputs: withBootModeOutputs({ ...prev.outputs, trustedBoot }) }));
  }

  function updateSigning(field: keyof typeof EMPTY_SIGNING, value: string) {
    setForm((prev) => ({ ...prev, signing: { ...prev.signing, [field]: value } }));
  }

  function updateProvisioning(field: keyof typeof EMPTY_PROVISIONING, value: unknown) {
    setForm((prev) => ({ ...prev, provisioning: { ...prev.provisioning, [field]: value } }));
  }

  // When arch changes, reset model to first available compatible with the new arch.
  function handleArchChange(arch: string) {
    const models = modelsForArch(arch);
    // A bundled extension is built for one architecture, and the handler
    // refuses a build whose bundle does not match the artifact arch. The
    // picks are made on the Extensions step, so going back and switching
    // arch would otherwise carry them forward and the build would be
    // rejected at submit with nothing in the form to explain why. The
    // hierarchies are plain paths and stay.
    const archChanged = arch !== form.arch;
    setForm((prev) =>
      archChanged && (prev.bundledExtensions?.length ?? 0) > 0
        ? { ...prev, arch, bundledExtensions: [] }
        : { ...prev, arch },
    );
    if (!customModel) {
      // Keep current model if still compatible, otherwise fall back to 'generic' / first.
      const stillCompatible = models.some((m) => m.value === form.model);
      if (!stillCompatible && models.length > 0) {
        update("model", models[0].value);
      }
    }
  }

  // Frontend cloud-config preview — must match buildCloudConfig in
  // pkg/handlers/artifacts.go. The backend is the source of truth at build
  // time; this function only powers the Review-step preview.
  const cloudConfigPreview = useMemo(() => {
    const groupName =
      groups.find((g) => g.id === form.provisioning.targetGroupId)?.name || "";
    return buildCloudConfigPreview({
      autoInstall: form.provisioning.autoInstall,
      registerAuroraBoot: form.provisioning.registerAuroraBoot,
      groupName,
      allowedCommands: form.provisioning.allowedCommands ?? [],
      variant: form.variant,
      kubernetesDistro: form.kubernetesDistro || "",
      kubernetesEnabled: form.kubernetesEnabled ?? true,
      userMode,
      username,
      password,
      sshKeys,
      extraYAML: advancedConfig,
      registrationUrl: window.location.origin,
      registrationToken,
    });
  }, [
    groups,
    form.provisioning.targetGroupId,
    form.provisioning.autoInstall,
    form.provisioning.registerAuroraBoot,
    form.provisioning.allowedCommands,
    form.variant,
    form.kubernetesDistro,
    form.kubernetesEnabled,
    userMode,
    username,
    password,
    sshKeys,
    advancedConfig,
    registrationToken,
  ]);

  // The "View full" toggle only makes sense for the content it was opened
  // against, so it is derived from that content: a newly truncated preview
  // never starts out expanded from a stale toggle.
  const showFullCloudConfigPreview = expandedCloudConfigPreview === cloudConfigPreview;

  // computeErrors produces a structured list: each error knows which wizard
  // step it belongs to and which field ref to focus. The top-of-page red
  // banner keeps rendering from the existing `errors: string[]` state,
  // derived from `fieldErrors.map(e => e.message)`.
  //
  // Pass an explicit step to scope to a single step (Next button flow);
  // leave undefined to validate the whole form (final Submit flow).
  function computeErrors(scope?: BuilderStep): FieldError[] {
    const errs: FieldError[] = [];

    // Base
    if (scope === undefined || scope === "base") {
      if (!form.name?.trim()) {
        errs.push({ field: "name", step: "base", message: "Name is required." });
      }
      if (buildMode === "image" && !form.baseImage.trim()) {
        errs.push({ field: "baseImage", step: "base", message: "Base image is required." });
      }
      if (buildMode === "dockerfile" && !form.dockerfile?.trim()) {
        errs.push({ field: "dockerfile", step: "base", message: "Dockerfile is required." });
      }
    }

    // System
    // Artifact version is no longer required: if the user leaves it blank
    // we stub DEFAULT_ARTIFACT_VERSION at submit time so a brand-new
    // build still has a usable version for upgrade tracking.
    if (scope === undefined || scope === "system") {
      if (form.variant === "standard" && !form.kubernetesDistro?.trim()) {
        errs.push({
          field: "kubernetesDistro",
          step: "system",
          message: "Kubernetes distro is required for standard variant.",
        });
      }
    }

    // Output
    if (scope === undefined || scope === "output") {
      // FIPS and Trusted Boot live in outputs too, but they shape the image
      // rather than add a format, so only the output cards count.
      const hasOutput = OUTPUT_GROUPS.some((g) => g.items.some((i) => form.outputs[i.field]));
      if (!hasOutput) {
        errs.push({
          field: "outputs",
          step: "output",
          message: "At least one output format must be selected.",
        });
      }
      if (form.outputs.trustedBoot && form.outputs.iso && !form.signing.ukiKeySetId) {
        errs.push({
          field: "ukiKeySetId",
          step: "output",
          message: "A secure boot key set must be selected for a Trusted Boot ISO.",
        });
      }
    }

    return errs;
  }

  // Per-step validation used by the Next button. Returns true when the
  // current step is clean; side-effects: sets the error banner and queues
  // a focus/scroll to the first invalid field on the same step.
  function validateStep(s: BuilderStep): boolean {
    const errs = computeErrors(s);
    setShownErrorSteps((prev) => (prev === "all" || prev.has(s) ? prev : new Set(prev).add(s)));
    if (errs.length > 0) focusFirstError(errs);
    return errs.length === 0;
  }

  // Two-phase focus: set the wizard step, then let the effect below run
  // once React has mounted the new step's DOM, and finally focus the first
  // invalid field. The detour through `focusTarget` state is required
  // because the target node doesn't exist yet in the same tick we change
  // the step.
  function focusFirstError(errs: FieldError[]) {
    if (errs.length === 0) return;
    const first = errs[0];
    setStep(first.step);
    setFocusTarget(first.field);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    // Only allow submission from the Review step (prevents accidental Enter-key submits
    // from inputs or Radix component keyboard handlers on earlier steps).
    if (step !== "review") return;
    setSubmitError(null);
    const fieldErrors = computeErrors();
    if (fieldErrors.length > 0) {
      setShownErrorSteps("all");
      focusFirstError(fieldErrors);
      return;
    }

    const input: CreateArtifactInput = {
      name: form.name || undefined,
      baseImage: form.baseImage,
      // Belt and braces: if the user cleared the Version field after we
      // prefilled it, still send the default so the backend never gets a
      // blank version.
      kairosVersion: form.kairosVersion.trim() || DEFAULT_ARTIFACT_VERSION,
      model: form.model,
      arch: form.arch,
      variant: form.variant,
      kubernetesDistro: form.variant === "standard" ? form.kubernetesDistro : undefined,
      kubernetesVersion: form.variant === "standard" ? form.kubernetesVersion : undefined,
      kubernetesEnabled: form.variant === "standard" ? form.kubernetesEnabled ?? true : undefined,
      // Only meaningful when pulling a base image from a registry.
      "allow-insecure-registries":
        buildMode === "image" ? form["allow-insecure-registries"] : undefined,
      dockerfile: buildMode === "dockerfile" ? form.dockerfile : undefined,
      hadronBase:
        selectedTemplate === HADRON_TEMPLATE_NAME
          ? hadronBase || undefined
          : undefined,
      hadronFirmware:
        selectedTemplate === HADRON_TEMPLATE_NAME ? hadronFirmware : undefined,
      hadronLayers:
        selectedTemplate === HADRON_TEMPLATE_NAME ? hadronLayers : undefined,
      hadronExtra:
        selectedTemplate === HADRON_TEMPLATE_NAME
          ? hadronExtra || undefined
          : undefined,
      extensions:
        selectedExtensionNames.length > 0 && !catalogMissing
          ? selectedExtensionNames
          : undefined,
      // Left out only for a Hadron build on the default catalog: that keeps
      // the server's default as the single source of that URL, so it can
      // move without every stored build disagreeing. Any other flavor states
      // its catalog, because the server default is the Hadron one.
      extensionsCatalogs:
        selectedExtensionNames.length > 0 &&
        !catalogMissing &&
        (!hadronBuild || extensionsCatalog.trim() !== DEFAULT_EXTENSIONS_CATALOG)
          ? [extensionsCatalog.trim()]
          : undefined,
      overlayId: form.overlayId || undefined,
      kairosInitImage: form.kairosInitImage || undefined,
      outputs: { ...form.outputs },
      signing: { ...form.signing },
      provisioning: {
        ...form.provisioning,
        userMode,
        username: userMode === "custom" ? username : undefined,
        password: userMode === "custom" ? password : undefined,
        sshKeys: userMode !== "none" && sshKeys.trim() ? sshKeys : undefined,
      },
      // Send only the user's extra YAML — backend builds the canonical document.
      cloudConfig: advancedConfig.trim() || undefined,
      // Both of these are collected on the Extensions step and both are
      // validated and stored by the handler, so leaving them out of this
      // literal discarded them at submit with no error anywhere: the picks
      // simply were not on the artifact afterwards. They are built field by
      // field rather than spread from `form`, so a field the form gains is
      // silently dropped until it is named here.
      extensionHierarchies:
        (form.extensionHierarchies?.sysext?.length ?? 0) > 0 ||
        (form.extensionHierarchies?.confext?.length ?? 0) > 0
          ? {
              sysext: form.extensionHierarchies?.sysext ?? [],
              confext: form.extensionHierarchies?.confext ?? [],
            }
          : undefined,
      bundledExtensions:
        (form.bundledExtensions?.length ?? 0) > 0
          ? form.bundledExtensions
          : undefined,
    };

    // createArtifact throws on any non-2xx. Without this catch the rejection
    // escaped the form's onSubmit handler: the page stayed on the Review step
    // with nothing rendered, so a refused build looked exactly like a click
    // that did not register. Every message the create handler writes for the
    // user reaches them through here, so show it and leave the operator on
    // Review with the button still enabled to correct and try again.
    try {
      const result = await createArtifact(input);
      navigate(`/artifacts/${result.id}`);
    } catch (err) {
      setSubmitError(
        `Build was not started: ${err instanceof Error ? err.message : String(err)}`,
      );
    }
  }

  // selectTemplate prefills the form from a template. It does not change the
  // step: the user reviews the Base step and moves on with Next.
  function selectTemplate(t: BuildTemplate) {
    setSelectedTemplate(t.name);
    if (t.name === HADRON_TEMPLATE_NAME) startHadronCatalogs();
    setForm((prev) => ({
      ...EMPTY_FORM,
      ...t.values,
      name: prev.name, // preserve user-typed name
      outputs: { ...EMPTY_OUTPUTS, ...t.values.outputs },
      signing: { ...EMPTY_SIGNING, ...t.values.signing },
      provisioning: { ...EMPTY_PROVISIONING, ...t.values.provisioning },
    }));
    setCustomModel(false);
  }

  // handleNext validates the current step only, then moves on. Leaving the
  // System step of a Hadron build also turns the composer inputs into the
  // build source.
  function handleNext() {
    if (!validateStep(step)) return;
    if (step === "system" && selectedTemplate === HADRON_TEMPLATE_NAME) {
      const composed =
        hadronFirmware.length > 0 ||
        hadronLayers.length > 0 ||
        hadronExtra.trim() !== "";
      if (composed) {
        const dockerfile = renderHadronMiddleContent(
          hadronFirmware,
          hadronLayers,
          hadronExtra,
        );
        setForm((prev) => ({ ...prev, dockerfile, baseImage: "" }));
        setBuildMode("dockerfile");
      } else {
        // Round-trip case: an earlier composed pass may have blanked
        // baseImage and populated dockerfile. Restore the plain path
        // so a user who removed all firmware/layers/extra still
        // gets a valid image-mode build.
        setForm((prev) => ({ ...prev, dockerfile: "", baseImage: hadronBase }));
        setBuildMode("image");
      }
    }
    goToStep(BUILDER_STEPS[BUILDER_STEPS.indexOf(step) + 1]);
  }

  // Errors are recomputed from the current state on every render.
  const liveErrors = computeErrors();
  const errors = liveErrors
    .filter((e) => shownErrorSteps === "all" || shownErrorSteps.has(e.step))
    .map((e) => e.message);
  // The rejected build goes in the same banner as the field errors, last,
  // because it is the newest thing the operator did.
  if (submitError) errors.push(submitError);
  const stepIndex = BUILDER_STEPS.indexOf(step);
  const currentStepErrors = liveErrors.filter((e) => e.step === step);
  const stepHasErrors = (key: BuilderStep) => liveErrors.some((e) => e.step === key);
  // Steps before the current one are done (or in error). A later step can be
  // opened again once it was reached and every step before it is valid.
  const wizardSteps: WizardStep[] = BUILDER_STEPS.map((key, i) => {
    let state: WizardStep["state"];
    if (i === stepIndex) {
      state = "current";
    } else if (i < stepIndex) {
      state = stepHasErrors(key) ? "error" : "done";
    } else if (i <= maxReached && !BUILDER_STEPS.slice(0, i).some(stepHasErrors)) {
      state = stepHasErrors(key) ? "error" : "done";
    } else {
      state = "todo";
    }
    return { key, label: STEP_LABELS[key], state };
  });

  const availableModels = modelsForArch(form.arch);

  // Count only actual output formats (not security modifiers)
  // The output cards of the selected boot mode. Trusted Boot lists only what
  // its image ships as, and its ISO is the signed UKI installer, named as the
  // artifact page names it.
  const outputGroups = form.outputs.trustedBoot
    ? OUTPUT_GROUPS.map((g) => ({
        ...g,
        items: g.items
          .filter((i) => TRUSTED_BOOT_OUTPUTS.includes(i.field))
          .map((i) => (i.field === "iso" ? { ...i, label: "UKI ISO", desc: "Signed installer image" } : i)),
      })).filter((g) => g.items.length > 0)
    : OUTPUT_GROUPS;

  const selectedOutputCount = outputGroups.flatMap((g) => g.items).filter(
    (i) => form.outputs[i.field],
  ).length;

  // Flattened list of currently-picked outputs (with their tone) for the
  // ambient chip row under the step indicator. Rebuilt each render from
  // the same data the Output step uses, so the two views can't drift.
  const selectedOutputItems = outputGroups.flatMap((g) =>
    g.items
      .filter((i) => form.outputs[i.field])
      .map((i) => ({ ...i, tone: g.tone })),
  );

  // The summary aside and the Review step read the same builder state.
  const summaryData: BuildSummaryData = {
    name: form.name ?? "",
    base: buildMode === "dockerfile" ? "Dockerfile" : form.baseImage,
    arch: form.arch,
    model: form.model,
    variant: form.variant,
    kubernetes:
      form.variant === "standard" && (form.kubernetesEnabled ?? true)
        ? [form.kubernetesDistro, form.kubernetesVersion].filter(Boolean).join(" ") || undefined
        : undefined,
    version: form.kairosVersion || DEFAULT_ARTIFACT_VERSION,
    bundledExtensions: (form.bundledExtensions ?? []).map((e) => e.name),
    // Review is the last screen before Start build, so an unread bundle has to
    // say so here rather than show the "None" of a source that had no bundle.
    bundledExtensionsUnavailable: cloneBundleUnavailable,
    catalogExtensions: catalogMissing ? [] : selectedExtensionNames,
    user: userMode,
    sshKeyCount: userMode === "none" ? 0 : sshKeys.split("\n").filter((l) => l.trim()).length,
    register: form.provisioning.registerAuroraBoot,
    targetGroup: form.provisioning.targetGroupId
      ? groups.find((g) => g.id === form.provisioning.targetGroupId)?.name || form.provisioning.targetGroupId
      : undefined,
    commands: form.provisioning.registerAuroraBoot ? (form.provisioning.allowedCommands ?? []) : [],
    fips: form.outputs.fips,
    trustedBoot: form.outputs.trustedBoot,
    // One ISO option, and the boot mode says which ISO it is. Name the one
    // this build produces rather than leave it to the Trusted Boot row.
    outputs: selectedOutputItems.map((i) =>
      i.field === "iso" && form.outputs.trustedBoot ? "UKI ISO" : i.label,
    ),
    overlayFiles: overlayFiles.length,
    overlayAttached: !!form.overlayId,
    autoInstall: form.provisioning.autoInstall,
    // Like the build request, the flag only applies to an image build.
    insecureRegistries: buildMode === "image" && !!form["allow-insecure-registries"],
  };
  // Edit links follow the stepper's rules: a step not reached yet stays closed.
  const editStep = (key: string) => {
    if (wizardSteps.find((w) => w.key === key)?.state !== "todo") goToStep(key as BuilderStep);
  };

  return (
    <div>
      <PageHeader
        title={cloneSource ? `Clone: ${cloneSource}` : "Build Artifact"}
        description={cloneSource ? "Modify and rebuild from an existing artifact" : "Configure and build a new OS artifact"}
      >
        <input
          ref={importInputRef}
          type="file"
          accept="application/json,.json"
          className="hidden"
          onChange={handleImportFile}
        />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button type="button" variant="outline" size="icon" aria-label="More actions">
              <MoreHorizontal aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={handleImportClick}>
              <FileUp aria-hidden="true" />
              Import config
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={handleExportConfig}>
              <Download aria-hidden="true" />
              Export config
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </PageHeader>
      {/* Ambient preview of the outputs the user has selected. Hidden when
          no outputs are picked so the row doesn't render a dangling label. */}
      {selectedOutputItems.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 mb-6">
          <span className="text-xs text-muted-foreground shrink-0">Outputs:</span>
          {selectedOutputItems.map((item) => {
            const Icon = item.icon;
            return (
              <span
                key={item.field}
                className={`inline-flex items-center gap-1 text-[11px] font-medium px-2 py-0.5 rounded-full border ${OUTPUT_TONE_CLASSES[item.tone]}`}
              >
                <Icon className="h-3 w-3" />
                {item.label}
              </span>
            );
          })}
        </div>
      )}


      <WizardShell
        steps={wizardSteps}
        current={step}
        onStepChange={(key) => goToStep(key as BuilderStep)}
        aside={step === "review" ? undefined : <BuildSummary data={summaryData} variant="aside" onEdit={editStep} />}
        footer={{
          onBack: stepIndex > 0 ? () => goToStep(BUILDER_STEPS[stepIndex - 1]) : () => navigate("/artifacts"),
          backLabel: stepIndex > 0 ? "Back" : "Cancel",
          status:
            currentStepErrors.length === 0
              ? { tone: "success", text: "All required fields set" }
              : {
                  tone: "warning",
                  text: `${currentStepErrors.length} issue${currentStepErrors.length === 1 ? "" : "s"} on this step`,
                },
          primary:
            step === "review"
              ? {
                  label: "Start build",
                  onClick: () => {
                    void handleSubmit({ preventDefault: () => {} } as FormEvent);
                  },
                }
              : { label: `Next: ${STEP_LABELS[BUILDER_STEPS[stepIndex + 1]]}`, onClick: handleNext },
        }}
      >
      {/* Validation errors */}
      {errors.length > 0 && (
        <div className="mb-6 rounded-md bg-red-500/10 border border-red-500/25 p-4">
          <ul className="list-disc list-inside text-sm text-red-700 space-y-1">
            {errors.map((err, i) => (
              <li key={i}>{err}</li>
            ))}
          </ul>
        </div>
      )}

      <form onSubmit={handleSubmit}>
        {/* Step 1: Base */}
        {step === "base" && (
          <div>
            <div className="mb-6 max-w-md">
              <Label className="mb-2 block text-sm font-medium">
                Name
                <InfoTooltip>
                  Friendly identifier shown throughout {PRODUCT_NAME}. Not used in the generated artifact filenames.
                </InfoTooltip>
              </Label>
              <Input
                ref={bindRef("name")}
                placeholder="e.g. Production v4.0.3 + custom agent"
                value={form.name || ""}
                onChange={(e) => update("name", e.target.value)}
              />
              <p className="text-xs text-muted-foreground mt-1">
                A friendly name to identify this artifact.
              </p>
            </div>

            {!cloneSource && (
              <div className="mb-6">
                <Label className="mb-2 block text-sm font-medium">Start from a template</Label>
                <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                  {TEMPLATES.map((t) => (
                    <Card
                      key={t.name}
                      className={`cursor-pointer transition-colors ${
                        selectedTemplate === t.name
                          ? "border-primary bg-primary-soft ring-1 ring-primary/20"
                          : "hover:border-primary/40"
                      }`}
                      role="button"
                      tabIndex={0}
                      aria-pressed={selectedTemplate === t.name}
                      onClick={() => selectTemplate(t)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          selectTemplate(t);
                        }
                      }}
                    >
                      <CardContent className="p-3">
                        <p className="font-medium text-sm">{t.name}</p>
                        <p className="text-xs text-muted-foreground mt-1">{t.description}</p>
                      </CardContent>
                    </Card>
                  ))}
                </div>
              </div>
            )}


            {(selectedTemplate === "Custom" ||
              (cloneSource && selectedTemplate !== HADRON_TEMPLATE_NAME)) && (
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">Image Source</CardTitle>
              </CardHeader>
              <CardContent className="grid gap-4">
                <div className="flex gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant={buildMode === "image" ? "default" : "outline"}
                    onClick={() => { setBuildMode("image"); update("dockerfile", ""); }}
                  >
                    Base Image
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant={buildMode === "dockerfile" ? "default" : "outline"}
                    onClick={() => { setBuildMode("dockerfile"); }}
                  >
                    Dockerfile
                  </Button>
                </div>

                {buildMode === "image" ? (
                  <>
                  <div className="grid gap-2">
                    <Label>
                      Base Image
                      <InfoTooltip>
                        Plain images (e.g. ubuntu:24.04) are auto-kairosified. Pre-built Kairos images are used directly.
                      </InfoTooltip>
                    </Label>
                    <Input
                      ref={bindRef("baseImage")}
                      placeholder="e.g. quay.io/kairos/hadron:v0.0.4-core-amd64-generic-v4.0.3"
                      value={form.baseImage}
                      onChange={(e) => update("baseImage", e.target.value)}
                    />
                  </div>
                  <div>
                    <label className="flex items-center gap-2 text-sm font-medium">
                      <input
                        type="checkbox"
                        checked={form["allow-insecure-registries"] ?? false}
                        onChange={(e) => update("allow-insecure-registries", e.target.checked)}
                        className="rounded border-input"
                      />
                      Allow insecure registries
                    </label>
                    <p className="text-xs text-muted-foreground mt-1 ml-6">
                      Allow pulling the base image from a registry served over plain HTTP or with an untrusted/self-signed TLS certificate.
                    </p>
                  </div>
                  </>
                ) : (
                  <div className="grid gap-2">
                    <Label>
                      Dockerfile
                      <InfoTooltip>
                        Build from a Dockerfile instead of a base image. Multi-stage is allowed; the final stage must produce a Kairos-derivable rootfs.{" "}
                        <a
                          href="https://kairos.io/docs/reference/build-from-scratch/"
                          target="_blank"
                          rel="noopener noreferrer"
                          className="underline"
                        >
                          Docs
                        </a>
                      </InfoTooltip>
                    </Label>
                    <Textarea
                      ref={bindRef("dockerfile")}
                      placeholder={"FROM golang:1.26-alpine AS builder\nRUN ...\n\nFROM quay.io/kairos/hadron:v0.0.4-core-amd64-generic-v4.0.3\nCOPY --from=builder /app /usr/sbin/app"}
                      value={form.dockerfile || ""}
                      onChange={(e) => update("dockerfile", e.target.value)}
                      rows={8}
                      className="font-mono text-sm"
                    />
                  </div>
                )}
              </CardContent>
            </Card>
            )}
          </div>
        )}

        {/* Step 2: System */}
        {step === "system" && (
          <div className="grid gap-6">
            {/* Architecture */}
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">
                  Architecture
                  <InfoTooltip>
                    CPU architecture of the target hardware. Determines which base rootfs is pulled and which systemd-boot stub is used for UKI builds.
                  </InfoTooltip>
                </CardTitle>
              </CardHeader>
              <CardContent>
                <SegmentedControl
                  ariaLabel="Architecture"
                  value={form.arch}
                  onChange={handleArchChange}
                  options={ARCHES.map((a) => ({ value: a.value, label: a.label, hint: a.desc }))}
                />
              </CardContent>
            </Card>

            {/* Model */}
            <Card>
              <CardHeader>
                <CardTitle className="text-sm flex items-center justify-between">
                  <span>
                    Model
                    <InfoTooltip>
                      Hardware model determines board-specific drivers and firmware bundled into the image. Only models compatible with the selected architecture are shown.
                    </InfoTooltip>
                  </span>
                  <button
                    type="button"
                    className="text-xs font-normal text-muted-foreground hover:text-foreground underline-offset-4 hover:underline"
                    onClick={() => {
                      if (customModel) {
                        setCustomModel(false);
                        const models = modelsForArch(form.arch);
                        if (models.length > 0) update("model", models[0].value);
                      } else {
                        setCustomModel(true);
                        update("model", "");
                      }
                    }}
                  >
                    {customModel ? "Use presets" : "Custom\u2026"}
                  </button>
                </CardTitle>
              </CardHeader>
              <CardContent>
                {customModel ? (
                  <Input
                    placeholder="e.g. generic"
                    value={form.model}
                    onChange={(e) => update("model", e.target.value)}
                  />
                ) : (
                  <div className="grid gap-3 md:grid-cols-2">
                    {availableModels.map((m) => {
                      const Icon = m.icon;
                      const selected = form.model === m.value;
                      return (
                        <button
                          key={m.value}
                          type="button"
                          onClick={() => update("model", m.value)}
                          className={`text-left rounded-lg border p-4 transition-colors ${
                            selected
                              ? "border-primary bg-primary-soft ring-1 ring-primary"
                              : "border-border hover:border-primary/50 hover:bg-muted/40"
                          }`}
                        >
                          <div className="flex items-start gap-3">
                            <Icon className={`h-5 w-5 mt-0.5 ${selected ? "text-primary" : "text-muted-foreground"}`} />
                            <div className="flex-1">
                              <div className="flex items-center gap-2">
                                <span className="font-medium">{m.label}</span>
                                {selected && <Check className="h-4 w-4 text-primary" />}
                              </div>
                              <p className="text-xs text-muted-foreground mt-1">{m.desc}</p>
                            </div>
                          </div>
                        </button>
                      );
                    })}
                  </div>
                )}
              </CardContent>
            </Card>

            {/* Variant */}
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">
                  Variant
                  <InfoTooltip>
                    Core is a minimal OS with only cloud-init. Standard bundles a Kubernetes distribution (K3s or K0s) into the image.
                  </InfoTooltip>
                </CardTitle>
              </CardHeader>
              <CardContent>
                <SegmentedControl
                  ariaLabel="Variant"
                  value={form.variant}
                  onChange={(value) => {
                    if (value === "standard") {
                      setForm((prev) => ({
                        ...prev,
                        variant: "standard",
                        kubernetesDistro: prev.kubernetesDistro || "k3s",
                        kubernetesEnabled: prev.kubernetesEnabled ?? true,
                      }));
                    } else {
                      setForm((prev) => ({
                        ...prev,
                        variant: "core",
                        kubernetesDistro: "",
                        kubernetesVersion: "",
                        kubernetesEnabled: true,
                      }));
                    }
                  }}
                  options={VARIANTS.map((v) => ({ value: v.value, label: v.label, hint: v.desc }))}
                />
              </CardContent>
            </Card>

            {/* Kubernetes fields (only when variant=standard) */}
            {form.variant === "standard" && (
              <Card>
                <CardHeader>
                  <CardTitle className="text-sm">Kubernetes</CardTitle>
                </CardHeader>
                <CardContent className="grid gap-4">
                  <label className="flex items-center gap-2 text-sm font-medium">
                    <input
                      type="checkbox"
                      checked={form.kubernetesEnabled ?? true}
                      onChange={(e) => update("kubernetesEnabled", e.target.checked)}
                      className="rounded border-input"
                    />
                    Enable Kubernetes on first boot
                    <InfoTooltip>
                      Baked into cloud-config as <code className="font-mono">k3s.enabled</code> or{" "}
                      <code className="font-mono">k0s.enabled</code>. The distribution is still bundled
                      into the image for offline installs; disable this to defer starting the cluster.
                    </InfoTooltip>
                  </label>
                  <div className="grid gap-2">
                    <Label>
                      Distribution
                      <InfoTooltip>
                        k3s or k0s, bundled into the image for offline installs.{" "}
                        <a
                          href="https://kairos.io/docs/reference/image_matrix/"
                          target="_blank"
                          rel="noopener noreferrer"
                          className="underline"
                        >
                          Docs
                        </a>
                      </InfoTooltip>
                    </Label>
                    <div className="flex gap-2">
                      {(["k3s", "k0s"] as const).map((d, idx) => (
                        <Button
                          key={d}
                          ref={idx === 0 ? bindRef("kubernetesDistro") : undefined}
                          type="button"
                          size="sm"
                          variant={form.kubernetesDistro === d ? "default" : "outline"}
                          onClick={() => update("kubernetesDistro", d)}
                        >
                          {d.toUpperCase()}
                        </Button>
                      ))}
                    </div>
                  </div>
                  <div className="grid gap-2">
                    <Label>
                      Version (optional)
                      <InfoTooltip>
                        Pinned Kubernetes version. Leave empty to take whatever the chosen distro defaults to.
                        Must be the full provider release tag, e.g.{" "}
                        <code>
                          {form.kubernetesDistro === "k0s"
                            ? "v1.36.1+k0s.0"
                            : "v1.36.1+k3s1"}
                        </code>
                        .
                      </InfoTooltip>
                    </Label>
                    <div className="flex gap-2">
                      <Input
                        className="flex-1"
                        placeholder={
                          form.kubernetesDistro === "k0s"
                            ? "e.g. v1.36.1+k0s.0"
                            : "e.g. v1.36.1+k3s1"
                        }
                        value={form.kubernetesVersion || ""}
                        onChange={(e) => update("kubernetesVersion", e.target.value)}
                      />
                      {(form.kubernetesDistro === "k3s" ||
                        form.kubernetesDistro === "k0s") && (
                        <KubernetesReleasePicker
                          key={form.kubernetesDistro}
                          distro={form.kubernetesDistro}
                          onSelect={(tag) => update("kubernetesVersion", tag)}
                        />
                      )}
                    </div>
                  </div>
                </CardContent>
              </Card>
            )}

            {/* Version */}
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">Version</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="grid gap-2">
                  <Label>
                    Artifact version
                    <InfoTooltip>
                      The version <em>you</em> are releasing with this build. It gets
                      baked into the image and is how {PRODUCT_NAME} decides whether a node is
                      up-to-date or needs an upgrade.
                      <br />
                      <br />
                      Defaults to <code>v1.0</code> if you leave it blank. Bump it every
                      time you ship a new build so your fleet can pick up the change.{" "}
                      <a
                        href="https://kairos.io/docs/upgrade/"
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        Upgrade docs
                      </a>
                    </InfoTooltip>
                  </Label>
                  <Input
                    ref={bindRef("kairosVersion")}
                    placeholder="v1.0"
                    value={form.kairosVersion}
                    onChange={(e) => update("kairosVersion", e.target.value)}
                  />
                  <p className="text-xs text-muted-foreground">
                    Used for upgrade tracking across your fleet. Leave blank to use{" "}
                    <code className="font-mono">v1.0</code>.
                  </p>
                </div>
              </CardContent>
            </Card>

            {selectedTemplate === HADRON_TEMPLATE_NAME && (
              <Card>
                <button
                  type="button"
                  className="w-full flex items-center justify-between px-6 py-4 text-sm font-medium hover:bg-muted/30 transition-colors rounded-t-lg"
                  onClick={() => setHadronAdvancedOpen(!hadronAdvancedOpen)}
                >
                  <span className="flex items-center gap-2">
                    {hadronAdvancedOpen ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                    Advanced: firmware, software layers, and version
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {(() => {
                      const parts: string[] = [];
                      if (hadronFirmware.length > 0) parts.push(`${hadronFirmware.length} firmware`);
                      if (hadronLayers.length > 0) parts.push(`${hadronLayers.length} layers`);
                      if (hadronExtra.trim() !== "") parts.push("extra Dockerfile");
                      return parts.length > 0 ? parts.join(", ") : hadronBase;
                    })()}
                  </span>
                </button>
                {hadronAdvancedOpen && (
                  <CardContent className="grid gap-4 pt-0">
                  <p className="text-xs text-muted-foreground">
                    Optional. Firmware and software layers are COPY'd onto the
                    Hadron base, producing a Dockerfile-driven build instead of
                    the plain image build. Same outputs (ISO, UKI, raw disk, ...)
                    either way.
                  </p>

                  <div className="grid gap-2">
                    <Label className="text-xs">
                      Base image
                      <InfoTooltip>
                        Pick an official Hadron release or switch to Custom…
                        to point at a mirror, digest-pin, or private build.
                      </InfoTooltip>
                    </Label>
                    <Select value={hadronBaseTag} onValueChange={updateHadronBaseTag}>
                      <SelectTrigger>
                        <SelectValue placeholder="Select Hadron version" />
                      </SelectTrigger>
                      <SelectContent>
                        {hadronBaseTags.map((t) => (
                          <SelectItem key={t} value={t}>
                            {t === "main" ? "main (tip of tree)" : t}
                          </SelectItem>
                        ))}
                        <SelectItem value={HADRON_CUSTOM_TAG_SENTINEL}>
                          Custom…
                        </SelectItem>
                      </SelectContent>
                    </Select>
                    {hadronBaseTag === HADRON_CUSTOM_TAG_SENTINEL && (
                      <Input
                        className="font-mono text-xs"
                        placeholder={DEFAULT_HADRON_BASE}
                        value={hadronBaseCustom}
                        onChange={(e) => updateHadronBaseCustom(e.target.value)}
                      />
                    )}
                    {hadronBase ? (
                      <p className="text-xs text-muted-foreground font-mono break-all">
                        {hadronBase}
                      </p>
                    ) : (
                      <p className="text-xs text-red-600 font-medium">
                        Pick a Hadron base image or type a custom reference.
                      </p>
                    )}
                  </div>

                  <div className="grid gap-2">
                    <Label className="text-xs">
                      Firmware images
                      <InfoTooltip>
                        Each firmware image is COPY'd verbatim into the base.
                        Pick only what your hardware needs — the full
                        linux-firmware tree is large.
                      </InfoTooltip>
                    </Label>
                    {hadronFirmware.length > 0 && (
                      <div className="flex flex-wrap gap-1.5">
                        {hadronFirmware.map((ref) => (
                          <span
                            key={ref}
                            className="inline-flex items-center gap-1 text-[11px] font-mono bg-secondary px-2 py-0.5 rounded"
                          >
                            {ref}
                            <button
                              type="button"
                              className="text-muted-foreground hover:text-foreground"
                              onClick={() =>
                                setHadronFirmware(hadronFirmware.filter((r) => r !== ref))
                              }
                            >
                              ×
                            </button>
                          </span>
                        ))}
                      </div>
                    )}
                    {firmwareCatalogState === "loading" && (
                      <p className="text-xs italic text-muted-foreground flex items-center gap-2">
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        Loading firmware catalog from upstream…
                      </p>
                    )}
                    {firmwareCatalogState === "error" && (
                      <p className="text-xs italic text-muted-foreground">
                        Firmware catalog unavailable — add refs manually below.
                      </p>
                    )}
                    {firmwareCatalogState === "ready" && firmwareCatalog.length > 0 && (
                      <>
                        <Input
                          placeholder="Filter firmware… (e.g. amdgpu, iwlwifi)"
                          value={firmwareQuery}
                          onChange={(e) => setFirmwareQuery(e.target.value)}
                          className="text-xs"
                        />
                        {(() => {
                          const q = firmwareQuery.trim().toLowerCase();
                          const visible = q
                            ? firmwareCatalog.filter((f) => f.name.toLowerCase().includes(q))
                            : firmwareCatalog;
                          return (
                            <div className="max-h-56 overflow-y-auto border rounded-md p-2 space-y-1">
                              {visible.length === 0 ? (
                                <p className="text-xs italic text-muted-foreground text-center py-2">
                                  No firmware matches "{firmwareQuery}"
                                </p>
                              ) : (
                                visible.map((f) => {
                                  const ref = `${f.image}:${f.version}`;
                                  const selected = hadronFirmware.includes(ref);
                                  return (
                                    <label
                                      key={ref}
                                      className={`flex items-center gap-2 text-xs cursor-pointer px-2 py-1 rounded hover:bg-muted/60 ${selected ? "bg-primary/10" : ""}`}
                                    >
                                      <input
                                        type="checkbox"
                                        checked={selected}
                                        onChange={() =>
                                          setHadronFirmware(
                                            selected
                                              ? hadronFirmware.filter((r) => r !== ref)
                                              : [...hadronFirmware, ref],
                                          )
                                        }
                                      />
                                      <span className="font-mono flex-1 truncate">{f.name}</span>
                                      <span className="text-muted-foreground text-[10px]">{f.version}</span>
                                    </label>
                                  );
                                })
                              )}
                            </div>
                          );
                        })()}
                      </>
                    )}
                    <div className="flex gap-2">
                      <Input
                        className="font-mono text-xs"
                        placeholder="Custom firmware ref e.g. ghcr.io/kairos-io/hadron-firmware/linux-firmware-amdgpu:20260622"
                        value={firmwareDraft}
                        onChange={(e) => setFirmwareDraft(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key !== "Enter") return;
                          e.preventDefault();
                          const ref = firmwareDraft.trim();
                          if (!ref || hadronFirmware.includes(ref)) return;
                          setHadronFirmware([...hadronFirmware, ref]);
                          setFirmwareDraft("");
                        }}
                      />
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          const ref = firmwareDraft.trim();
                          if (!ref || hadronFirmware.includes(ref)) return;
                          setHadronFirmware([...hadronFirmware, ref]);
                          setFirmwareDraft("");
                        }}
                      >
                        Add
                      </Button>
                    </div>
                  </div>

                  <div className="grid gap-2">
                    <Label className="text-xs">
                      Software layers
                      <InfoTooltip>
                        Applied in order — later layers overwrite files from
                        earlier ones. Use the arrows to reorder.
                      </InfoTooltip>
                    </Label>
                    {hadronLayers.length > 0 && (
                      <ol className="border rounded-md p-2 space-y-1">
                        {hadronLayers.map((ref, idx) => (
                          <li key={ref} className="flex items-center gap-2 text-xs">
                            <span className="w-6 text-center text-muted-foreground font-mono">
                              {idx + 1}
                            </span>
                            <span className="font-mono flex-1 truncate">{ref}</span>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon"
                              className="h-6 w-6"
                              disabled={idx === 0}
                              onClick={() => {
                                const next = hadronLayers.slice();
                                [next[idx - 1], next[idx]] = [next[idx], next[idx - 1]];
                                setHadronLayers(next);
                              }}
                            >
                              ↑
                            </Button>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon"
                              className="h-6 w-6"
                              disabled={idx === hadronLayers.length - 1}
                              onClick={() => {
                                const next = hadronLayers.slice();
                                [next[idx + 1], next[idx]] = [next[idx], next[idx + 1]];
                                setHadronLayers(next);
                              }}
                            >
                              ↓
                            </Button>
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon"
                              className="h-6 w-6"
                              onClick={() =>
                                setHadronLayers(hadronLayers.filter((_, i) => i !== idx))
                              }
                            >
                              <X className="h-3.5 w-3.5" />
                            </Button>
                          </li>
                        ))}
                      </ol>
                    )}
                    {layersCatalogState === "loading" && (
                      <p className="text-xs italic text-muted-foreground flex items-center gap-2">
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        Loading layers catalog from upstream…
                      </p>
                    )}
                    {layersCatalogState === "error" && (
                      <p className="text-xs italic text-muted-foreground">
                        Layers catalog unavailable — add refs manually below.
                      </p>
                    )}
                    {layersCatalogState === "ready" && layersCatalog.length > 0 && (
                      <>
                        <Input
                          placeholder="Filter layers… (e.g. git, gpg)"
                          value={layerQuery}
                          onChange={(e) => setLayerQuery(e.target.value)}
                          className="text-xs"
                        />
                        {(() => {
                          const q = layerQuery.trim().toLowerCase();
                          const visible = q
                            ? layersCatalog.filter(
                                (l) =>
                                  l.name.toLowerCase().includes(q) ||
                                  (l.title || "").toLowerCase().includes(q) ||
                                  (l.description || "").toLowerCase().includes(q),
                              )
                            : layersCatalog;
                          return (
                            <div className="max-h-56 overflow-y-auto border rounded-md p-2 space-y-1">
                              {visible.length === 0 ? (
                                <p className="text-xs italic text-muted-foreground text-center py-2">
                                  No layer matches "{layerQuery}"
                                </p>
                              ) : (
                                visible.map((l) => {
                                  const ref = `${l.image}:${l.latest || "latest"}`;
                                  const selected = hadronLayers.includes(ref);
                                  return (
                                    <label
                                      key={l.name}
                                      className={`flex items-center gap-2 text-xs cursor-pointer px-2 py-1 rounded hover:bg-muted/60 ${selected ? "bg-primary/10" : ""}`}
                                    >
                                      <input
                                        type="checkbox"
                                        checked={selected}
                                        onChange={() =>
                                          setHadronLayers(
                                            selected
                                              ? hadronLayers.filter((r) => r !== ref)
                                              : [...hadronLayers, ref],
                                          )
                                        }
                                      />
                                      <span className="font-mono min-w-[8ch]">{l.name}</span>
                                      <span className="flex-1 text-muted-foreground truncate">
                                        {l.description || l.title}
                                      </span>
                                      <span className="text-muted-foreground text-[10px]">{l.latest}</span>
                                    </label>
                                  );
                                })
                              )}
                            </div>
                          );
                        })()}
                      </>
                    )}
                    <div className="flex gap-2">
                      <Input
                        className="font-mono text-xs"
                        placeholder="Custom layer ref e.g. ghcr.io/kairos-io/hadron-layers/git:2.55.0"
                        value={layerDraft}
                        onChange={(e) => setLayerDraft(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key !== "Enter") return;
                          e.preventDefault();
                          const ref = layerDraft.trim();
                          if (!ref || hadronLayers.includes(ref)) return;
                          setHadronLayers([...hadronLayers, ref]);
                          setLayerDraft("");
                        }}
                      />
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          const ref = layerDraft.trim();
                          if (!ref || hadronLayers.includes(ref)) return;
                          setHadronLayers([...hadronLayers, ref]);
                          setLayerDraft("");
                        }}
                      >
                        Add
                      </Button>
                    </div>
                  </div>

                  <div className="grid gap-2">
                    <Label className="text-xs">
                      Extra Dockerfile (appended)
                      <InfoTooltip>
                        Appended verbatim after all COPY layers. Useful for
                        ad-hoc RUN steps that don't warrant a layer image.
                      </InfoTooltip>
                    </Label>
                    <Textarea
                      className="font-mono text-xs min-h-[5rem]"
                      placeholder="RUN echo 'hello' > /etc/motd"
                      value={hadronExtra}
                      onChange={(e) => setHadronExtra(e.target.value)}
                    />
                  </div>
                </CardContent>
                )}
              </Card>
            )}
          </div>
        )}

        {/* Step 3: Extensions */}
        {step === "extensions" && (
          <div className="grid gap-6">
            <p className="text-sm text-muted-foreground">
              Extensions come from two places. <strong className="text-foreground">Install after boot</strong>:
              extensions built on this instance ride along with every upgrade to the artifact.{" "}
              <strong className="text-foreground">Bake into the image</strong>: extensions from a catalog are
              written into the ISO.
            </p>
            {/* A toast is gone by the time the operator reaches this step, so
                the failed read is stated where the empty list is shown. */}
            {cloneBundleUnavailable && (
              <p className="text-sm text-destructive">
                The bundled extensions of the cloned artifact could not be read, so none were
                carried over. This list is not the source artifact&apos;s bundle.
              </p>
            )}
            {/* Bundled extensions: ride along with every upgrade to this artifact. */}
            <BundledExtensionsCard
              arch={form.arch}
              bundled={form.bundledExtensions ?? []}
              onChange={(next) => update("bundledExtensions", next)}
              // The catalog picker is on this step too, so the jump scrolls to
              // its URL field.
              onGoToCatalog={() => setFocusTarget("extensionsCatalog")}
            />

              {/* Catalog extensions materialized into the ISO */}
              <Card>
                <CardHeader>
                  <CardTitle className="text-sm flex items-center gap-2">
                    <Package className="h-4 w-4 text-primary" />
                    System Extensions
                    <InfoTooltip>
                      Extensions are resolved from the catalog and written into the
                      ISO, so the installed system carries them without pulling
                      anything at first boot. Only extensions published for the{" "}
                      {form.arch} architecture are listed. To use an extension
                      this {PRODUCT_NAME} instance built itself, and carry it with
                      every upgrade to the artifact instead, see "Extensions
                      built on this instance" above.
                    </InfoTooltip>
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  {!hadronBuild && configuredCatalogs.length === 0 && catalogMissing && (
                    <div className="rounded-md border border-dashed p-3 grid gap-3">
                      <p className="text-sm text-muted-foreground">
                        The default catalog publishes extensions built for Hadron,
                        so this flavor has no catalog yet. Use one of the catalogs
                        below, type the URL of your own, or save catalogs in{" "}
                        <Link to="/settings" className="underline text-foreground">
                          Settings
                        </Link>{" "}
                        so every build offers them. You can also start the server
                        with <code className="font-mono text-xs">--extensions-catalog</code>.
                      </p>
                      <div className="grid gap-2">
                        {SUGGESTED_EXTENSION_CATALOGS.map((suggestion) => (
                          <div
                            key={suggestion.url}
                            className="rounded-md border p-2 flex items-start gap-2"
                          >
                            <div className="grid gap-0.5 min-w-0 flex-1">
                              <span className="text-xs font-medium">{suggestion.name}</span>
                              <span className="text-xs font-mono text-muted-foreground truncate">
                                {suggestion.url}
                              </span>
                              <span className="text-xs text-muted-foreground">
                                {suggestion.description}
                              </span>
                            </div>
                            <Button
                              type="button"
                              size="sm"
                              variant="outline"
                              onClick={() => pickExtensionsCatalog(suggestion.url)}
                              aria-label={`Use ${suggestion.name}`}
                            >
                              Use
                            </Button>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  <div className="grid gap-1">
                    <Label className="text-xs">
                      Catalog
                      <InfoTooltip>
                        {hadronBuild
                          ? "Defaults to the Kairos hadron-layers catalog, the same index a node reads. Point it at your own index to publish your own extensions."
                          : "The catalogs given with --extensions-catalog and saved in Settings are offered here. Point it at any index to use its extensions."}
                      </InfoTooltip>
                    </Label>
                    {catalogChoices.length > 1 && (
                      <div className="flex flex-wrap gap-1.5 mb-1">
                        {catalogChoices.map((url) => (
                          <Button
                            key={url}
                            type="button"
                            size="sm"
                            variant={url === extensionsCatalog.trim() ? "default" : "outline"}
                            className="h-7 max-w-full text-xs font-mono"
                            title={url}
                            aria-pressed={url === extensionsCatalog.trim()}
                            onClick={() => pickExtensionsCatalog(url)}
                          >
                            <span className="truncate">{url}</span>
                          </Button>
                        ))}
                      </div>
                    )}
                    <div className="flex gap-2">
                      <Input
                        ref={bindRef("extensionsCatalog")}
                        value={extensionsCatalog}
                        onChange={(e) => {
                          setExtensionsCatalogOverride(e.target.value);
                          setExtensionsCatalogTouched(true);
                        }}
                        placeholder={
                          hadronBuild
                            ? DEFAULT_EXTENSIONS_CATALOG
                            : "https://example.com/extensions/releases.json"
                        }
                        className="font-mono text-xs"
                        aria-label="Extension catalog URL"
                      />
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        onClick={() => loadExtensionsCatalog(extensionsCatalog)}
                        disabled={catalogMissing}
                      >
                        {extensionsCatalogState === "loading" ? (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        ) : (
                          "Load"
                        )}
                      </Button>
                    </div>
                  </div>

                  {extensionsCatalogState === "error" && (
                    <p className="text-sm text-amber-700">
                      Could not read that catalog. Check the URL, or that it serves
                      CORS headers for this browser.
                    </p>
                  )}

                  {extensionsCatalogState === "ready" && availableExtensions.length === 0 && (
                    <p className="text-sm text-muted-foreground">
                      That catalog publishes no extension for {form.arch}.
                    </p>
                  )}

                  {availableExtensions.length > 0 && (
                    <div className="grid gap-2 md:grid-cols-2">
                      {availableExtensions.map((item) => {
                        const selected = selectedExtensions[item.name] !== undefined;
                        return (
                          <div
                            key={item.name}
                            className="rounded-md border p-2 flex items-start gap-2"
                          >
                            <input
                              type="checkbox"
                              className="mt-1"
                              id={`extension-${item.name}`}
                              checked={selected}
                              onChange={() =>
                                setSelectedExtensions((current) => {
                                  const next = { ...current };
                                  if (selected) {
                                    delete next[item.name];
                                  } else {
                                    next[item.name] = LATEST_VERSION;
                                  }
                                  return next;
                                })
                              }
                            />
                            <div className="grid gap-1 min-w-0 flex-1">
                              <Label
                                htmlFor={`extension-${item.name}`}
                                className="text-xs font-mono"
                              >
                                {item.name}
                              </Label>
                              {item.description && (
                                <p className="text-xs text-muted-foreground truncate">
                                  {item.description}
                                </p>
                              )}
                              {selected && (
                                <Select
                                  value={selectedExtensions[item.name]}
                                  onValueChange={(v) =>
                                    setSelectedExtensions((current) => ({
                                      ...current,
                                      [item.name]: v,
                                    }))
                                  }
                                >
                                  <SelectTrigger
                                    className="h-7 text-xs"
                                    aria-label={`${item.name} version`}
                                  >
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent>
                                    <SelectItem value={LATEST_VERSION}>
                                      Latest{item.latest ? ` (${item.latest})` : ""}
                                    </SelectItem>
                                    {item.versions.map((v) => (
                                      <SelectItem key={v.version} value={v.version}>
                                        {v.version}
                                      </SelectItem>
                                    ))}
                                  </SelectContent>
                                </Select>
                              )}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  )}

                  {catalogMissing && selectedExtensionNames.length > 0 && (
                    <div className="rounded-md bg-amber-500/10 border border-amber-500/25 p-3 flex gap-2">
                      <AlertTriangle className="h-4 w-4 text-amber-600 shrink-0 mt-0.5" />
                      <p className="text-sm text-amber-700">
                        No catalog is set, so {selectedExtensionNames.join(", ")}{" "}
                        will not be baked into the image. Pick a catalog to keep
                        them.
                      </p>
                    </div>
                  )}

                  {unavailableExtensions.length > 0 && (
                    <div className="rounded-md bg-amber-500/10 border border-amber-500/25 p-3 flex gap-2">
                      <AlertTriangle className="h-4 w-4 text-amber-600 shrink-0 mt-0.5" />
                      <p className="text-sm text-amber-700">
                        This catalog publishes nothing for {form.arch} for{" "}
                        {unavailableExtensions.join(", ")}. The build will fail
                        unless you deselect them or pick another architecture.
                      </p>
                    </div>
                  )}

                  {selectedExtensionNames.length > 0 && !form.outputs.iso && (
                    <div className="rounded-md bg-amber-500/10 border border-amber-500/25 p-3 flex gap-2">
                      <AlertTriangle className="h-4 w-4 text-amber-600 shrink-0 mt-0.5" />
                      <p className="text-sm text-amber-700">
                        Extensions are written into the ISO, so select the ISO
                        output for them to end up anywhere.
                      </p>
                    </div>
                  )}
                </CardContent>
              </Card>

            <Card>
              <CardContent className="pt-6">
                {/* Pre-configure for system extensions: bakes a systemd drop-in
                    so the OS image accepts overlays on the listed paths
                    without manual setup at upgrade time. */}
                <details>
                  <summary className="cursor-pointer text-sm font-semibold flex items-center gap-2">
                    Pre-configure for system extensions
                    <span className="text-xs font-normal opacity-60">Optional · advanced</span>
                  </summary>
                  <p className="text-xs text-muted-foreground mt-2">
                    Bakes a systemd drop-in so the node accepts sysext overlays on the listed paths without manual setup.
                  </p>
                  <div className="mt-3">
                    <div className="text-xs text-muted-foreground mb-1.5">Additional sysext hierarchies</div>
                    <HierarchyChipInput
                      value={form.extensionHierarchies?.sysext ?? []}
                      onChange={(next) =>
                        update("extensionHierarchies", {
                          sysext: next,
                          confext: form.extensionHierarchies?.confext ?? [],
                        })
                      }
                      implicitRoot="/usr"
                      quickAdds={["/opt", "/srv", "/var/lib"]}
                    />
                  </div>
                  <details className="mt-3">
                    <summary className="cursor-pointer text-xs opacity-80">Confext hierarchies (optional)</summary>
                    <div className="mt-2">
                      <HierarchyChipInput
                        value={form.extensionHierarchies?.confext ?? []}
                        onChange={(next) =>
                          update("extensionHierarchies", {
                            sysext: form.extensionHierarchies?.sysext ?? [],
                            confext: next,
                          })
                        }
                        implicitRoot="/etc"
                      />
                    </div>
                  </details>
                </details>
              </CardContent>
            </Card>
          </div>
        )}

        {/* Step 4: Access */}
        {step === "access" && (
          <div className="grid gap-6">
            {/* Access */}
            <Card>
              <CardHeader>
                <CardTitle className="text-sm">Access</CardTitle>
              </CardHeader>
              <CardContent className="grid gap-4">
                <div className="grid gap-2">
                  <Label>
                    User Setup
                    <InfoTooltip>
                      How the default login is provisioned on first boot: none, a default <code>kairos</code>/<code>kairos</code> user, or a custom one.{" "}
                      <a
                        href="https://kairos.io/docs/reference/configuration/"
                        target="_blank"
                        rel="noopener noreferrer"
                        className="underline"
                      >
                        Docs
                      </a>
                    </InfoTooltip>
                  </Label>
                  <SegmentedControl<UserMode>
                    ariaLabel="User setup"
                    value={userMode}
                    onChange={(mode) => {
                      setUserMode(mode);
                      if (mode === "default") {
                        setUsername("kairos");
                        setPassword("kairos");
                      }
                    }}
                    options={[
                      { value: "default", label: "Default user" },
                      { value: "custom", label: "Custom user" },
                      { value: "none", label: "No user" },
                    ]}
                  />
                </div>

                {userMode === "default" && (
                  <div className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2">
                    <p className="text-sm text-warning-foreground">
                      The default user is <code className="font-mono">kairos</code> and its password is{" "}
                      <code className="font-mono">kairos</code>. Change it before you put the node on a network you
                      do not trust, or pick a custom user.
                    </p>
                  </div>
                )}

                {userMode !== "none" && (
                  <div className="grid grid-cols-2 gap-3">
                    <div className="grid gap-2">
                      <Label>
                        Username
                        <InfoTooltip>
                          Login user created at first boot.
                        </InfoTooltip>
                      </Label>
                      <Input
                        value={username}
                        onChange={(e) => setUsername(e.target.value)}
                        disabled={userMode === "default"}
                      />
                    </div>
                    <div className="grid gap-2">
                      <Label>
                        Password
                        <InfoTooltip>
                          Stored as plain text in the generated cloud-config. Prefer SSH keys for anything you actually care about.
                        </InfoTooltip>
                      </Label>
                      <Input
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        disabled={userMode === "default"}
                      />
                    </div>
                  </div>
                )}

                {userMode === "none" && (
                  <div className="rounded-md bg-amber-500/10 border border-amber-500/25 p-3">
                    <p className="text-sm text-amber-700">
                      No user will be created. You must configure access via the advanced cloud-config section in the Output step.
                    </p>
                  </div>
                )}

                {userMode !== "none" && (
                  <div className="grid gap-2">
                    <Label>
                      SSH Authorized Keys (optional)
                      <InfoTooltip>
                        One key per line. Kairos also accepts <code>github:user</code> and <code>gitlab:user</code> shortcuts.{" "}
                        <a
                          href="https://kairos.io/docs/reference/configuration/"
                          target="_blank"
                          rel="noopener noreferrer"
                          className="underline"
                        >
                          Docs
                        </a>
                      </InfoTooltip>
                    </Label>
                    <Textarea
                      placeholder={"ssh-rsa AAAAB3... user@host\nssh-ed25519 AAAA... other@host"}
                      value={sshKeys}
                      onChange={(e) => setSshKeys(e.target.value)}
                      rows={3}
                      className="font-mono text-xs"
                    />
                    <p className="text-xs text-muted-foreground">One key per line.</p>
                  </div>
                )}
              </CardContent>
            </Card>
              {/* Provisioning */}
              <Card>
                <CardHeader className="pb-3">
                  <CardTitle className="text-sm">Provisioning</CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div>
                    <label className="flex items-center gap-2 text-sm font-medium">
                      <input
                        type="checkbox"
                        checked={form.provisioning.autoInstall}
                        onChange={(e) => updateProvisioning("autoInstall", e.target.checked)}
                        className="rounded border-input"
                      />
                      Auto-install
                    </label>
                    <p className="text-xs text-muted-foreground mt-1 ml-6">
                      Install to disk on first boot, no interaction needed.
                    </p>
                    {/* When the user disables auto-install we nudge them toward
                        the upstream Kairos docs so they know how to drive the
                        install by hand — otherwise a booted image just sits at
                        the live prompt and looks broken. Colors are picked for
                        WCAG AA contrast against the light/dark backgrounds. */}
                    {!form.provisioning.autoInstall && (
                      <div className="mt-2 ml-6 rounded-md border border-amber-400/60 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-400/40 dark:bg-amber-950/60 dark:text-amber-100">
                        With auto-install off, the image boots into a live
                        environment and you install it manually.{" "}
                        <a
                          href="https://kairos.io/docs/installation/"
                          target="_blank"
                          rel="noopener noreferrer"
                          className="font-semibold underline underline-offset-2 hover:text-amber-950 dark:hover:text-white"
                        >
                          Read the installation guide →
                        </a>
                      </div>
                    )}
                  </div>

                  <div>
                    <label className="flex items-center gap-2 text-sm font-medium">
                      <input
                        type="checkbox"
                        checked={form.provisioning.registerAuroraBoot}
                        onChange={(e) => updateProvisioning("registerAuroraBoot", e.target.checked)}
                        className="rounded border-input"
                      />
                      Register with {PRODUCT_NAME}
                    </label>
                    <p className="text-xs text-muted-foreground mt-1 ml-6">
                      Node will phone home to this {PRODUCT_NAME} server on boot.
                    </p>
                  </div>

                  {form.provisioning.registerAuroraBoot && (
                    <div className="grid gap-2 pt-1">
                      <Label className="text-xs">
                        Target Group
                        <InfoTooltip>
                          Assigns the registered node to a fleet group on first boot. Groups are managed from the Groups page.
                        </InfoTooltip>
                      </Label>
                      <Select
                        value={form.provisioning.targetGroupId || "__none__"}
                        onValueChange={(v) => updateProvisioning("targetGroupId", v === "__none__" ? "" : v)}
                      >
                        <SelectTrigger>
                          <SelectValue placeholder="Select group (optional)" />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="__none__">None</SelectItem>
                          {groups.map((g) => (
                            <SelectItem key={g.id} value={g.id}>
                              {g.name}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                  )}

                  {form.provisioning.registerAuroraBoot && (
                    <CommandPresets
                      value={form.provisioning.allowedCommands ?? []}
                      onChange={(next) => updateProvisioning("allowedCommands", next)}
                      renderCustom={() => (
                        <AllowedCommandsPicker
                          value={form.provisioning.allowedCommands ?? []}
                          onChange={(next) => updateProvisioning("allowedCommands", next)}
                        />
                      )}
                    />
                  )}
                </CardContent>
              </Card>

              {/* Security */}
              <Card>
                <CardHeader className="pb-3">
                  <CardTitle className="text-sm">Security</CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div>
                    <label className="flex items-center gap-2 text-sm font-medium">
                      <input
                        type="checkbox"
                        checked={!!form.outputs.fips}
                        onChange={(e) => updateOutput("fips", e.target.checked)}
                        className="rounded border-input"
                      />
                      FIPS
                    </label>
                    <p className="text-xs text-muted-foreground mt-1 ml-6">
                      Build with FIPS-validated cryptographic modules.
                    </p>
                  </div>
                </CardContent>
              </Card>
          </div>
        )}

        {/* Step 5: Output */}
        {step === "output" && (
          <div className="grid gap-6 lg:grid-cols-5">
            <div className="lg:col-span-3 space-y-6">
              <Card>
                <CardHeader className="pb-3">
                  <CardTitle className="text-sm">Boot mode</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  <div role="radiogroup" aria-label="Boot mode" className="grid gap-3 sm:grid-cols-2">
                    {[
                      {
                        trusted: false,
                        label: "Standard",
                        desc: "Boots through GRUB. Every output format is available.",
                      },
                      {
                        trusted: true,
                        label: "Trusted Boot",
                        desc: "Boots a signed Unified Kernel Image, measured by the TPM.",
                      },
                    ].map((mode) => {
                      const selected = !!form.outputs.trustedBoot === mode.trusted;
                      return (
                        <label
                          key={mode.label}
                          className={`flex gap-3 rounded-lg border p-3 cursor-pointer transition-colors ${
                            selected
                              ? "border-primary bg-primary-soft ring-1 ring-primary/20"
                              : "border-border hover:border-primary/40"
                          }`}
                        >
                          <input
                            type="radio"
                            name="bootMode"
                            checked={selected}
                            onChange={() => setTrustedBoot(mode.trusted)}
                            className="mt-1"
                          />
                          <span>
                            <span className="block font-medium text-sm">{mode.label}</span>
                            <span className="block text-xs text-muted-foreground mt-0.5">{mode.desc}</span>
                          </span>
                        </label>
                      );
                    })}
                  </div>
                  {form.outputs.trustedBoot && (
                    <p className="text-xs text-muted-foreground">
                      A Trusted Boot image carries no GRUB and no separate initrd, so it ships as a
                      signed UKI installer ISO and as a container tarball. Netboot and disk images
                      need Standard mode.
                    </p>
                  )}
                </CardContent>
              </Card>

              <Card ref={bindRef("outputs")}>
                <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-3">
                  <CardTitle className="text-sm">Outputs</CardTitle>
                  <span className="text-xs text-muted-foreground">
                    {selectedOutputCount} selected
                  </span>
                </CardHeader>
                <CardContent className="space-y-6">
                  {outputGroups.map((group) => (
                    <div key={group.title}>
                      <p className="text-xs font-medium text-muted-foreground mb-3 uppercase tracking-wide">
                        {group.title}
                      </p>
                      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                        {group.items.map((item) => {
                          const Icon = item.icon;
                          const checked = !!form.outputs[item.field];
                          return (
                            <button
                              key={item.field}
                              type="button"
                              aria-pressed={checked}
                              onClick={() => updateOutput(item.field, !checked)}
                              className={`relative text-left rounded-lg border p-3 transition-colors ${
                                checked
                                  ? "border-primary bg-primary-soft ring-1 ring-primary/20"
                                  : "border-border hover:border-primary/40"
                              }`}
                            >
                              {checked && (
                                <span className="absolute top-2 right-2 h-4 w-4 rounded-full bg-primary text-primary-foreground flex items-center justify-center">
                                  <Check className="h-3 w-3" strokeWidth={3} />
                                </span>
                              )}
                              <Icon
                                className={`h-5 w-5 mb-2 ${
                                  checked ? "text-primary" : "text-muted-foreground"
                                }`}
                              />
                              <p className="font-medium text-sm">{item.label}</p>
                              <p className="text-xs text-muted-foreground mt-0.5">{item.desc}</p>
                            </button>
                          );
                        })}
                      </div>
                    </div>
                  ))}

                  {form.outputs.trustedBoot && form.outputs.iso && (
                    <div className="rounded-md bg-amber-500/10 border border-amber-500/25 p-3 flex gap-2">
                      <AlertTriangle className="h-4 w-4 text-amber-600 shrink-0 mt-0.5" />
                      <p className="text-sm text-amber-700">
                        The Trusted Boot ISO is signed with secure boot keys. Pick a key set in the panel below.
                      </p>
                    </div>
                  )}
                </CardContent>
              </Card>

              {/* UKI Signing Keys (only for a Trusted Boot ISO) */}
              {form.outputs.trustedBoot && form.outputs.iso && (
                <Card>
                  <CardHeader>
                    <CardTitle className="text-sm flex items-center gap-2">
                      <ShieldCheck className="h-4 w-4 text-primary" />
                      UKI Signing Keys
                    </CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    <div className="grid gap-1">
                      <Label className="text-xs">
                        Key Set
                        <InfoTooltip>
                          Secure boot key set stored on this {PRODUCT_NAME} server. Create or import one under Certificates.
                        </InfoTooltip>
                      </Label>
                      <Select
                        value={form.signing.ukiKeySetId || "__none__"}
                        onValueChange={(v) => updateSigning("ukiKeySetId", v === "__none__" ? "" : v)}
                      >
                        <SelectTrigger ref={bindRef("ukiKeySetId")}>
                          <SelectValue placeholder="Select key set" />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="__none__">Select a key set...</SelectItem>
                          {keySets.map((ks) => (
                            <SelectItem key={ks.id} value={ks.id}>
                              {ks.name}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="grid gap-1">
                      <Label className="text-xs">Enrollment Policy</Label>
                      <Select
                        value={form.signing.ukiSecureBootEnroll}
                        onValueChange={(v) => updateSigning("ukiSecureBootEnroll", v)}
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="if-safe">if-safe</SelectItem>
                          <SelectItem value="force">force</SelectItem>
                          <SelectItem value="manual">manual</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                  </CardContent>
                </Card>
              )}
            </div>

            <div className="lg:col-span-2 space-y-6">
              {/* Overlay Files */}
              <Card role="region" aria-label="Overlay Files">
                <CardHeader className="pb-3">
                  <CardTitle className="text-sm">Overlay Files</CardTitle>
                </CardHeader>
                <CardContent>
                  <div
                    onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
                    onDragLeave={() => setDragOver(false)}
                    onDrop={(e) => {
                      e.preventDefault();
                      setDragOver(false);
                      void uploadOverlay(Array.from(e.dataTransfer.files));
                    }}
                    className={`border-2 border-dashed rounded-lg p-5 text-center cursor-pointer transition-colors ${
                      dragOver ? "border-primary bg-primary-soft" : "border-muted-foreground/25 hover:border-muted-foreground/50"
                    }`}
                    onClick={() => overlayInputRef.current?.click()}
                  >
                    {overlayUploading ? (
                      <div className="flex items-center justify-center gap-2">
                        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                        <span className="text-sm text-muted-foreground">Uploading...</span>
                      </div>
                    ) : form.overlayId ? (
                      <div className="space-y-2">
                        {overlayFiles.length > 0 ? (
                          <div className="flex flex-wrap gap-1.5 justify-center">
                            {overlayFiles.map((name) => (
                              <span key={name} className="text-xs bg-secondary px-2 py-1 rounded font-mono">{name}</span>
                            ))}
                          </div>
                        ) : (
                          // A clone or an imported config carries the overlay
                          // by ID only; its file names are not on the record.
                          <p className="text-sm text-muted-foreground">
                            Uses a previously uploaded overlay
                          </p>
                        )}
                        <button
                          type="button"
                          className="text-xs text-red-500 hover:text-red-700 flex items-center gap-1 mx-auto"
                          onClick={(e) => {
                            e.stopPropagation();
                            update("overlayId", "");
                            setOverlayFiles([]);
                          }}
                        >
                          <X className="h-3 w-3" /> Clear files
                        </button>
                      </div>
                    ) : (
                      <>
                        <Upload className="h-7 w-7 mx-auto text-muted-foreground/40 mb-2" />
                        <p className="text-sm text-muted-foreground">Drop files or a .tar.gz here</p>
                        <p className="text-xs text-muted-foreground/60 mt-1">Overlaid on top of the rootfs</p>
                      </>
                    )}
                    <input
                      ref={overlayInputRef}
                      type="file"
                      multiple
                      className="hidden"
                      onChange={(e) => {
                        const files = Array.from(e.target.files ?? []);
                        // Reset the input so the same files can be picked again.
                        e.target.value = "";
                        void uploadOverlay(files);
                      }}
                    />
                  </div>
                </CardContent>
              </Card>

              {/* Advanced — single collapsible card containing both Cloud Config + Kairos Init Image */}
              <Card>
                <button
                  type="button"
                  className="w-full flex items-center justify-between px-6 py-4 text-sm font-medium hover:bg-muted/30 transition-colors rounded-t-lg"
                  onClick={() => setShowAdvanced(!showAdvanced)}
                >
                  <span className="flex items-center gap-2">
                    {showAdvanced ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                    Advanced
                  </span>
                  <span className="text-xs text-muted-foreground">Cloud config &amp; init image</span>
                </button>
                {showAdvanced && (
                  <CardContent className="space-y-5 pt-0">
                    <div className="grid gap-2">
                      <Label className="text-xs">
                        Cloud Config (appended)
                        <InfoTooltip>
                          Raw YAML appended to the generated #cloud-config. Use for custom stages, units, or packages.{" "}
                          <a
                            href="https://kairos.io/docs/reference/configuration/"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="underline"
                          >
                            Docs
                          </a>
                        </InfoTooltip>
                      </Label>
                      <Textarea
                        placeholder={"# Additional cloud-config\nstages:\n  boot:\n    - name: disable-ssh\n      commands:\n        - systemctl disable sshd"}
                        value={advancedConfig}
                        onChange={(e) => setAdvancedConfig(e.target.value)}
                        rows={6}
                        className="font-mono text-xs"
                      />
                      <p className="text-xs text-muted-foreground">
                        Appended to the generated config. {PRODUCT_NAME} registration is auto-injected by the server.
                      </p>
                    </div>
                    <div className="grid gap-2">
                      <Label className="text-xs">
                        Kairos Init Image
                        <InfoTooltip>
                          Override the kairos-init container used during the build. Advanced — leave empty to use the default.{" "}
                          <a
                            href="https://kairos.io/docs/reference/kairos-factory/"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="underline"
                          >
                            Docs
                          </a>
                        </InfoTooltip>
                      </Label>
                      <Input
                        placeholder="e.g. quay.io/kairos/kairos-init:latest"
                        value={form.kairosInitImage || ""}
                        onChange={(e) => update("kairosInitImage", e.target.value)}
                        className="font-mono text-xs"
                      />
                      <p className="text-xs text-muted-foreground">
                        Override the default kairos-init image used when kairosifying base distros.
                      </p>
                    </div>
                  </CardContent>
                )}
              </Card>
            </div>
          </div>
        )}

        {/* Step 6: Review */}
        {step === "review" && (
          <div className="grid gap-6">
            <BuildSummary data={summaryData} variant="full" onEdit={editStep} />
            {(advancedConfig.trim() || userMode !== "default") && (
              <Card>
                <CardContent className="grid gap-4">
                {/* Cloud Config Preview */}
                {(() => {
                  const isTruncated = cloudConfigPreview.length > 500;
                  const previewText =
                    !isTruncated || showFullCloudConfigPreview
                      ? cloudConfigPreview
                      : cloudConfigPreview.slice(0, 500) + "\n...";
                  return (
                    <div>
                      <div className="flex items-center justify-between mb-1">
                        <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Cloud Config Preview</p>
                        {isTruncated && (
                          <button
                            type="button"
                            aria-expanded={showFullCloudConfigPreview}
                            className="text-xs text-primary hover:underline"
                            onClick={() =>
                              setExpandedCloudConfigPreview(showFullCloudConfigPreview ? null : cloudConfigPreview)
                            }
                          >
                            {showFullCloudConfigPreview ? "Show less" : "View full"}
                          </button>
                        )}
                      </div>
                      <pre
                        className={`text-xs font-mono bg-muted/50 rounded p-3 overflow-x-auto overflow-y-auto whitespace-pre-wrap ${
                          showFullCloudConfigPreview ? "max-h-96" : "max-h-40"
                        }`}
                      >
                        {previewText}
                      </pre>
                    </div>
                  );
                })()}
                </CardContent>
              </Card>
            )}
          </div>
        )}
      </form>
      </WizardShell>
    </div>
  );
}

interface BundledExtensionEntry {
  name: string;
  type: "sysext" | "confext";
  pinnedVersion?: string;
  order?: number;
}

// BundledExtensionsCard is one of the two places the builder asks about
// extensions. This one offers what this AuroraBoot instance built, and the
// selection rides along with every upgrade to the artifact. The other is
// System Extensions below it on the Extensions step, which resolves names from a catalog
// and writes them into the artifact itself.
//
// Both cards are kept, because they answer different questions (built here or
// published upstream, carried with upgrades or written in), but each one now
// names the other. Without that, a fresh instance shows this card empty on the
// step before, and it reads as "extensions do not work", which is what
// kairos-io/kairos#4959 reports.
function BundledExtensionsCard({
  arch,
  bundled,
  onChange,
  onGoToCatalog,
}: {
  arch: string;
  bundled: BundledExtensionEntry[];
  onChange: (next: BundledExtensionEntry[]) => void;
  onGoToCatalog: () => void;
}) {
  const [available, setAvailable] = useState<Extension[]>([]);

  useEffect(() => {
    listExtensions()
      .then((rows) =>
        setAvailable(
          rows.filter((e) => e.phase === "Ready" && e.arch === arch),
        ),
      )
      .catch(() => {});
  }, [arch]);

  const bundledKeys = new Set(bundled.map((b) => `${b.type}:${b.name}`));

  function addToBundle(ext: Extension) {
    onChange([
      ...bundled,
      { name: ext.name, type: ext.type, order: bundled.length },
    ]);
  }

  function removeFromBundle(idx: number) {
    onChange(bundled.filter((_, i) => i !== idx));
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">
          Extensions built on this instance
        </CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-xs text-muted-foreground mb-3">
          Extensions this {PRODUCT_NAME} instance built on the Extensions page.
          What you select here rides along with every upgrade to this artifact.
          Only <code>Ready</code> extensions matching arch <code>{arch}</code>{" "}
          are listed.
        </p>
        <p className="text-xs text-muted-foreground mb-3">
          To use an extension published in a catalog, and write it into the
          artifact itself, see{" "}
          <button
            type="button"
            className="underline underline-offset-2 hover:text-foreground"
            onClick={onGoToCatalog}
          >
            System Extensions
          </button>{" "}
          below.
        </p>

        {bundled.length > 0 && (
          <div className="mb-3">
            <div className="text-xs text-muted-foreground mb-1.5">
              {bundled.length} extension{bundled.length === 1 ? "" : "s"} bundled
            </div>
            <div className="flex flex-wrap gap-1.5">
              {bundled.map((b, i) => (
                <span
                  key={`${b.type}:${b.name}`}
                  className="inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full border bg-muted"
                >
                  <ExtensionTypeChip type={b.type} />
                  <code className="font-mono">{b.name}</code>
                  {b.pinnedVersion && (
                    <code className="text-[10px] opacity-60">
                      @{b.pinnedVersion}
                    </code>
                  )}
                  <button
                    type="button"
                    aria-label={`Remove ${b.name} from bundle`}
                    className="opacity-60 hover:opacity-100"
                    onClick={() => removeFromBundle(i)}
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          </div>
        )}

        {available.length === 0 ? (
          <div className="space-y-2">
            <p className="text-xs text-muted-foreground">
              Nothing built on this instance matches arch <code>{arch}</code>.
              Build one from the Extensions page, or take one from a catalog
              instead.
            </p>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={onGoToCatalog}
            >
              Pick from a catalog
            </Button>
          </div>
        ) : (
          <div className="grid gap-1.5 max-h-64 overflow-auto">
            {available.map((e) => {
              const inBundle = bundledKeys.has(`${e.type}:${e.name}`);
              return (
                <div
                  key={e.id}
                  className="flex items-center justify-between gap-2 text-sm px-2.5 py-1.5 rounded-md border bg-background"
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <ExtensionTypeChip type={e.type} />
                    <span className="font-medium truncate">{e.name}</span>
                    <code className="text-[11px] opacity-60">{e.version}</code>
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    aria-label={`Add ${e.name} to bundle`}
                    disabled={inBundle}
                    onClick={() => addToBundle(e)}
                  >
                    {inBundle ? "Added" : "Add"}
                  </Button>
                </div>
              );
            })}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
