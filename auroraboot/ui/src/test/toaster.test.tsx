import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { Toaster } from "@/components/ui/toaster";
import { toast } from "@/hooks/useToast";

describe("Toaster", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("hides a toast after three seconds", () => {
    render(<Toaster />);
    act(() => toast("saved", "success"));
    expect(screen.getByText("saved")).toBeInTheDocument();

    act(() => vi.advanceTimersByTime(3000));
    expect(screen.queryByText("saved")).not.toBeInTheDocument();
  });

  it("leaves no timer behind when it unmounts with a toast showing", () => {
    const { unmount } = render(<Toaster />);
    act(() => toast("saved", "success"));
    expect(vi.getTimerCount()).toBe(1);

    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
