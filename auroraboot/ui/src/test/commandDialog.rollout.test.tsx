import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { CommandDialog } from "@/components/CommandDialog";

vi.mock("@/api/artifacts", () => ({
  listArtifacts: vi.fn().mockResolvedValue([]),
  resolveBundle: vi.fn().mockResolvedValue([]),
}));

function openForm(
  onSubmit: ReturnType<typeof vi.fn>,
  action: string,
  targetCount?: number,
) {
  render(
    <CommandDialog
      open
      onOpenChange={() => {}}
      onSubmit={onSubmit}
      title="Send command"
      targetCount={targetCount}
    />,
  );
  fireEvent.click(screen.getByText(action));
}

describe("the Rollout section", () => {
  it("shows only the reboot timeout for a single node and sends it", () => {
    const onSubmit = vi.fn();
    openForm(onSubmit, "Reboot", 1);
    expect(screen.getByText("1 node")).toBeInTheDocument();
    expect(screen.queryByLabelText("Nodes at a time")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Stop on the first failure")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Wait for a node to come back"), {
      target: { value: "15" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send reboot" }));
    expect(onSubmit.mock.calls[0][2]).toEqual({ stopOnFailure: true, rebootTimeoutSeconds: 900 });
  });

  it("sends a rollout for a single node whose command does not reboot", () => {
    const onSubmit = vi.fn();
    openForm(onSubmit, "Run command", 1);
    expect(screen.queryByText("Rollout")).not.toBeInTheDocument();
    fireEvent.change(screen.getByPlaceholderText("e.g. uname -a"), { target: { value: "true" } });
    fireEvent.click(screen.getByRole("button", { name: "Run command" }));
    expect(onSubmit.mock.calls[0][2]).toEqual({ stopOnFailure: true, rebootTimeoutSeconds: 600 });
  });

  it("carries a repeated batch's timeout over to a single node", () => {
    const onSubmit = vi.fn();
    const dialog = (open: boolean) => (
      <CommandDialog
        open={open}
        onOpenChange={() => {}}
        onSubmit={onSubmit}
        defaultCommand="reboot"
        defaultRollout={{ concurrency: 2, stopOnFailure: false, rebootTimeoutSeconds: 1200 }}
        nodeChoices={[{ id: "n1", label: "host-n1" }]}
      />
    );
    // The dialog seeds its defaults when it opens.
    const { rerender } = render(dialog(false));
    rerender(dialog(true));
    expect(screen.getByLabelText("Wait for a node to come back")).toHaveValue(20);
    fireEvent.click(screen.getByRole("button", { name: "Send reboot" }));
    expect(onSubmit.mock.calls[0][2]).toEqual({ stopOnFailure: false, rebootTimeoutSeconds: 1200 });
  });

  it("sends no rollout when the command does not start a batch", () => {
    const onSubmit = vi.fn();
    openForm(onSubmit, "Reboot");
    expect(screen.queryByLabelText("Wait for a node to come back")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Send reboot" }));
    expect(onSubmit.mock.calls[0][2]).toBeUndefined();
  });

  it("sends the defaults when the user changes nothing", () => {
    const onSubmit = vi.fn();
    openForm(onSubmit, "Reboot", 3);
    expect(screen.getByText("3 nodes")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Reboot 3 nodes" }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const rollout = onSubmit.mock.calls[0][2];
    expect(rollout).toEqual({ stopOnFailure: true, rebootTimeoutSeconds: 600 });
    expect(Object.keys(rollout)).not.toContain("concurrency");
  });

  it("sends the nodes at a time, the stop setting and the timeout in seconds", () => {
    const onSubmit = vi.fn();
    openForm(onSubmit, "Reboot", 3);
    fireEvent.change(screen.getByLabelText("Nodes at a time"), { target: { value: "2" } });
    fireEvent.click(screen.getByLabelText("Stop on the first failure"));
    fireEvent.change(screen.getByLabelText("Wait for a node to come back"), {
      target: { value: "15" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Reboot 3 nodes" }));
    expect(onSubmit.mock.calls[0][2]).toEqual({
      concurrency: 2,
      stopOnFailure: false,
      rebootTimeoutSeconds: 900,
    });
  });

  it("shows the timeout only for commands that reboot the node", () => {
    const { unmount } = render(
      <CommandDialog open onOpenChange={() => {}} onSubmit={vi.fn()} targetCount={3} />,
    );
    fireEvent.click(screen.getByText("Reboot"));
    expect(screen.getByLabelText("Wait for a node to come back")).toBeInTheDocument();
    unmount();

    render(
      <CommandDialog open onOpenChange={() => {}} onSubmit={vi.fn()} targetCount={3} />,
    );
    fireEvent.click(screen.getByText("Run command"));
    expect(screen.getByText("Rollout")).toBeInTheDocument();
    expect(screen.queryByLabelText("Wait for a node to come back")).not.toBeInTheDocument();
  });

  it("names the node count on the submit button", () => {
    openForm(vi.fn(), "Reboot", 3);
    expect(screen.getByRole("button", { name: "Reboot 3 nodes" })).toBeInTheDocument();
  });
});

describe("the Nodes at a time setting", () => {
  it.each(["-1", "1.5"])("blocks the submit and explains why for %s", (value) => {
    const onSubmit = vi.fn();
    openForm(onSubmit, "Reboot", 3);
    const input = screen.getByLabelText("Nodes at a time");
    fireEvent.change(input, { target: { value } });
    expect(screen.getByText("Enter a whole number, or leave it empty to run on all nodes at once.")).toBeInTheDocument();
    expect(input).toHaveAttribute("aria-invalid", "true");
    const submit = screen.getByRole("button", { name: "Reboot 3 nodes" });
    expect(submit).toBeDisabled();
    fireEvent.click(submit);
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("blocks the submit for text the browser cannot read as a number", () => {
    const onSubmit = vi.fn();
    openForm(onSubmit, "Reboot", 3);
    const input = screen.getByLabelText("Nodes at a time");
    // A browser reports "two" in a number input as an empty value with
    // validity.badInput set; jsdom never sets it, so the test does.
    Object.defineProperty(input, "validity", { value: { badInput: true }, configurable: true });
    fireEvent.input(input, { target: { value: "" } });
    expect(screen.getByText("Enter a whole number, or leave it empty to run on all nodes at once.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reboot 3 nodes" })).toBeDisabled();
  });

  it("treats 0 as all nodes at once", () => {
    const onSubmit = vi.fn();
    openForm(onSubmit, "Reboot", 3);
    fireEvent.change(screen.getByLabelText("Nodes at a time"), { target: { value: "0" } });
    expect(screen.queryByText(/Enter a whole number/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Reboot 3 nodes" }));
    expect(onSubmit.mock.calls[0][2]).toEqual({ stopOnFailure: true, rebootTimeoutSeconds: 600 });
  });
});
