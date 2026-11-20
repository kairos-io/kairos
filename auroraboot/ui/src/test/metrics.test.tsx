import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, renderHook, act } from "@testing-library/react";

import type { NodeMetrics } from "@/api/metrics";
import {
  cpuPercent,
  diskPercent,
  formatUptime,
  maxDiskPercent,
  memPercent,
  usageTone,
} from "@/lib/metrics";
import { useNodeMetrics } from "@/hooks/useMetrics";
import { Gauge } from "@/components/fleet/Gauge";
import { Sparkline } from "@/components/fleet/Sparkline";
import { MeterBar } from "@/components/fleet/MeterBar";

const sample: NodeMetrics = {
  sampledAt: "2026-09-29T09:41:07Z",
  uptimeSeconds: 3895200,
  load: [13.9, 12.1, 10.4],
  cpu: { usedPercent: 87.2 },
  memory: { totalBytes: 1000, availableBytes: 214 },
  disks: [
    { label: "COS_OEM", mount: "/oem", totalBytes: 1000, usedBytes: 100 },
    { label: "COS_PERSISTENT", mount: "/usr/local", totalBytes: 1000, usedBytes: 658 },
  ],
  temperatureC: 71,
};

describe("metrics helpers", () => {
  it("computes rounded percentages", () => {
    expect(cpuPercent(sample)).toBe(87);
    expect(memPercent(sample)).toBe(79);
    expect(diskPercent(sample.disks![1])).toBe(66);
    expect(maxDiskPercent(sample)).toBe(66);
  });

  it("returns null when a field is missing", () => {
    const empty: NodeMetrics = { sampledAt: "2026-09-29T09:41:07Z" };
    expect(cpuPercent(empty)).toBeNull();
    expect(memPercent(empty)).toBeNull();
    expect(maxDiskPercent(empty)).toBeNull();
    expect(memPercent({ ...empty, memory: { totalBytes: 0, availableBytes: 0 } })).toBeNull();
    expect(diskPercent({ mount: "/", totalBytes: 0, usedBytes: 0 })).toBe(0);
  });

  it("formats uptime", () => {
    expect(formatUptime(45 * 86400 + 2 * 3600 + 5 * 60)).toBe("45d 2h");
    expect(formatUptime(3 * 3600 + 12 * 60 + 9)).toBe("3h 12m");
    expect(formatUptime(4 * 60 + 30)).toBe("4m");
  });

  it("picks a tone by threshold", () => {
    expect(usageTone(79)).toBe("success");
    expect(usageTone(80)).toBe("warning");
    expect(usageTone(89)).toBe("warning");
    expect(usageTone(90)).toBe("danger");
  });
});

describe("metric components", () => {
  it("labels the gauge", () => {
    render(<Gauge value={87} label="CPU" />);
    expect(screen.getByRole("img", { name: "CPU 87%" })).toBeInTheDocument();
  });

  it("renders nothing for a sparkline with one point", () => {
    const { container } = render(<Sparkline values={[40]} tone="success" />);
    expect(container.querySelector("svg")).toBeNull();
  });

  it("renders a sparkline path for two points", () => {
    const { container } = render(<Sparkline values={[40, 60]} tone="success" />);
    expect(container.querySelectorAll("path").length).toBeGreaterThan(0);
  });

  it("shows a dash for a missing meter value", () => {
    render(<MeterBar value={null} />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("shows the meter percentage", () => {
    render(<MeterBar value={42} />);
    expect(screen.getByText("42%")).toBeInTheDocument();
  });
});

describe("useNodeMetrics", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.useFakeTimers();
    fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ latest: sample, samples: [sample] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("fetches on mount and again after the interval", async () => {
    const { result, unmount } = renderHook(() => useNodeMetrics("n1", 10000));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(String(fetchMock.mock.calls[0][0])).toBe("/api/v1/nodes/n1/metrics");
    expect(result.current.loading).toBe(false);
    expect(result.current.latest?.cpu?.usedPercent).toBe(87.2);
    expect(result.current.samples).toHaveLength(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);

    unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30000);
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("keeps the last data when a fetch fails", async () => {
    const { result } = renderHook(() => useNodeMetrics("n1", 10000));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    fetchMock.mockImplementation(async () => new Response("boom", { status: 500 }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(result.current.latest?.cpu?.usedPercent).toBe(87.2);
  });

  it("clears the old node's data when the id changes", async () => {
    const { result, rerender } = renderHook(({ id }) => useNodeMetrics(id, 10000), {
      initialProps: { id: "n1" },
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.latest?.cpu?.usedPercent).toBe(87.2);

    // The next fetch never resolves, so what we see is the state before it.
    fetchMock.mockImplementation(() => new Promise<Response>(() => {}));
    rerender({ id: "n2" });
    expect(result.current.latest).toBeNull();
    expect(result.current.samples).toEqual([]);
    expect(result.current.loading).toBe(true);
    expect(String(fetchMock.mock.calls.at(-1)?.[0])).toBe("/api/v1/nodes/n2/metrics");
  });
});
