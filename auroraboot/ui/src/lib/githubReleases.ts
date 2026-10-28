export type KubernetesDistro = "k3s" | "k0s";

type GithubRelease = {
  tag_name: string;
  draft?: boolean;
  prerelease?: boolean;
};

const REPOS: Record<KubernetesDistro, string> = {
  k3s: "k3s-io/k3s",
  k0s: "k0sproject/k0s",
};

const CACHE_PREFIX = "auroraboot:releases:v2:";
const memoryCache = new Map<string, string[]>();

function cacheKey(distro: KubernetesDistro, limit: number): string {
  return `${CACHE_PREFIX}${distro}:${limit}`;
}

function readCache(key: string): string[] | null {
  const mem = memoryCache.get(key);
  if (mem) return mem;
  try {
    const raw = sessionStorage.getItem(key);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed) && parsed.every((t) => typeof t === "string")) {
      memoryCache.set(key, parsed);
      return parsed;
    }
  } catch {
    // ignore corrupt cache
  }
  return null;
}

function writeCache(key: string, tags: string[]): void {
  memoryCache.set(key, tags);
  try {
    sessionStorage.setItem(key, JSON.stringify(tags));
  } catch {
    // sessionStorage may be unavailable (e.g. private mode); fall back to memory only.
  }
}

export function clearReleaseCache(): void {
  memoryCache.clear();
}

// Parse the leading vMAJOR.MINOR.PATCH out of a tag like "v1.36.1+k3s1" so
// backported releases (e.g. v1.30.14 cut after v1.32.5) sort under the
// higher-versioned line even though GitHub lists them by publish date.
function parseVersion(tag: string): [number, number, number] | null {
  const match = tag.match(/^v?(\d+)\.(\d+)\.(\d+)/);
  if (!match) return null;
  return [Number(match[1]), Number(match[2]), Number(match[3])];
}

// Trailing digits in the provider suffix — e.g. `+k3s10` → 10, `+k0s.2` → 2.
// Ignores anything before `+` so a bare `v1.32.5` doesn't pull the patch
// number into the tie-break; falls back to 0 when no suffix number exists.
function parseSuffixNumber(tag: string): number {
  const plus = tag.indexOf("+");
  if (plus < 0) return 0;
  const match = tag.slice(plus + 1).match(/(\d+)\D*$/);
  return match ? Number(match[1]) : 0;
}

export function compareTagsDesc(a: string, b: string): number {
  const va = parseVersion(a);
  const vb = parseVersion(b);
  if (va && vb) {
    for (let i = 0; i < 3; i += 1) {
      if (va[i] !== vb[i]) return vb[i] - va[i];
    }
    const suffixDiff = parseSuffixNumber(b) - parseSuffixNumber(a);
    if (suffixDiff !== 0) return suffixDiff;
    return b.localeCompare(a);
  }
  if (va) return -1;
  if (vb) return 1;
  return b.localeCompare(a);
}

export async function fetchKubernetesDistroReleases(
  distro: KubernetesDistro,
  limit: number,
): Promise<string[]> {
  const key = cacheKey(distro, limit);
  const cached = readCache(key);
  if (cached) return cached;

  // per_page pulls more than `limit` so we can drop prereleases/drafts and
  // still return `limit` stable tags. Clamped to GitHub's max of 100.
  const perPage = Math.min(Math.max(limit * 3, 10), 100);
  const url = `https://api.github.com/repos/${REPOS[distro]}/releases?per_page=${perPage}`;

  const res = await fetch(url, {
    headers: { Accept: "application/vnd.github+json" },
  });

  if (!res.ok) {
    if (res.status === 403) {
      throw new Error(
        "GitHub API rate limit reached. Please wait a bit and try again, or enter the version manually.",
      );
    }
    throw new Error(`GitHub returned ${res.status} when listing ${distro} releases.`);
  }

  const body = (await res.json()) as GithubRelease[];
  const tags = body
    .filter((r) => !r.draft && !r.prerelease && typeof r.tag_name === "string")
    .map((r) => r.tag_name)
    .sort(compareTagsDesc)
    .slice(0, limit);

  writeCache(key, tags);
  return tags;
}
