import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  clearReleaseCache,
  compareTagsDesc,
  fetchKubernetesDistroReleases,
} from "./githubReleases";

type GithubRelease = {
  tag_name: string;
  draft?: boolean;
  prerelease?: boolean;
};

function mockFetch(responses: Array<{ ok?: boolean; status?: number; body: unknown }>) {
  let call = 0;
  return vi.fn(async () => {
    const r = responses[Math.min(call, responses.length - 1)];
    call += 1;
    return {
      ok: r.ok ?? true,
      status: r.status ?? 200,
      json: async () => r.body,
    } as Response;
  });
}

const k3sReleases: GithubRelease[] = [
  { tag_name: "v1.31.2+k3s1" },
  { tag_name: "v1.31.2-rc1+k3s1", prerelease: true },
  { tag_name: "v1.31.1+k3s1" },
  { tag_name: "v1.31.0+k3s1", draft: true },
  { tag_name: "v1.30.6+k3s1" },
  { tag_name: "v1.30.5+k3s1" },
  { tag_name: "v1.30.4+k3s1" },
  { tag_name: "v1.30.3+k3s1" },
];

describe("fetchKubernetesDistroReleases", () => {
  beforeEach(() => {
    sessionStorage.clear();
    clearReleaseCache();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("hits the k3s releases endpoint and returns stable tags only", async () => {
    const fetchMock = mockFetch([{ body: k3sReleases }]);
    vi.stubGlobal("fetch", fetchMock);

    const tags = await fetchKubernetesDistroReleases("k3s", 5);

    expect(fetchMock).toHaveBeenCalledOnce();
    const url = fetchMock.mock.calls[0][0] as string;
    expect(url).toContain("api.github.com/repos/k3s-io/k3s/releases");
    expect(tags).toEqual([
      "v1.31.2+k3s1",
      "v1.31.1+k3s1",
      "v1.30.6+k3s1",
      "v1.30.5+k3s1",
      "v1.30.4+k3s1",
    ]);
  });

  it("uses the k0s repo for the k0s distro", async () => {
    const fetchMock = mockFetch([
      { body: [{ tag_name: "v1.31.2+k0s.0" }, { tag_name: "v1.31.1+k0s.0" }] },
    ]);
    vi.stubGlobal("fetch", fetchMock);

    await fetchKubernetesDistroReleases("k0s", 5);

    const url = fetchMock.mock.calls[0][0] as string;
    expect(url).toContain("api.github.com/repos/k0sproject/k0s/releases");
  });

  it("caches results in sessionStorage across calls", async () => {
    const fetchMock = mockFetch([{ body: k3sReleases }]);
    vi.stubGlobal("fetch", fetchMock);

    const first = await fetchKubernetesDistroReleases("k3s", 5);
    const second = await fetchKubernetesDistroReleases("k3s", 5);

    expect(fetchMock).toHaveBeenCalledOnce();
    expect(second).toEqual(first);
  });

  it("throws a friendly error on rate limit", async () => {
    const fetchMock = mockFetch([
      { ok: false, status: 403, body: { message: "API rate limit exceeded" } },
    ]);
    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchKubernetesDistroReleases("k3s", 5)).rejects.toThrow(/rate limit/i);
  });

  it("clamps per_page to GitHub's max of 100 even when limit is high", async () => {
    const fetchMock = mockFetch([{ body: [] }]);
    vi.stubGlobal("fetch", fetchMock);

    await fetchKubernetesDistroReleases("k3s", 500);

    const url = fetchMock.mock.calls[0][0] as string;
    expect(url).toContain("per_page=100");
  });

  it("throws on other non-OK responses", async () => {
    const fetchMock = mockFetch([{ ok: false, status: 500, body: {} }]);
    vi.stubGlobal("fetch", fetchMock);

    await expect(fetchKubernetesDistroReleases("k3s", 5)).rejects.toThrow(/500/);
  });

  it("sorts by semver desc even when GitHub returns backports interleaved", async () => {
    // GitHub orders by publish date, so a v1.30.x backport can land above a
    // higher v1.32.x release. We want the higher line at the top.
    const interleaved: GithubRelease[] = [
      { tag_name: "v1.30.14+k3s3" },
      { tag_name: "v1.32.5+k3s1" },
      { tag_name: "v1.31.10+k3s2" },
      { tag_name: "v1.32.4+k3s1" },
      { tag_name: "v1.31.9+k3s1" },
    ];
    const fetchMock = mockFetch([{ body: interleaved }]);
    vi.stubGlobal("fetch", fetchMock);

    const tags = await fetchKubernetesDistroReleases("k3s", 5);

    expect(tags).toEqual([
      "v1.32.5+k3s1",
      "v1.32.4+k3s1",
      "v1.31.10+k3s2",
      "v1.31.9+k3s1",
      "v1.30.14+k3s3",
    ]);
  });
});

describe("compareTagsDesc", () => {
  it("orders higher MAJOR.MINOR.PATCH first", () => {
    const tags = ["v1.30.14+k3s3", "v1.32.5+k3s1", "v1.31.10+k3s2"];
    expect([...tags].sort(compareTagsDesc)).toEqual([
      "v1.32.5+k3s1",
      "v1.31.10+k3s2",
      "v1.30.14+k3s3",
    ]);
  });

  it("treats 10 as newer than 9 numerically, not lexically", () => {
    expect(compareTagsDesc("v1.31.10+k3s1", "v1.31.9+k3s1")).toBeLessThan(0);
  });

  it("tie-breaks equal MAJOR.MINOR.PATCH by suffix", () => {
    expect(compareTagsDesc("v1.32.5+k3s2", "v1.32.5+k3s1")).toBeLessThan(0);
  });

  it("compares suffix numerically so k3s10 outranks k3s9", () => {
    expect(compareTagsDesc("v1.32.5+k3s10", "v1.32.5+k3s9")).toBeLessThan(0);
    const tags = ["v1.32.5+k3s9", "v1.32.5+k3s10", "v1.32.5+k3s2"];
    expect([...tags].sort(compareTagsDesc)).toEqual([
      "v1.32.5+k3s10",
      "v1.32.5+k3s9",
      "v1.32.5+k3s2",
    ]);
  });
});
