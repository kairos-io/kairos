import { describe, it, expect } from "vitest";

import type { Node } from "@/api/nodes";
import { ociImage } from "@/lib/nodeInfo";

function node(osRelease: Record<string, string> | null): Node {
  return {
    id: "n1",
    hostname: "edge-1",
    machineID: "m1",
    groupID: "",
    labels: {},
    phase: "Online",
    osRelease,
    agentVersion: "",
    lastHeartbeat: null,
    createdAt: "",
    updatedAt: "",
  };
}

// kairos-io/kairos#4547: the node view never showed which OCI image the node
// runs, although the agent has reported it all along. sdk/versioneer writes
// KAIROS_IMAGE_REPO and KAIROS_IMAGE_LABEL into /etc/kairos-release, and the
// agent merges that whole file into the heartbeat's osRelease.
describe("ociImage", () => {
  it("joins the reported repository and tag", () => {
    expect(
      ociImage(
        node({
          KAIROS_IMAGE_REPO: "quay.io/kairos/ubuntu",
          KAIROS_IMAGE_LABEL: "24.04-standard-amd64-generic-v3.4.2",
        }),
      ),
    ).toBe("quay.io/kairos/ubuntu:24.04-standard-amd64-generic-v3.4.2");
  });

  it("returns the repository alone when no tag was reported", () => {
    expect(ociImage(node({ KAIROS_IMAGE_REPO: "quay.io/kairos/ubuntu" }))).toBe("quay.io/kairos/ubuntu");
  });

  it("returns an empty string when no repository was reported", () => {
    expect(ociImage(node({ KAIROS_IMAGE_LABEL: "v3.4.2" }))).toBe("");
    expect(ociImage(node({}))).toBe("");
    expect(ociImage(node(null))).toBe("");
  });

  it("does not append the tag twice when the repository already carries it", () => {
    expect(
      ociImage(
        node({
          KAIROS_IMAGE_REPO: "quay.io/kairos/ubuntu:v3.4.2",
          KAIROS_IMAGE_LABEL: "v3.4.2",
        }),
      ),
    ).toBe("quay.io/kairos/ubuntu:v3.4.2");
  });

  it("leaves a digest pinned repository alone", () => {
    const digest = "quay.io/kairos/ubuntu@sha256:" + "a".repeat(64);
    expect(ociImage(node({ KAIROS_IMAGE_REPO: digest, KAIROS_IMAGE_LABEL: "v3.4.2" }))).toBe(digest);
  });

  it("keeps a registry port from reading as a tag", () => {
    expect(
      ociImage(
        node({
          KAIROS_IMAGE_REPO: "registry.local:5000/kairos/ubuntu",
          KAIROS_IMAGE_LABEL: "5000",
        }),
      ),
    ).toBe("registry.local:5000/kairos/ubuntu:5000");
  });

  it("ignores surrounding whitespace", () => {
    expect(
      ociImage(
        node({
          KAIROS_IMAGE_REPO: "  quay.io/kairos/ubuntu  ",
          KAIROS_IMAGE_LABEL: "  v3.4.2  ",
        }),
      ),
    ).toBe("quay.io/kairos/ubuntu:v3.4.2");
  });
});
