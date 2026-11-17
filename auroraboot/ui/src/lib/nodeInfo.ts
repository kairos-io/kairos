// Helpers that read hardware and network facts out of a node record. The
// agent reports most of them as strings inside osRelease.

import type { Node } from "@/api/nodes";
import { formatBytes } from "@/lib/time";

const memUnits: Record<string, number> = {
  b: 1,
  kb: 1024,
  kib: 1024,
  mb: 1024 ** 2,
  mib: 1024 ** 2,
  gb: 1024 ** 3,
  gib: 1024 ** 3,
};

// memGiB parses osRelease.MEM_TOTAL ("8024512 kB", as in /proc/meminfo, where
// kB means KiB) into a readable size such as "7.7 GiB".
export function memGiB(node: Node): string | null {
  const raw = node.osRelease?.MEM_TOTAL?.trim();
  if (!raw) return null;
  const m = /^(\d+(?:\.\d+)?)\s*([a-z]*)$/i.exec(raw);
  if (!m) return null;
  const unit = m[2] ? memUnits[m[2].toLowerCase()] : 1024;
  if (!unit) return null;
  return formatBytes(parseFloat(m[1]) * unit);
}

// cpuCount reads osRelease.CPU_COUNT as a number.
export function cpuCount(node: Node): number | null {
  const raw = node.osRelease?.CPU_COUNT?.trim();
  if (!raw || !/^\d+$/.test(raw)) return null;
  return parseInt(raw, 10);
}

// imageVersion is the Kairos version the node runs, or "".
export function imageVersion(node: Node): string {
  return node.osRelease?.KAIROS_VERSION ?? "";
}

// nodeAddress prefers the IP the server saw the node connect from, then the
// first address the agent reported.
export function nodeAddress(node: Node): string {
  return node.remoteIP || node.addresses?.[0]?.address || "";
}

// hasBootIssue is true when the node did not boot its active image.
export function hasBootIssue(node: Node): boolean {
  const s = (node.bootState ?? "").toLowerCase();
  return s === "recovery" || s === "passive";
}

// ociImage is the OCI reference of the image the node is running, assembled
// from the repository and tag the agent reports out of /etc/kairos-release
// (KAIROS_IMAGE_REPO and KAIROS_IMAGE_LABEL, written by sdk/versioneer).
// Returns "" when no repository was reported, because a tag on its own names
// no image.
export function ociImage(node: Node): string {
  const repo = node.osRelease?.KAIROS_IMAGE_REPO?.trim() ?? "";
  if (!repo) return "";
  const tag = node.osRelease?.KAIROS_IMAGE_LABEL?.trim() ?? "";
  // A repository pinned by digest, or one that already carries the tag, names
  // the image on its own. Comparing against the whole ":<tag>" suffix keeps a
  // registry port such as "registry:5000/kairos/ubuntu" from reading as a tag.
  if (!tag || repo.includes("@") || repo.endsWith(`:${tag}`)) return repo;
  return `${repo}:${tag}`;
}
