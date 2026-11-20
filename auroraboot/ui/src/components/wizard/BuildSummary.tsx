import { useId, type ReactNode } from "react";

import type { Artifact } from "@/api/artifacts";
import { StatusDot } from "@/components/fleet/StatusDot";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export type BuildSummaryData = {
  name: string;
  base: string;
  arch: string;
  model: string;
  variant: string;
  kubernetes?: string;
  version: string;
  bundledExtensions: string[];
  // Set when a clone could not read its source's bundle. The list is then
  // empty for a reason the operator has to be told, because an empty list and
  // an unreadable one are the same picture.
  bundledExtensionsUnavailable?: boolean;
  catalogExtensions: string[];
  user: "default" | "custom" | "none";
  sshKeyCount: number;
  register: boolean;
  targetGroup?: string;
  commands: string[];
  fips: boolean;
  trustedBoot: boolean;
  outputs: string[];
  overlayFiles: number;
  // Set when an overlay is attached but its file names are not known, as for
  // a stored artifact or a clone, which reference the overlay by ID only.
  overlayAttached?: boolean;
  autoInstall: boolean;
  insecureRegistries: boolean;
};

export interface BuildSummaryProps {
  data: BuildSummaryData;
  onEdit?(step: string): void;
  variant: "aside" | "full";
}

// Commands that can run arbitrary code or wipe the node.
const DESTRUCTIVE_COMMANDS = ["exec", "reset", "apply-cloud-config"];

const USER_LABELS: Record<BuildSummaryData["user"], string> = {
  default: "Default user",
  custom: "Custom user",
  none: "No user",
};

// Output formats in the order the builder lists them.
const OUTPUT_LABELS: [keyof Artifact, string][] = [
  ["iso", "ISO"],
  ["netboot", "Netboot"],
  ["rawDisk", "Raw Disk"],
  ["cloudImage", "Cloud Image"],
  ["gce", "Google Cloud"],
  ["vhd", "Azure (VHD)"],
  ["maas", "MAAS"],
  ["tar", "TAR"],
];

type Row = { label: string; value: ReactNode; warning?: string; mono?: boolean };
type Section = { title: string; step: string; rows: Row[] };

function list(items: string[]): string {
  return items.length > 0 ? items.join(", ") : "None";
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

function yesNo(v: boolean): string {
  return v ? "Yes" : "No";
}

function sections(d: BuildSummaryData): Section[] {
  const access: Row[] = [
    { label: "User", value: USER_LABELS[d.user], warning: d.user === "default" ? "default password" : undefined },
    { label: "SSH keys", value: plural(d.sshKeyCount, "SSH key", "SSH keys") },
    { label: "Install", value: d.autoInstall ? "Auto-install on first boot" : "Manual install" },
    { label: "Register", value: yesNo(d.register) },
  ];
  if (d.register) {
    access.push({ label: "Target group", value: d.targetGroup || "None" });
    access.push({
      label: "Commands",
      value: d.commands.length > 0 ? d.commands.join(", ") : "Observe-only (no commands)",
      warning: d.commands.some((c) => DESTRUCTIVE_COMMANDS.includes(c)) ? "destructive commands allowed" : undefined,
      mono: d.commands.length > 0,
    });
  }
  return [
    {
      title: "Base",
      step: "base",
      rows: [
        { label: "Name", value: d.name || "None" },
        { label: "Image", value: d.base || "None", mono: true },
      ],
    },
    {
      title: "System",
      step: "system",
      rows: [
        { label: "Architecture", value: d.arch || "None" },
        { label: "Model", value: d.model || "None" },
        { label: "Variant", value: d.variant || "None" },
        { label: "Kubernetes distro", value: d.kubernetes || "None" },
        { label: "Version", value: d.version || "None" },
      ],
    },
    {
      title: "Extensions",
      step: "extensions",
      rows: [
        {
          label: "Install after boot",
          // "None" here is a claim about the source artifact. When the read
          // failed we do not know, so the row says that instead.
          value: d.bundledExtensionsUnavailable ? "Unknown" : list(d.bundledExtensions),
          warning: d.bundledExtensionsUnavailable
            ? "could not be read from the cloned artifact, nothing carried over"
            : undefined,
        },
        { label: "Bake into the image", value: list(d.catalogExtensions) },
      ],
    },
    { title: "Access", step: "access", rows: access },
    {
      title: "Output",
      step: "output",
      rows: [
        { label: "Boot mode", value: d.trustedBoot ? "Trusted Boot" : "Standard" },
        { label: "Formats", value: list(d.outputs) },
        {
          label: "Overlay files",
          value:
            d.overlayFiles === 0 && d.overlayAttached
              ? "Attached"
              : plural(d.overlayFiles, "file", "files"),
        },
      ],
    },
    {
      title: "Security",
      step: "access",
      rows: [
        { label: "FIPS", value: yesNo(d.fips) },
        {
          label: "Insecure registries",
          value: yesNo(d.insecureRegistries),
          warning: d.insecureRegistries ? "insecure registries allowed" : undefined,
        },
      ],
    },
  ];
}

// BuildSummary lists the build configuration by step. The builder shows it
// as a compact aside and full width on Review; the artifact page shows it
// read-only (without onEdit).
export function BuildSummary({ data, onEdit, variant }: BuildSummaryProps) {
  const full = variant === "full";
  const id = useId();
  return (
    <Card className={cn(!full && "gap-3 py-4")}>
      <CardHeader className={cn(!full && "px-4")}>
        <CardTitle className="text-sm">Build summary</CardTitle>
      </CardHeader>
      <CardContent className={cn("grid gap-4", full ? "sm:grid-cols-2" : "px-4")}>
        {sections(data).map((s) => (
          <section key={s.title} className="min-w-0">
            <div className="mb-1 flex items-center justify-between gap-2">
              <h3 id={`${id}-${s.title}`} className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{s.title}</h3>
              {onEdit && (
                <button
                  type="button"
                  aria-describedby={`${id}-${s.title}`}
                  className="text-xs text-primary hover:underline focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  onClick={() => onEdit(s.step)}
                >
                  Edit
                </button>
              )}
            </div>
            <dl className="grid gap-1 text-sm">
              {s.rows.map((r) => (
                <div
                  key={r.label}
                  className={cn("flex min-w-0 gap-2", full ? "flex-row" : "flex-col gap-0 sm:flex-row sm:gap-2")}
                >
                  <dt className={cn("shrink-0 text-muted-foreground", full ? "w-36" : "sm:w-28")}>{r.label}</dt>
                  <dd className="flex min-w-0 flex-col">
                    <span className={cn("break-words", r.mono && "break-all font-mono text-xs")}>{r.value}</span>
                    {r.warning && (
                      <span className="flex items-center gap-1.5 text-xs text-warning-foreground">
                        <StatusDot tone="warning" />
                        {r.warning}
                      </span>
                    )}
                  </dd>
                </div>
              ))}
            </dl>
          </section>
        ))}
      </CardContent>
    </Card>
  );
}

// summaryFromArtifact builds the summary from a stored artifact. The store
// does not keep the user setup, SSH keys, allowed commands, bundled
// extensions or overlay file names, so those stay empty.
// eslint-disable-next-line react-refresh/only-export-components -- used by the artifact page
export function summaryFromArtifact(a: Artifact): BuildSummaryData {
  const kubernetes =
    a.variant === "standard" && a.kubernetesEnabled !== false && a.kubernetesDistro
      ? [a.kubernetesDistro, a.kubernetesVersion].filter(Boolean).join(" ")
      : undefined;
  return {
    name: a.name ?? "",
    base: a.baseImage || (a.dockerfile ? "Dockerfile" : ""),
    arch: a.arch ?? "",
    model: a.model ?? "",
    variant: a.variant ?? "",
    kubernetes,
    version: a.kairosVersion ?? "",
    bundledExtensions: [],
    catalogExtensions: a.extensions ?? [],
    user: "none",
    sshKeyCount: 0,
    register: !!a.registerAuroraBoot,
    targetGroup: a.targetGroupId || undefined,
    commands: [],
    fips: !!a.fips,
    trustedBoot: !!a.trustedBoot,
    // There is one ISO option and the boot mode says which ISO it is, so the
    // summary names the one this build produces rather than leaving the
    // reader to infer it from the Trusted Boot row.
    outputs: OUTPUT_LABELS.filter(([k]) => a[k]).map(([k, label]) =>
      k === "iso" && a.trustedBoot ? "UKI ISO" : label,
    ),
    overlayFiles: 0,
    overlayAttached: !!a.overlayId,
    autoInstall: !!a.autoInstall,
    insecureRegistries: !!a["allow-insecure-registries"],
  };
}
