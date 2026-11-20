import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { CommandDialog } from "@/components/CommandDialog";

vi.mock("@/api/artifacts", () => ({
  listArtifacts: vi.fn().mockResolvedValue([]),
  resolveBundle: vi.fn().mockResolvedValue([]),
}));

// The server (store.CmdApplyCloudConfig) and the agent only know the command
// as apply-cloud-config.
describe("the Apply config form", () => {
  it("sends apply-cloud-config with the YAML", () => {
    const onSubmit = vi.fn();
    render(<CommandDialog open onOpenChange={() => {}} onSubmit={onSubmit} />);
    fireEvent.click(screen.getByText("Apply config"));
    fireEvent.change(screen.getByPlaceholderText(/^#cloud-config/), {
      target: { value: "#cloud-config\n" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send config" }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0]).toBe("apply-cloud-config");
    expect(onSubmit.mock.calls[0][1]).toEqual({ config: "#cloud-config" });
  });
});
