import { useEffect, useState } from "react";
import { PageHeader } from "@/components/PageHeader";
import { StatusBadge } from "@/components/StatusBadge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Wifi, Server, Rocket, Disc } from "lucide-react";
import {
  listDeployments,
  finalizeDeployment,
  type Deployment,
  type DeployProgress,
  type EjectState,
} from "@/api/deployments";
import { useUIWebSocket } from "@/hooks/useUIWebSocket";
import { toast } from "@/hooks/useToast";

// ejectBadge maps a deployment's EjectState to a badge. "" (not armed) renders
// nothing — only deployments in the eject lifecycle get a badge.
function ejectBadge(state: EjectState | undefined): {
  label: string;
  variant: "default" | "secondary" | "destructive" | "outline";
} | null {
  switch (state) {
    case "pending":
      return { label: "Eject pending", variant: "secondary" };
    case "ejecting":
      return { label: "Ejecting…", variant: "outline" };
    case "ejected":
      return { label: "Ejected", variant: "default" };
    case "eject-failed":
      return { label: "Eject failed", variant: "destructive" };
    default:
      return null;
  }
}

// canFinalize reports whether the "Finalize" action should be offered: a Redfish
// deployment with a BMC target that is not already mid-eject. It is offered even
// for deployments whose policy was off, since manual finalize is an operator
// override.
function canFinalize(d: Deployment): boolean {
  return (
    d.method.toLowerCase() === "redfish" &&
    !!d.bmcTargetId &&
    d.ejectState !== "ejecting"
  );
}

export function Deployments() {
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [finalizing, setFinalizing] = useState<string | null>(null);

  function fetchDeployments() {
    listDeployments()
      .then(setDeployments)
      .catch(() => {})
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    fetchDeployments();
  }, []);

  // Live updates: apply deploy-progress events streamed over the UI WebSocket so
  // the table reflects each step in real time. This is the primary path; the
  // poll below is a fallback for clients that miss an event (e.g. mid-reconnect).
  useUIWebSocket((msg) => {
    if (msg.type !== "deploy-progress") return;
    const p = msg.data as DeployProgress;
    setDeployments((prev) =>
      prev.map((d) =>
        d.id === p.deploymentId
          ? { ...d, status: p.status, progress: p.progress, message: p.message }
          : d
      )
    );
  });

  // Auto-poll every 15s as a fallback while a deployment is active. Live
  // WebSocket events drive most updates; this catches anything the socket missed.
  useEffect(() => {
    const hasActive = deployments.some(
      (d) => d.status.toLowerCase() === "active" || d.status.toLowerCase() === "running"
    );
    if (!hasActive) return;
    const interval = setInterval(fetchDeployments, 15000);
    return () => clearInterval(interval);
  }, [deployments]);

  async function handleFinalize(d: Deployment) {
    // One finalize at a time: a second click while a request is in flight would
    // overwrite the tracked id, re-enabling the first button mid-request.
    if (finalizing) return;
    setFinalizing(d.id);
    try {
      const updated = await finalizeDeployment(d.id);
      setDeployments((prev) =>
        prev.map((x) => (x.id === updated.id ? updated : x))
      );
      if (updated.ejectState === "eject-failed") {
        toast(
          `Eject failed: ${updated.ejectError || "unknown error"}`,
          "error"
        );
      } else {
        toast("Media ejected; node will boot from disk", "success");
      }
    } catch (err) {
      toast(`Finalize failed: ${(err as Error).message}`, "error");
    } finally {
      setFinalizing(null);
    }
  }

  function methodIcon(method: string) {
    if (method.toLowerCase() === "pxe" || method.toLowerCase() === "netboot") {
      return <Wifi className="h-4 w-4 text-primary" />;
    }
    return <Server className="h-4 w-4 text-primary" />;
  }

  const filtered = deployments.filter(
    (d) => !search || d.artifactId.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div>
      <PageHeader
        title="Deployments"
        description="Track artifact deployments to bare-metal nodes"
      />

      <div className="flex items-center gap-4 mb-4">
        <Input placeholder="Search by artifact ID..." value={search} onChange={e => setSearch(e.target.value)} className="max-w-sm" />
      </div>

      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="flex items-center justify-center py-16 text-muted-foreground">
              Loading...
            </div>
          ) : filtered.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
              {search ? (
                <>
                  <Server className="h-10 w-10 mb-3 opacity-40" />
                  <p>No matching deployments</p>
                  <p className="text-xs mt-1">Try adjusting your search query.</p>
                </>
              ) : (
                <>
                  <Rocket className="h-12 w-12 mb-3 text-muted-foreground/30" />
                  <div className="text-center">
                    <p className="font-medium text-foreground">No deployments yet</p>
                    <p className="text-sm mt-1">
                      Deploy an artifact to hardware from the artifact detail page.
                    </p>
                  </div>
                </>
              )}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-12">Method</TableHead>
                  <TableHead>Artifact</TableHead>
                  <TableHead>Target</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Progress</TableHead>
                  <TableHead>Started</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((d) => (
                  <TableRow key={d.id}>
                    <TableCell>{methodIcon(d.method)}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {d.artifactId.slice(0, 12)}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {d.bmcTargetId ? d.bmcTargetId.slice(0, 12) : "-"}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-col items-start gap-1">
                        <StatusBadge status={d.status} />
                        {(() => {
                          const eb = ejectBadge(d.ejectState);
                          return eb ? (
                            <Badge variant={eb.variant} title={d.ejectError || undefined}>
                              {eb.label}
                            </Badge>
                          ) : null;
                        })()}
                      </div>
                    </TableCell>
                    <TableCell>
                      {d.progress > 0 ? (
                        <div className="space-y-1">
                          <div className="flex items-center gap-2">
                            <div className="h-2 w-24 rounded-full bg-secondary overflow-hidden">
                              <div
                                className="h-full rounded-full bg-primary transition-all"
                                style={{ width: `${Math.min(d.progress, 100)}%` }}
                              />
                            </div>
                            <span className="text-xs text-muted-foreground">
                              {d.progress}%
                            </span>
                          </div>
                          {d.message && (
                            <span className="text-xs text-muted-foreground block truncate max-w-[16rem]">
                              {d.message}
                            </span>
                          )}
                        </div>
                      ) : (
                        <span className="text-xs text-muted-foreground">-</span>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {d.startedAt
                        ? new Date(d.startedAt).toLocaleString()
                        : "-"}
                    </TableCell>
                    <TableCell className="text-right">
                      {canFinalize(d) ? (
                        <Button
                          variant="outline"
                          size="sm"
                          // Disable ALL finalize buttons while one is in flight
                          // (not just the active row) so a click can't silently
                          // no-op against the in-progress guard.
                          disabled={finalizing !== null}
                          onClick={() => handleFinalize(d)}
                          title="Eject the virtual media and boot from disk"
                        >
                          <Disc className="h-3.5 w-3.5 mr-1" />
                          {finalizing === d.id ? "Finalizing…" : "Finalize"}
                        </Button>
                      ) : (
                        <span className="text-xs text-muted-foreground">-</span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
