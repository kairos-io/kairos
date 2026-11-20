import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";

import { WizardShell, type WizardStep } from "@/components/wizard/WizardShell";

const steps: WizardStep[] = [
  { key: "base", label: "Base", summary: "Ubuntu 24.04", state: "done" },
  { key: "system", label: "System", state: "current" },
  { key: "output", label: "Output", state: "todo" },
];

function renderShell(overrides: Partial<Parameters<typeof WizardShell>[0]> = {}) {
  const onStepChange = vi.fn();
  const onClick = vi.fn();
  const onBack = vi.fn();
  render(
    <WizardShell
      steps={steps}
      current="system"
      onStepChange={onStepChange}
      aside={<div>Build summary</div>}
      footer={{
        onBack,
        status: { tone: "success", text: "All required fields set" },
        secondary: <button type="button">Save as template</button>,
        primary: { label: "Next: Output", onClick },
      }}
      {...overrides}
    >
      <p>Form body</p>
    </WizardShell>,
  );
  return { onStepChange, onClick, onBack };
}

describe("WizardShell", () => {
  it("renders the stepper, the form, the aside and the footer", () => {
    renderShell();
    const nav = screen.getByRole("navigation", { name: "Steps" });
    expect(within(nav).getAllByRole("button")).toHaveLength(3);
    expect(screen.getByText("Form body")).toBeInTheDocument();
    expect(screen.getByText("Build summary")).toBeInTheDocument();
    expect(screen.getByText("Ubuntu 24.04")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save as template" })).toBeInTheDocument();
  });

  it("marks the current step", () => {
    renderShell();
    const nav = screen.getByRole("navigation", { name: "Steps" });
    const current = within(nav).getByRole("button", { name: /System/ });
    expect(current).toHaveAttribute("aria-current", "step");
    expect(within(nav).getByRole("button", { name: /Base/ })).not.toHaveAttribute("aria-current");
  });

  it("calls onStepChange when a done step is clicked", () => {
    const { onStepChange } = renderShell();
    const nav = screen.getByRole("navigation", { name: "Steps" });
    fireEvent.click(within(nav).getByRole("button", { name: /Base/ }));
    expect(onStepChange).toHaveBeenCalledWith("base");
  });

  it("disables todo steps", () => {
    const { onStepChange } = renderShell();
    const nav = screen.getByRole("navigation", { name: "Steps" });
    const todo = within(nav).getByRole("button", { name: /Output/ });
    expect(todo).toBeDisabled();
    fireEvent.click(todo);
    expect(onStepChange).not.toHaveBeenCalled();
  });

  it("lets an error step be clicked", () => {
    const onStepChange = vi.fn();
    render(
      <WizardShell
        steps={[
          { key: "a", label: "Alpha", state: "error" },
          { key: "b", label: "Beta", state: "current" },
        ]}
        current="b"
        onStepChange={onStepChange}
        footer={{ primary: { label: "Next", onClick: () => {} } }}
      >
        body
      </WizardShell>,
    );
    fireEvent.click(screen.getByRole("button", { name: /Alpha/ }));
    expect(onStepChange).toHaveBeenCalledWith("a");
  });

  it("shows the primary label, calls onClick and the back button", () => {
    const { onClick, onBack } = renderShell();
    fireEvent.click(screen.getByRole("button", { name: "Next: Output" }));
    expect(onClick).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Back" }));
    expect(onBack).toHaveBeenCalled();
  });

  it("uses a custom back label and hides back without onBack", () => {
    const { unmount } = render(
      <WizardShell
        steps={steps}
        current="system"
        onStepChange={() => {}}
        footer={{ onBack: () => {}, backLabel: "Cancel", primary: { label: "Next", onClick: () => {} } }}
      >
        body
      </WizardShell>,
    );
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
    unmount();
    render(
      <WizardShell steps={steps} current="system" onStepChange={() => {}} footer={{ primary: { label: "Next", onClick: () => {} } }}>
        body
      </WizardShell>,
    );
    expect(screen.queryByRole("button", { name: "Back" })).not.toBeInTheDocument();
  });

  it("shows the loading state on the primary button", () => {
    renderShell({
      footer: { primary: { label: "Start build", onClick: () => {}, loading: true } },
    });
    const primary = screen.getByRole("button", { name: "Start build" });
    expect(primary).toBeDisabled();
    expect(primary).toHaveAttribute("aria-busy", "true");
  });

  it("disables the primary button when asked", () => {
    renderShell({
      footer: { primary: { label: "Start build", onClick: () => {}, disabled: true } },
    });
    expect(screen.getByRole("button", { name: "Start build" })).toBeDisabled();
  });

  it("renders the status text in a sticky footer", () => {
    renderShell();
    const status = screen.getByText("All required fields set");
    expect(status).toBeInTheDocument();
    const footer = status.closest("footer");
    expect(footer).not.toBeNull();
    expect(footer!.className).toContain("sticky");
    expect(footer!.className).toContain("bottom-0");
  });
});
