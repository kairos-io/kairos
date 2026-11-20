import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Button", () => {
  const variants = [
    ["default", "bg-primary"],
    ["outline", "border-input"],
    ["secondary", "bg-secondary"],
    ["ghost", "hover:bg-accent"],
    ["destructive", "bg-danger"],
    ["destructive-outline", "text-danger-foreground"],
    ["link", "text-primary"],
  ] as const;

  it.each(variants)("renders the %s variant with %s", (variant, cls) => {
    render(<Button variant={variant}>Go</Button>);
    expect(screen.getByRole("button", { name: "Go" })).toHaveClass(cls);
  });

  it("uses the primary hover token for the default variant", () => {
    render(<Button>Go</Button>);
    expect(screen.getByRole("button")).toHaveClass("hover:bg-primary-hover", "h-8");
  });

  it.each([
    ["sm", "h-7"],
    ["default", "h-8"],
    ["lg", "h-10"],
    ["icon", "h-8"],
    ["icon-sm", "h-7"],
  ] as const)("size %s has %s", (size, cls) => {
    render(
      <Button size={size} aria-label="x">
        <Plus />
      </Button>,
    );
    expect(screen.getByRole("button")).toHaveClass(cls);
  });

  it("icon-sm is 28x28", () => {
    render(
      <Button size="icon-sm" aria-label="Add">
        <Plus />
      </Button>,
    );
    expect(screen.getByRole("button", { name: "Add" })).toHaveClass("h-7", "w-7");
  });

  it("loading disables the button, sets aria-busy and shows a spinner first", () => {
    render(<Button loading>Rotating…</Button>);
    const btn = screen.getByRole("button", { name: "Rotating…" });
    expect(btn).toBeDisabled();
    expect(btn).toHaveAttribute("aria-busy", "true");
    const first = btn.firstElementChild;
    expect(first?.tagName.toLowerCase()).toBe("svg");
    expect(first).toHaveClass("animate-spin");
  });

  it("is not busy when not loading", () => {
    render(<Button>Save</Button>);
    const btn = screen.getByRole("button", { name: "Save" });
    expect(btn).not.toBeDisabled();
    expect(btn).not.toHaveAttribute("aria-busy");
    expect(btn.querySelector(".animate-spin")).toBeNull();
  });

  it("warns for an icon-only button without an aria-label", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    render(
      <Button size="icon">
        <Plus />
      </Button>,
    );
    expect(warn).toHaveBeenCalledWith("Button: icon-only button needs an aria-label");
  });

  it("warns for an unlabeled icon-sm button", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    render(
      <Button size="icon-sm">
        <Plus />
      </Button>,
    );
    expect(warn).toHaveBeenCalledWith("Button: icon-only button needs an aria-label");
  });

  it("does not warn for a labeled icon button", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    render(
      <>
        <Button size="icon" aria-label="Add">
          <Plus />
        </Button>
        <Button size="icon-sm" title="Remove">
          <Plus />
        </Button>
      </>,
    );
    expect(warn).not.toHaveBeenCalledWith("Button: icon-only button needs an aria-label");
  });
});
