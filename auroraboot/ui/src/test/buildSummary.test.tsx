import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { BuildSummary, summaryFromArtifact, type BuildSummaryData } from "@/components/wizard/BuildSummary";
import type { Artifact } from "@/api/artifacts";

const data: BuildSummaryData = {
  name: "edge-fleet",
  base: "quay.io/kairos/ubuntu:24.04",
  arch: "arm64",
  model: "rpi4",
  variant: "standard",
  kubernetes: "k3s v1.30.0",
  version: "v3.4.2",
  bundledExtensions: ["tailscale"],
  catalogExtensions: ["nvidia@1.2"],
  user: "default",
  sshKeyCount: 2,
  register: true,
  targetGroup: "lab",
  commands: ["upgrade", "reboot", "exec"],
  fips: true,
  trustedBoot: false,
  outputs: ["ISO", "Raw Disk"],
  overlayFiles: 3,
  autoInstall: true,
  insecureRegistries: true,
};

describe("BuildSummary", () => {
  it("shows every field", () => {
    render(<BuildSummary data={data} variant="full" />);
    for (const text of [
      "edge-fleet",
      "quay.io/kairos/ubuntu:24.04",
      "arm64",
      "rpi4",
      "standard",
      "k3s v1.30.0",
      "v3.4.2",
      "tailscale",
      "nvidia@1.2",
      "2 SSH keys",
      "lab",
      "upgrade, reboot, exec",
      "ISO, Raw Disk",
      "3 files",
    ]) {
      expect(screen.getByText(text)).toBeInTheDocument();
    }
    expect(screen.getByText("Default user")).toBeInTheDocument();
    expect(screen.getByText("Register")).toBeInTheDocument();
    expect(screen.getByText("FIPS")).toBeInTheDocument();
    expect(screen.getByText("Boot mode")).toBeInTheDocument();
    expect(screen.getByText("Standard")).toBeInTheDocument();
    for (const section of ["Base", "System", "Extensions", "Access", "Output", "Security"]) {
      expect(screen.getByRole("heading", { name: section })).toBeInTheDocument();
    }
  });

  it("says an overlay is attached when its file names are not known", () => {
    render(<BuildSummary data={{ ...data, overlayFiles: 0, overlayAttached: true }} variant="full" />);
    expect(screen.getByText("Attached")).toBeInTheDocument();
    expect(screen.queryByText("0 files")).not.toBeInTheDocument();
  });

  it("renders warnings with the warning tone", () => {
    render(<BuildSummary data={data} variant="aside" />);
    expect(screen.getByText("default password")).toHaveClass("text-warning-foreground");
    expect(screen.getByText("destructive commands allowed")).toHaveClass("text-warning-foreground");
  });

  it("has no warnings for a custom user and safe commands", () => {
    render(
      <BuildSummary data={{ ...data, user: "custom", commands: ["upgrade", "reboot"] }} variant="aside" />,
    );
    expect(screen.queryByText("default password")).not.toBeInTheDocument();
    expect(screen.queryByText("destructive commands allowed")).not.toBeInTheDocument();
  });

  it("shows auto-install in Access", () => {
    const { unmount } = render(<BuildSummary data={data} variant="full" />);
    expect(screen.getByText("Auto-install on first boot")).toBeInTheDocument();
    unmount();
    render(<BuildSummary data={{ ...data, autoInstall: false }} variant="full" />);
    expect(screen.getByText("Manual install")).toBeInTheDocument();
    expect(screen.queryByText("Auto-install on first boot")).not.toBeInTheDocument();
  });

  it("warns when insecure registries are allowed", () => {
    render(<BuildSummary data={data} variant="aside" />);
    expect(screen.getByText("Insecure registries")).toBeInTheDocument();
    expect(screen.getByText("insecure registries allowed")).toHaveClass("text-warning-foreground");
  });

  it("does not warn when insecure registries are not allowed", () => {
    render(<BuildSummary data={{ ...data, insecureRegistries: false }} variant="aside" />);
    expect(screen.getByText("Insecure registries")).toBeInTheDocument();
    expect(screen.queryByText("insecure registries allowed")).not.toBeInTheDocument();
  });

  it("calls onEdit with the step key", () => {
    const onEdit = vi.fn();
    render(<BuildSummary data={data} variant="aside" onEdit={onEdit} />);
    fireEvent.click(screen.getByRole("button", { name: "Edit", description: "System" }));
    expect(onEdit).toHaveBeenCalledWith("system");
    fireEvent.click(screen.getByRole("button", { name: "Edit", description: "Security" }));
    expect(onEdit).toHaveBeenLastCalledWith("access");
  });

  it("hides Edit buttons without onEdit", () => {
    render(<BuildSummary data={data} variant="full" />);
    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
  });
});

describe("summaryFromArtifact", () => {
  it("fills what the stored artifact has", () => {
    const a = {
      id: "a1",
      name: "stored",
      phase: "Ready",
      message: "",
      baseImage: "quay.io/kairos/fedora:40",
      kairosVersion: "v3.1.0",
      model: "generic",
      arch: "amd64",
      variant: "standard",
      iso: true,
      cloudImage: false,
      netboot: true,
      rawDisk: false,
      tar: false,
      gce: false,
      vhd: false,
      maas: false,
      fips: true,
      trustedBoot: false,
      autoInstall: false,
      registerAuroraBoot: true,
      targetGroupId: "g1",
      kubernetesDistro: "k3s",
      kubernetesVersion: "v1.30.0",
      extensions: ["nvidia"],
      artifacts: [],
      createdAt: "",
      updatedAt: "",
    } as Artifact;
    const s = summaryFromArtifact(a);
    expect(s).toMatchObject({
      name: "stored",
      base: "quay.io/kairos/fedora:40",
      arch: "amd64",
      model: "generic",
      variant: "standard",
      kubernetes: "k3s v1.30.0",
      version: "v3.1.0",
      catalogExtensions: ["nvidia"],
      bundledExtensions: [],
      register: true,
      targetGroup: "g1",
      fips: true,
      trustedBoot: false,
      outputs: ["ISO", "Netboot"],
      commands: [],
      sshKeyCount: 0,
      overlayFiles: 0,
      autoInstall: false,
      insecureRegistries: false,
    });
    expect(
      summaryFromArtifact({ ...a, autoInstall: true, "allow-insecure-registries": true }),
    ).toMatchObject({ autoInstall: true, insecureRegistries: true });
  });

  it("shows the boot mode with the output formats", () => {
    render(<BuildSummary data={{ ...data, trustedBoot: true, outputs: ["UKI ISO"] }} variant="full" />);
    const output = screen.getByRole("heading", { name: "Output" }).closest("section")!;
    expect(output.textContent).toContain("Boot mode");
    expect(output.textContent).toContain("Trusted Boot");
    expect(output.textContent).toContain("UKI ISO");
    const security = screen.getByRole("heading", { name: "Security" }).closest("section")!;
    expect(security.textContent).not.toContain("Trusted Boot");
  });

  it("calls the ISO of a Trusted Boot artifact the UKI ISO", () => {
    const a = { trustedBoot: true, iso: true, tar: true, artifacts: [] } as unknown as Artifact;
    expect(summaryFromArtifact(a).outputs).toEqual(["UKI ISO", "TAR"]);
  });

  it("marks the overlay attached when the artifact has one", () => {
    const base = { artifacts: [] } as unknown as Artifact;
    expect(summaryFromArtifact(base).overlayAttached).toBeFalsy();
    expect(
      summaryFromArtifact({ ...base, overlayId: "6f1c2a0e-3b7d-4c1e-9a52-0d8e4f7b1c3a" }).overlayAttached,
    ).toBe(true);
  });

  it("leaves Kubernetes empty on core or when disabled", () => {
    const base = { variant: "core", kubernetesDistro: "k3s", artifacts: [] } as unknown as Artifact;
    expect(summaryFromArtifact(base).kubernetes).toBeUndefined();
    expect(
      summaryFromArtifact({ ...base, variant: "standard", kubernetesEnabled: false }).kubernetes,
    ).toBeUndefined();
  });

  it("defaults auto-install and insecure registries to false", () => {
    const s = summaryFromArtifact({ artifacts: [] } as unknown as Artifact);
    expect(s.autoInstall).toBe(false);
    expect(s.insecureRegistries).toBe(false);
  });
});
