import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { CommandDialog } from "@/components/CommandDialog";

// The agent refuses these two arguments on a reset, in
// agent/internal/phonehome/handlers.go handleReset, before it switches the
// boot entry or schedules the reboot. A Reset command carrying either one is
// not a degraded reset, it is no reset at all: the node stays on its active
// entry and the command is recorded Failed. So the dialog must not be able to
// mint them, whatever the operator does to the Reset form.
const REFUSED_BY_THE_AGENT = ["reset-oem", "config"];

vi.mock("@/api/artifacts", () => ({
  listArtifacts: vi.fn().mockResolvedValue([]),
  resolveBundle: vi.fn().mockResolvedValue([]),
}));

function openResetForm(onSubmit: ReturnType<typeof vi.fn>) {
  render(
    <CommandDialog
      open
      onOpenChange={() => {}}
      onSubmit={onSubmit}
      title="Send command"
    />,
  );
  fireEvent.click(screen.getByText("Reset"));
}

describe("the Reset form", () => {
  it("sends no argument the agent refuses, even with every control filled in", () => {
    const onSubmit = vi.fn();
    openResetForm(onSubmit);

    // Drive every control the form offers, rather than naming the ones that
    // exist today: a control added later is caught the same way.
    for (const box of screen.queryAllByRole("checkbox")) {
      fireEvent.click(box);
    }
    for (const field of screen.queryAllByRole("textbox")) {
      fireEvent.change(field, { target: { value: "#cloud-config\n" } });
    }

    fireEvent.click(screen.getByRole("button", { name: "Send reset" }));

    expect(onSubmit).toHaveBeenCalledTimes(1);
    const [command, args] = onSubmit.mock.calls[0];
    expect(command).toBe("reset");
    for (const refused of REFUSED_BY_THE_AGENT) {
      expect(Object.keys(args)).not.toContain(refused);
    }
  });

  it("still says the reset wipes persistent data", () => {
    openResetForm(vi.fn());
    expect(screen.getByText(/wipe all persistent data/i)).toBeInTheDocument();
  });
});
