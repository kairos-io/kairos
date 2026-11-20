import { Activity } from "lucide-react";
import type { NodeMetrics } from "@/api/metrics";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Gauge } from "@/components/fleet/Gauge";
import { Sparkline } from "@/components/fleet/Sparkline";
import { MeterBar } from "@/components/fleet/MeterBar";
import { cpuPercent, diskPercent, formatUptime, memPercent, usageTone } from "@/lib/metrics";
import { formatBytes, timeAgo } from "@/lib/time";

interface ResourcesCardProps {
  latest: NodeMetrics;
  samples: NodeMetrics[];
}

function series(samples: NodeMetrics[], pick: (m: NodeMetrics) => number | null): number[] {
  return samples.map(pick).filter((v): v is number => v !== null);
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-semibold tabular-nums">{value}</dd>
    </div>
  );
}

// ResourcesCard shows the live usage an agent reports: CPU and memory gauges
// with their recent history, a bar per disk, and load, uptime and temperature.
export function ResourcesCard({ latest, samples }: ResourcesCardProps) {
  const cpu = cpuPercent(latest);
  const mem = memPercent(latest);
  const disks = latest.disks ?? [];
  const load = latest.load ?? [];

  return (
    <Card role="region" aria-labelledby="resources-title" className="mb-6">
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle id="resources-title" className="flex items-center gap-2 text-sm font-medium">
          <Activity className="h-4 w-4" aria-hidden="true" />
          Resources
        </CardTitle>
        <span className="text-xs text-muted-foreground">Live · sampled {timeAgo(latest.sampledAt)}</span>
      </CardHeader>
      <CardContent>
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          {cpu !== null && (
            <div className="rounded-md border p-3">
              <p className="mb-2 text-xs font-semibold text-muted-foreground">CPU</p>
              <div className="flex items-center gap-3">
                <Gauge value={cpu} label="CPU" />
                {load.length > 0 && (
                  <p className="text-xs text-muted-foreground">
                    Load <b className="tabular-nums text-foreground">{load[0].toFixed(1)}</b>
                    {load.slice(1).map((l) => ` / ${l.toFixed(1)}`).join("")}
                  </p>
                )}
              </div>
              <Sparkline className="mt-2" values={series(samples, cpuPercent)} tone={usageTone(cpu)} />
            </div>
          )}
          {mem !== null && latest.memory && (
            <div className="rounded-md border p-3">
              <p className="mb-2 text-xs font-semibold text-muted-foreground">Memory</p>
              <div className="flex items-center gap-3">
                <Gauge value={mem} label="Memory" sub={`of ${formatBytes(latest.memory.totalBytes)}`} />
                <div className="grid gap-0.5 text-xs text-muted-foreground">
                  <span>
                    <b className="tabular-nums text-foreground">
                      {formatBytes(latest.memory.totalBytes - latest.memory.availableBytes)}
                    </b>{" "}
                    used
                  </span>
                  <span>{formatBytes(latest.memory.availableBytes)} available</span>
                </div>
              </div>
              <Sparkline className="mt-2" values={series(samples, memPercent)} tone={usageTone(mem)} />
            </div>
          )}
          {disks.length > 0 && (
            <div className="rounded-md border p-3">
              <p className="mb-2 text-xs font-semibold text-muted-foreground">Disk</p>
              <ul className="grid gap-3">
                {disks.map((d) => (
                  <li key={d.mount} className="grid gap-1 text-xs">
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate font-medium" title={d.mount}>
                        {d.label || d.mount}
                      </span>
                      <span className="text-muted-foreground tabular-nums">
                        {formatBytes(d.usedBytes)} / {formatBytes(d.totalBytes)}
                      </span>
                    </div>
                    <MeterBar value={diskPercent(d)} className="[&>div:first-child]:flex-1" />
                  </li>
                ))}
              </ul>
            </div>
          )}
          <div className="rounded-md border p-3">
            <p className="mb-2 text-xs font-semibold text-muted-foreground">System</p>
            <dl className="grid gap-2 text-xs">
              <Fact
                label="Temperature"
                value={latest.temperatureC != null ? `${Math.round(latest.temperatureC)} °C` : "—"}
              />
              <Fact
                label="Uptime"
                value={latest.uptimeSeconds != null ? formatUptime(latest.uptimeSeconds) : "—"}
              />
              <Fact label="Load (1m)" value={load.length > 0 ? load[0].toFixed(1) : "—"} />
            </dl>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
