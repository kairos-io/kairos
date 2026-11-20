import { memo, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { StatusBadge } from "@/components/StatusBadge";
import { Wifi, Server, Loader2, Cpu, MemoryStick, AlertTriangle, Search } from "lucide-react";
import {
  type BMCTarget,
  type NetbootStatus,
  type InspectResult,
  listBMCTargets,
  createBMCTarget,
  inspectHardware,
  deployRedfish,
  getNetbootStatus,
  startNetboot,
  stopNetboot,
  getNetbootLogs,
} from "@/api/deployments";
import { type QuirkProfile, listQuirkProfiles } from "@/api/redfish";
import { useUIWebSocket } from "@/hooks/useUIWebSocket";
import { ansiToHtml } from "@/lib/ansi";

// Memoized per-line renderer, same reasoning as ArtifactDetail's build-log
// LogLine: a live netboot session appends one chunk at a time, and without
// memo every chunk would re-run ansiToHtml over every prior line.
const NetbootLogLine = memo(function NetbootLogLine({ line }: { line: string }) {
  return (
    <div dangerouslySetInnerHTML={{ __html: ansiToHtml(line) || "&nbsp;" }} />
  );
});

// Minimum hardware AuroraBoot wants before deploying. Kept deliberately simple
// and visible: a node below either threshold raises a warning that the operator
// must explicitly override before Deploy is enabled.
const MIN_MEMORY_GIB = 2;
const MIN_CPU_COUNT = 1;

interface DeployDialogProps {
  artifactId: string;
  artifactFiles: string[];
  hasNetboot: boolean;
  // The tab to open on, when that method is available. Falls back to the
  // first available method otherwise.
  defaultMethod?: "pxe" | "redfish";
  onClose: () => void;
}

// Shown on a disabled method tab: why it is unavailable and how to get it.
const NO_NETBOOT_REASON = "This artifact has no Netboot output. Clone it and enable Netboot.";
const NO_ISO_REASON = "This artifact has no ISO output. Clone it and enable ISO.";

export function DeployDialog({
  artifactId,
  artifactFiles,
  hasNetboot,
  defaultMethod,
  onClose,
}: DeployDialogProps) {
  const hasIso = artifactFiles.some((f) => f.endsWith(".iso"));
  const available = { pxe: hasNetboot, redfish: hasIso };
  const defaultTab =
    defaultMethod && available[defaultMethod] ? defaultMethod : hasNetboot || !hasIso ? "pxe" : "redfish";
  const methodNames = [hasNetboot && "PXE boot", hasIso && "RedFish BMC"].filter(Boolean);
  const description = methodNames.length
    ? `Deploy this artifact to bare-metal nodes via ${methodNames.join(" or ")}.`
    : "This artifact has no output that can be deployed. Clone it and enable Netboot or ISO.";

  // PXE state
  const [netbootStatus, setNetbootStatus] = useState<NetbootStatus | null>(null);
  const [pxeLoading, setPxeLoading] = useState(false);
  const [pxeError, setPxeError] = useState("");
  const [netbootLogs, setNetbootLogs] = useState("");
  const logPaneRef = useRef<HTMLDivElement | null>(null);
  // Bumped whenever a new netboot session starts, so a getNetbootLogs()
  // response from before Start (or from a stale reconnect resync) can be
  // told apart from the current session and dropped instead of overwriting
  // a freshly-cleared pane (kairos-io/AuroraBoot#806 review).
  const netbootEpochRef = useRef(0);
  const netbootStatusRef = useRef<NetbootStatus | null>(null);

  // RedFish state
  const [bmcTargets, setBmcTargets] = useState<BMCTarget[]>([]);
  const [selectedTarget, setSelectedTarget] = useState<string>("");
  const [deploying, setDeploying] = useState(false);
  const [deployError, setDeployError] = useState("");
  const [deploySuccess, setDeploySuccess] = useState(false);

  // Pre-flight inspection state (saved targets only — there is no by-credentials
  // inspect endpoint).
  const [inspecting, setInspecting] = useState(false);
  const [inspectResult, setInspectResult] = useState<InspectResult | null>(null);
  const [inspectError, setInspectError] = useState("");
  const [overrideWarning, setOverrideWarning] = useState(false);

  // New BMC target form
  const [showNewTarget, setShowNewTarget] = useState(false);
  const [newTarget, setNewTarget] = useState({
    name: "",
    endpoint: "",
    vendor: "generic",
    username: "",
    password: "",
    verifySSL: false,
    systemId: "",
  });
  const [creatingTarget, setCreatingTarget] = useState(false);

  // Quirk profiles loaded by the server (built-in + operator), for the vendor
  // selector — same source as the BMC Registration page. Until the fetch
  // resolves (or if it fails), fall back to the spec-default generic profile so
  // the selector is never empty/unusable: generic always exists server-side.
  const [profiles, setProfiles] = useState<QuirkProfile[]>([]);
  const vendorOptions: QuirkProfile[] = profiles.length
    ? profiles
    : [
        {
          name: "generic",
          tier: "A",
          tierDescription: "tier A: core-tested",
          origin: "builtin",
        },
      ];

  useEffect(() => {
    if (hasNetboot) {
      getNetbootStatus().then(setNetbootStatus).catch(() => {});
      const epoch = netbootEpochRef.current;
      getNetbootLogs()
        .then((text) => {
          if (netbootEpochRef.current === epoch) setNetbootLogs(text);
        })
        .catch(() => {});
    }
    if (hasIso) {
      listBMCTargets().then(setBmcTargets).catch(() => {});
      listQuirkProfiles().then(setProfiles).catch(() => {});
    }
  }, [hasNetboot, hasIso]);

  useEffect(() => {
    netbootStatusRef.current = netbootStatus;
  });

  // Pre-split once per update so ansiToHtml runs per-line (SGR color state
  // from one line must not bleed into the next) instead of over the whole
  // buffer.
  const netbootLogLines = useMemo(
    () => (netbootLogs ? netbootLogs.split("\n") : []),
    [netbootLogs],
  );

  // Live PXE server output (kairos-io/kairos#4596): the snapshot fetch above
  // gets you caught up, this keeps you live while the dialog is open. There
  // is at most one netboot session at a time, so every chunk belongs to the
  // session currently shown here — no id to filter on.
  const { connected: wsConnected } = useUIWebSocket((msg) => {
    if (msg.type !== "netboot-log" || !hasNetboot) return;
    const data = msg.data as { chunk?: string };
    if (!data.chunk) return;
    setNetbootLogs((prev) => prev + data.chunk);
  });

  // Re-sync the log snapshot once per WebSocket (re)connection, the same
  // pattern ArtifactDetail uses for build logs: a drop that misses live
  // chunks would otherwise leave the pane silently incomplete. Guarded by
  // the same epoch as the initial fetch, so a resync racing a fresh Start
  // can't overwrite it either.
  const lastWsConnected = useRef(false);
  useEffect(() => {
    const justConnected = wsConnected && !lastWsConnected.current;
    lastWsConnected.current = wsConnected;
    if (!justConnected || !hasNetboot || !netbootStatusRef.current?.running) return;
    const epoch = netbootEpochRef.current;
    getNetbootLogs()
      .then((text) => {
        if (netbootEpochRef.current === epoch) setNetbootLogs(text);
      })
      .catch(() => {});
  }, [wsConnected, hasNetboot]);

  // Auto-scroll the log pane to the newest line as chunks arrive.
  useEffect(() => {
    const el = logPaneRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [netbootLogs]);

  // Poll netboot status while running
  useEffect(() => {
    if (!hasNetboot || !netbootStatus?.running) return;
    const interval = setInterval(() => {
      getNetbootStatus().then(setNetbootStatus).catch(() => {});
    }, 3000);
    return () => clearInterval(interval);
  }, [hasNetboot, netbootStatus?.running]);

  // Clear any prior inspection when the operator picks a different target.
  function chooseTarget(id: string) {
    setSelectedTarget(id);
    setInspectResult(null);
    setInspectError("");
    setOverrideWarning(false);
  }

  // belowMinimum reports whether an inspected node is under either threshold.
  const belowMinimum =
    inspectResult !== null &&
    (inspectResult.memoryGiB < MIN_MEMORY_GIB ||
      inspectResult.processorCount < MIN_CPU_COUNT);

  // deployBlocked gates the Deploy button: a node that failed pre-flight must be
  // explicitly overridden before it can be deployed.
  const deployBlocked = belowMinimum && !overrideWarning;

  async function handleInspect() {
    if (!selectedTarget) return;
    setInspecting(true);
    setInspectError("");
    setInspectResult(null);
    setOverrideWarning(false);
    try {
      const result = await inspectHardware(selectedTarget);
      setInspectResult(result);
    } catch (err) {
      setInspectError(err instanceof Error ? err.message : "Inspection failed");
    } finally {
      setInspecting(false);
    }
  }

  async function handlePxeToggle() {
    setPxeLoading(true);
    setPxeError("");
    const action = netbootStatus?.running ? "stop" : "start";
    try {
      if (netbootStatus?.running) {
        await stopNetboot();
      } else {
        // A fresh session gets a fresh pane: the server resets its own log
        // buffer on Start, so stale text from a previous run must not linger.
        // Bump the epoch first so any in-flight getNetbootLogs() response
        // from before this Start is recognized as stale and dropped.
        netbootEpochRef.current += 1;
        setNetbootLogs("");
        await startNetboot(artifactId);
      }
      const status = await getNetbootStatus();
      setNetbootStatus(status);
    } catch (err) {
      const reason = err instanceof Error ? err.message : "request failed";
      setPxeError(`Could not ${action} netboot: ${reason}`);
      // The server may be in another state than the dialog shows, as when
      // another session already started it, so the badge and button follow
      // the server.
      getNetbootStatus().then(setNetbootStatus).catch(() => {});
    } finally {
      setPxeLoading(false);
    }
  }

  async function handleRedfishDeploy() {
    setDeploying(true);
    setDeployError("");
    setDeploySuccess(false);
    try {
      await deployRedfish(artifactId, { bmcTargetId: selectedTarget });
      setDeploySuccess(true);
    } catch (err) {
      setDeployError(err instanceof Error ? err.message : "Deploy failed");
    } finally {
      setDeploying(false);
    }
  }

  async function handleCreateTarget(e: React.FormEvent) {
    e.preventDefault();
    setCreatingTarget(true);
    try {
      const created = await createBMCTarget(newTarget);
      setBmcTargets((prev) => [...prev, created]);
      chooseTarget(created.id);
      setShowNewTarget(false);
      setNewTarget({ name: "", endpoint: "", vendor: "generic", username: "", password: "", verifySSL: false, systemId: "" });
    } catch {
      // ignore
    } finally {
      setCreatingTarget(false);
    }
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Deploy Artifact</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        <Tabs defaultValue={defaultTab}>
          <TabsList className="w-full">
            {/* Both methods are always listed. A disabled trigger has no
                pointer events, so the reason sits on a wrapper to keep the
                tooltip working on hover. */}
            <span className="flex flex-1" title={hasNetboot ? undefined : NO_NETBOOT_REASON}>
              <TabsTrigger value="pxe" className="flex-1 gap-2" disabled={!hasNetboot}>
                <Wifi className="h-4 w-4" /> PXE Boot
              </TabsTrigger>
            </span>
            <span className="flex flex-1" title={hasIso ? undefined : NO_ISO_REASON}>
              <TabsTrigger value="redfish" className="flex-1 gap-2" disabled={!hasIso}>
                <Server className="h-4 w-4" /> RedFish
              </TabsTrigger>
            </span>
          </TabsList>

          {hasNetboot && (
            <TabsContent value="pxe" className="space-y-4">
              <div className="rounded-md border p-4 space-y-3">
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">Netboot Server</span>
                  <StatusBadge status={netbootStatus?.running ? "Running" : "Stopped"} />
                </div>
                {netbootStatus?.running && (
                  <div className="text-xs text-muted-foreground space-y-1">
                    <p>Address: <span className="font-mono">{netbootStatus.advertisedAddress || netbootStatus.address}:{netbootStatus.port}</span></p>
                    <p>Artifact: <span className="font-mono">{netbootStatus.artifactId.slice(0, 12)}</span></p>
                  </div>
                )}
                <Button
                  className="w-full"
                  variant={netbootStatus?.running ? "destructive" : "default"}
                  onClick={handlePxeToggle}
                  disabled={pxeLoading}
                >
                  {pxeLoading && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  {netbootStatus?.running ? "Stop Netboot" : "Start Netboot"}
                </Button>
                {pxeError && (
                  <div className="bg-red-500/10 border border-red-500/25 text-red-700 rounded-md p-3 text-sm">
                    {pxeError}
                  </div>
                )}
              </div>

              {/* Live PXE server log, so a stalled/failed boot is debuggable
                  instead of just a status badge (kairos-io/kairos#4596). Shown
                  once a session has produced any output, and kept visible
                  after Stop so a failure can still be read back. */}
              {netbootLogs && (
                <div className="rounded-md border">
                  <div className="flex items-center justify-between px-3 py-2 border-b">
                    <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                      Netboot Log
                    </span>
                    {netbootStatus?.running && (
                      <span className="text-xs text-muted-foreground">live</span>
                    )}
                  </div>
                  <div
                    ref={logPaneRef}
                    className="text-xs font-mono bg-muted/50 rounded-b-md p-3 max-h-64 overflow-y-auto overflow-x-auto whitespace-pre-wrap"
                  >
                    {netbootLogLines.map((line, i) => (
                      <NetbootLogLine key={i} line={line} />
                    ))}
                  </div>
                </div>
              )}
            </TabsContent>
          )}

          {hasIso && (
            <TabsContent value="redfish" className="space-y-4">
              {/* Target selector */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label>BMC Target</Label>
                  <Link
                    to="/bmc"
                    className="text-xs text-primary hover:underline"
                    onClick={onClose}
                  >
                    Manage BMCs →
                  </Link>
                </div>
                <Select value={selectedTarget} onValueChange={chooseTarget}>
                  <SelectTrigger>
                    <SelectValue placeholder="Select a BMC target..." />
                  </SelectTrigger>
                  <SelectContent>
                    {bmcTargets.map((t) => (
                      <SelectItem key={t.id} value={t.id}>
                        {t.name} ({t.endpoint})
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <div className="flex items-center justify-between">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-xs text-primary"
                    onClick={() => setShowNewTarget(!showNewTarget)}
                  >
                    {showNewTarget ? "Cancel" : "+ Add new target"}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    className="text-xs gap-1.5"
                    onClick={handleInspect}
                    disabled={!selectedTarget || inspecting}
                  >
                    {inspecting ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <Search className="h-3.5 w-3.5" />
                    )}
                    Pre-flight inspect
                  </Button>
                </div>
              </div>

              {/* Pre-flight: inspection error */}
              {inspectError && (
                <div className="bg-red-500/10 border border-red-500/25 text-red-700 rounded-md p-3 text-sm">
                  {inspectError}
                </div>
              )}

              {/* Pre-flight: inspection result */}
              {inspectResult && (
                <div className="rounded-md border p-4 space-y-3">
                  <div className="text-sm font-medium">Hardware pre-flight</div>
                  <div className="grid grid-cols-2 gap-2 text-xs">
                    <div className="text-muted-foreground">Model</div>
                    <div className="font-mono">{inspectResult.model || "-"}</div>
                    <div className="text-muted-foreground">Manufacturer</div>
                    <div className="font-mono">{inspectResult.manufacturer || "-"}</div>
                    <div className="text-muted-foreground">Serial</div>
                    <div className="font-mono">{inspectResult.serialNumber || "-"}</div>
                    <div className="text-muted-foreground flex items-center gap-1">
                      <MemoryStick className="h-3.5 w-3.5" /> Memory
                    </div>
                    <div
                      className={
                        inspectResult.memoryGiB < MIN_MEMORY_GIB
                          ? "font-mono text-red-600 font-semibold"
                          : "font-mono"
                      }
                    >
                      {inspectResult.memoryGiB} GiB
                    </div>
                    <div className="text-muted-foreground flex items-center gap-1">
                      <Cpu className="h-3.5 w-3.5" /> Processors
                    </div>
                    <div
                      className={
                        inspectResult.processorCount < MIN_CPU_COUNT
                          ? "font-mono text-red-600 font-semibold"
                          : "font-mono"
                      }
                    >
                      {inspectResult.processorCount}
                    </div>
                  </div>
                  {inspectResult.supportedFeatures.length > 0 && (
                    <div className="space-y-1">
                      <div className="text-xs text-muted-foreground">Supported features</div>
                      <div className="flex flex-wrap gap-1">
                        {inspectResult.supportedFeatures.map((f) => (
                          <span
                            key={f}
                            className="rounded bg-secondary px-1.5 py-0.5 text-xs font-mono"
                          >
                            {f}
                          </span>
                        ))}
                      </div>
                    </div>
                  )}
                  {belowMinimum && (
                    <div className="bg-amber-500/10 border border-amber-500/30 text-amber-800 rounded-md p-3 text-xs space-y-2">
                      <div className="flex items-start gap-2">
                        <AlertTriangle className="h-4 w-4 mt-0.5 shrink-0" />
                        <span>
                          This node is below the recommended minimum (
                          {MIN_MEMORY_GIB} GiB memory, {MIN_CPU_COUNT} CPU). Deploy
                          may fail or the installed system may be unusable.
                        </span>
                      </div>
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={overrideWarning}
                          onChange={(e) => setOverrideWarning(e.target.checked)}
                        />
                        Deploy anyway (override)
                      </label>
                    </div>
                  )}
                </div>
              )}

              {/* New target form */}
              {showNewTarget && (
                <form onSubmit={handleCreateTarget} className="space-y-3 rounded-md border p-4">
                  <div className="grid grid-cols-2 gap-3">
                    <div className="space-y-1">
                      <Label className="text-xs">Name</Label>
                      <Input
                        value={newTarget.name}
                        onChange={(e) => setNewTarget({ ...newTarget, name: e.target.value })}
                        placeholder="my-server"
                        required
                      />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Endpoint</Label>
                      <Input
                        value={newTarget.endpoint}
                        onChange={(e) => setNewTarget({ ...newTarget, endpoint: e.target.value })}
                        placeholder="https://10.0.0.1"
                        required
                      />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Username</Label>
                      <Input
                        value={newTarget.username}
                        onChange={(e) => setNewTarget({ ...newTarget, username: e.target.value })}
                        required
                      />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Password</Label>
                      <Input
                        type="password"
                        value={newTarget.password}
                        onChange={(e) => setNewTarget({ ...newTarget, password: e.target.value })}
                        required
                      />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Vendor profile</Label>
                      <Select
                        value={newTarget.vendor}
                        onValueChange={(v) => setNewTarget({ ...newTarget, vendor: v })}
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {vendorOptions.map((p) => (
                            <SelectItem key={p.name} value={p.name}>
                              {p.name} (tier {p.tier})
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-1 col-span-2">
                      <Label className="text-xs">System ID</Label>
                      <Input
                        value={newTarget.systemId}
                        onChange={(e) => setNewTarget({ ...newTarget, systemId: e.target.value })}
                        placeholder="optional"
                      />
                      <p className="text-xs text-muted-foreground">
                        Leave blank for single-system BMCs; required when the BMC
                        exposes multiple ComputerSystems.
                      </p>
                    </div>
                  </div>
                  <Button type="submit" size="sm" disabled={creatingTarget} className="w-full">
                    {creatingTarget && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                    Add Target
                  </Button>
                </form>
              )}

              {/* Deploy button */}
              {deployError && (
                <div className="bg-red-500/10 border border-red-500/25 text-red-700 rounded-md p-3 text-sm">
                  {deployError}
                </div>
              )}
              {deploySuccess && (
                <div className="bg-green-500/10 border border-green-500/25 text-green-700 rounded-md p-3 text-sm">
                  Deployment started successfully.
                </div>
              )}
              <Button
                className="w-full"
                onClick={handleRedfishDeploy}
                disabled={!selectedTarget || deploying || deployBlocked}
              >
                {deploying && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                Deploy via RedFish
              </Button>
              <p className="text-xs text-muted-foreground">
                Pre-flight inspection is available for saved BMC targets only.
              </p>
            </TabsContent>
          )}
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}
