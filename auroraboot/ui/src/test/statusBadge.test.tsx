import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";

import { StatusBadge } from "@/components/StatusBadge";

describe("StatusBadge", () => {
  it("renders a known status", () => {
    render(<StatusBadge status="Active" />);
    expect(screen.getByText("Active")).toBeInTheDocument();
  });

  // A node fetched from an endpoint that does not return a node has no phase.
  // One absent field must not be able to throw out of render and take the
  // whole page with it, whatever put it there.
  it("renders a placeholder instead of throwing when the status is missing", () => {
    const missing = undefined as unknown as string;
    expect(() => render(<StatusBadge status={missing} />)).not.toThrow();
    expect(screen.getByText("unknown")).toBeInTheDocument();
  });

  it("renders a placeholder for an empty status", () => {
    render(<StatusBadge status="" />);
    expect(screen.getByText("unknown")).toBeInTheDocument();
  });

  // Expired is a command phase the server produces. It is terminal but it is
  // not a failure the node reported, so it reads in the neutral tone.
  it("styles Expired with the neutral tone", () => {
    const { container } = render(<StatusBadge status="Expired" />);
    expect(container.firstElementChild?.className).toContain("bg-neutral/15");
    expect(container.firstElementChild?.className).toContain("text-neutral-foreground");
  });

  // Running used to be red-orange, which users read as an error.
  it("styles Running with the info tone", () => {
    const { container } = render(<StatusBadge status="Running" />);
    expect(container.firstElementChild?.className).toContain("bg-info/15");
    expect(container.firstElementChild?.className).toContain("text-info-foreground");
    expect(container.firstElementChild?.className).toContain("border-info/25");
  });

  // Canceled is the phase a fail-fast batch leaves on the nodes it stopped
  // before delivering to them. Like Expired it is terminal, and like Expired
  // it is not a failure the node reported, so it must not read in the danger
  // tone: an operator scanning a stopped rollout would count every untouched
  // node as broken.
  it("styles Canceled with the neutral tone", () => {
    const { container } = render(<StatusBadge status="Canceled" />);
    expect(container.firstElementChild?.className).toContain("bg-neutral/15");
    expect(container.firstElementChild?.className).toContain("text-neutral-foreground");
    expect(screen.getByText("Canceled")).toBeInTheDocument();
  });
});
