import { useEffect, useState, useRef, useCallback, useMemo, memo } from "react";
import { useParams, useNavigate } from "react-router";
import {
  getArtifact,
  getArtifactLogs,
  cancelArtifact,
  deleteArtifact,
  updateArtifact,
  artifactDownloadUrl,
  artifactImageUrl,
  createDownloadLink,
  type Artifact,
} from "@/api/artifacts";
import { StatusBadge } from "@/components/StatusBadge";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Download,
  XCircle,
  Bookmark,
  Pencil,
  Check,
  Copy,
  Link2,
  Rocket,
  FileDown,
  Box,
  Cpu,
  Clock,
  AlertTriangle,
  CheckCircle2,
  MoreHorizontal,
  Terminal as TerminalIcon,
  ArrowDown,
  WrapText,
  Loader2,
  ChevronDown,
  ChevronRight,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SplitButton } from "@/components/SplitButton";
import { BuildSummary, summaryFromArtifact } from "@/components/wizard/BuildSummary";
import { DeployDialog } from "@/components/DeployDialog";
import { ansiToHtml } from "@/lib/ansi";

// LogLine is a memoized single-row renderer for the build-log pane. Long
// builds produce thousands of lines; without memo, every WS chunk that
// appends a new line would re-run ansiToHtml for every existing line and
// hand React a fresh __html string identity, forcing dangerouslySetInnerHTML
// to reconcile the whole list. React.memo's shallow prop compare on the
// `line` string skips unchanged rows entirely, so a live chunk only
// re-renders the tail.
const LogLine = memo(function LogLine({ line }: { line: string }) {
  return (
    <div dangerouslySetInnerHTML={{ __html: ansiToHtml(line) || "&nbsp;" }} />
  );
});

// LogGutterNumber matches LogLine for the same reason: unchanged line
// numbers should not trigger a DOM update on every incoming chunk.
const LogGutterNumber = memo(function LogGutterNumber({ n }: { n: number }) {
  return <div>{n}</div>;
});
import { listGroups, type Group } from "@/api/groups";
import { downloadBuildConfig, payloadFromArtifact } from "@/lib/buildConfig";
import { toast } from "@/hooks/useToast";
import { useUIWebSocket } from "@/hooks/useUIWebSocket";

// formatDuration renders a build duration as a compact, humane string.
// Examples: "47s", "4m 21s", "1h 12m".
function formatDuration(totalSeconds: number): string {
  if (totalSeconds <= 0) return "0s";
  const h = Math.floor(totalSeconds / 3600);
  const m = Math.floor((totalSeconds % 3600) / 60);
  const s = totalSeconds % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

export function ArtifactDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [artifact, setArtifact] = useState<Artifact | null>(null);
  const [groups, setGroups] = useState<Group[]>([]);
  const [logs, setLogs] = useState<string>("");
  const [cancelling, setCancelling] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [editingName, setEditingName] = useState(false);
  const [showDeploy, setShowDeploy] = useState(false);
  const [deployMethod, setDeployMethod] = useState<"pxe" | "redfish" | undefined>(undefined);
  const [nameInput, setNameInput] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const logsContainerRef = useRef<HTMLDivElement>(null);
  const [followLogs, setFollowLogs] = useState(true);
  const [wrapLogs, setWrapLogs] = useState(true);
  const [now, setNow] = useState(() => Date.now());
  // Collapse state for the two big-ish cards on this page. Both start
  // collapsed so the landing view highlights status + downloads; users click
  // the header/chevron to peek at config or the log body. Toolbar buttons on
  // the log card (live indicator, copy, download) stay visible even when the
  // body is collapsed.
  const [configOpen, setConfigOpen] = useState(false);
  const [logsOpen, setLogsOpen] = useState(false);

  const fetchArtifact = useCallback(() => {
    if (!id) return;
    getArtifact(id).then(setArtifact).catch(() => {});
  }, [id]);

  const fetchLogs = useCallback(() => {
    if (!id) return;
    getArtifactLogs(id)
      .then((text) => setLogs(typeof text === "string" ? text : String(text)))
      .catch(() => {});
  }, [id]);

  // Initial fetch
  useEffect(() => {
    fetchArtifact();
    fetchLogs();
    listGroups().then(setGroups).catch(() => {});
  }, [fetchArtifact, fetchLogs]);

  function handleExportConfig() {
    if (!artifact) return;
    const payload = payloadFromArtifact(artifact, groups);
    const filename = downloadBuildConfig(payload);
    toast(`Exported ${filename}`, "success");
  }

  // Auto-poll artifact every 3s while building
  useEffect(() => {
    if (!artifact || (artifact.phase !== "Pending" && artifact.phase !== "Building")) return;
    const interval = setInterval(fetchArtifact, 3000);
    return () => clearInterval(interval);
  }, [artifact, fetchArtifact]);

  // Live log streaming via the shared UI WebSocket. We subscribe once per
  // mount and filter incoming messages by build id. The initial snapshot
  // on mount (fetchLogs in the combined useEffect above) covers history,
  // and a one-shot re-fetch on reconnect covers any chunks that flushed
  // while the socket was down.
  const { connected: wsConnected } = useUIWebSocket((msg) => {
    if (msg.type !== "build-log" || !id) return;
    const payload = msg.data as { id?: string; chunk?: string } | undefined;
    if (!payload || payload.id !== id || !payload.chunk) return;
    setLogs((prev) => prev + payload.chunk);
  });

  // Re-sync the full log body once per WebSocket (re)connection, to cover
  // any chunks that flushed while the socket was down. We intentionally
  // do NOT depend on `artifact` here — otherwise every 3s artifact poll
  // would retrigger this effect and turn it into a log poll. We read the
  // current artifact via a ref instead.
  const artifactRef = useRef<Artifact | null>(null);
  useEffect(() => {
    artifactRef.current = artifact;
  });
  const lastWsConnected = useRef(false);
  useEffect(() => {
    const justConnected = wsConnected && !lastWsConnected.current;
    lastWsConnected.current = wsConnected;
    if (!justConnected) return;
    const a = artifactRef.current;
    if (!a || (a.phase !== "Pending" && a.phase !== "Building")) return;
    fetchLogs();
  }, [wsConnected, fetchLogs]);

  // When the build transitions to a terminal phase, do one last HTTP
  // fetch to pick up any trailing chunks that raced the final flush.
  const lastPhaseRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    const phase = artifact?.phase;
    const prev = lastPhaseRef.current;
    lastPhaseRef.current = phase;
    if (!phase || prev === phase) return;
    if (phase === "Ready" || phase === "Error") {
      fetchLogs();
    }
  }, [artifact?.phase, fetchLogs]);

  // Auto-scroll the log container (not the page) to the bottom when new
  // content arrives and follow-tail is enabled. scrollIntoView() would
  // scroll every ancestor scroll container including <html>, yanking the
  // whole page down past the header every few seconds during a live build.
  useEffect(() => {
    if (!followLogs) return;
    const el = logsContainerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [logs, followLogs]);

  // If the user scrolls the log pane up by hand, stop auto-following so the
  // view doesn't yank them back to the bottom mid-read. Re-enable when they
  // scroll back near the bottom.
  useEffect(() => {
    const el = logsContainerRef.current;
    if (!el) return;
    const onScroll = () => {
      const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
      if (distanceFromBottom > 40) {
        setFollowLogs(false);
      } else if (distanceFromBottom < 10) {
        setFollowLogs(true);
      }
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, []);

  // Tick every second during active builds so the live duration indicator
  // actually moves forward in real time.
  useEffect(() => {
    if (!artifact || (artifact.phase !== "Pending" && artifact.phase !== "Building")) return;
    const interval = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(interval);
  }, [artifact]);

  async function handleCancel() {
    if (!id) return;
    setCancelling(true);
    try {
      await cancelArtifact(id);
      fetchArtifact();
    } finally {
      setCancelling(false);
    }
  }

  async function handleDelete() {
    if (!id) return;
    setDeleting(true);
    try {
      await deleteArtifact(id);
      navigate("/artifacts");
    } finally {
      setDeleting(false);
    }
  }

  // Log statistics for the header toolbar. Memoize so we don't recompute on
  // unrelated state changes during a build (the log string can be large).
  // Declared before the early-return so React's hook order stays stable
  // between the "loading" and "loaded" renders.
  const { logLineCount, logLines } = useMemo(() => {
    if (!logs) return { logLineCount: 0, logLines: [] as string[] };
    const lines = logs.split("\n");
    return { logLineCount: lines.length, logLines: lines };
  }, [logs]);

  if (!artifact) {
    return (
      <div className="flex items-center justify-center py-16 text-muted-foreground">
        Loading...
      </div>
    );
  }

  const isActive = artifact.phase === "Pending" || artifact.phase === "Building";
  const artifactFiles = artifact.artifacts || [];

  function extractFilename(path: string): string {
    const parts = path.split("/");
    return parts[parts.length - 1] || path;
  }

  // Identity line: everything you'd want at a glance — what's being built,
  // from what base, on which hardware target. Empty pieces are simply
  // skipped so dockerfile-mode builds don't render a dangling "null ·".
  const identityParts = [
    artifact.baseImage,
    artifact.kairosVersion,
    [artifact.arch || "amd64", artifact.model || "generic", artifact.variant || "core"]
      .filter(Boolean)
      .join("/"),
  ].filter(Boolean);

  function copyId() {
    navigator.clipboard.writeText(artifact!.id).then(
      () => toast("Copied build ID", "success"),
      () => toast("Copy failed", "error"),
    );
  }

  // Duration string: live-updating while building, frozen once terminal.
  const createdMs = artifact.createdAt ? new Date(artifact.createdAt).getTime() : 0;
  const updatedMs = artifact.updatedAt ? new Date(artifact.updatedAt).getTime() : 0;
  const endMs = isActive ? now : updatedMs;
  const durationSec = createdMs > 0 && endMs > createdMs ? Math.floor((endMs - createdMs) / 1000) : 0;
  const durationText = formatDuration(durationSec);

  function handleCopyLogs() {
    if (!logs) return;
    navigator.clipboard.writeText(logs).then(
      () => toast("Copied build logs", "success"),
      () => toast("Copy failed", "error"),
    );
  }

  function handleDownloadLogs() {
    if (!logs) return;
    const blob = new Blob([logs], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    const slug = (artifact!.name || artifact!.id.slice(0, 8)).trim().toLowerCase().replace(/[^a-z0-9-]+/g, "-");
    a.download = `${slug}.log`;
    a.click();
    URL.revokeObjectURL(url);
  }

  function scrollLogsToBottom() {
    setFollowLogs(true);
    requestAnimationFrame(() => {
      const el = logsContainerRef.current;
      if (el) el.scrollTop = el.scrollHeight;
    });
  }

  async function handleToggleSaved() {
    await updateArtifact(id!, { saved: !artifact!.saved });
    fetchArtifact();
  }

  // copyDownloadLink copies a link to one file that works for an hour without
  // signing in, made absolute on the address this browser reaches the server at.
  async function copyDownloadLink(filename: string) {
    try {
      const link = await createDownloadLink(id!, filename);
      await navigator.clipboard.writeText(new URL(link.path, window.location.origin).href);
      const at = new Date(link.expiresAt).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
      toast(`Link copied, expires in 1 hour (at ${at})`, "success");
    } catch {
      toast("Copy failed", "error");
    }
  }

  function openDeploy(method?: "pxe" | "redfish") {
    setDeployMethod(method);
    setShowDeploy(true);
  }

  // The deploy methods the Deploy dialog offers. A method whose output the
  // artifact lacks stays listed but disabled, with the reason.
  const hasIso = artifactFiles.some((f) => f.endsWith(".iso"));
  const deployMethods = [
    {
      label: "PXE boot (netboot)",
      onSelect: () => openDeploy("pxe"),
      disabled: !artifact.netboot,
      hint: artifact.netboot
        ? undefined
        : "This artifact has no Netboot output. Clone it and enable Netboot.",
    },
    {
      label: "RedFish (virtual media)",
      onSelect: () => openDeploy("redfish"),
      disabled: !hasIso,
      hint: hasIso ? undefined : "This artifact has no ISO output. Clone it and enable ISO.",
    },
  ];

  const targetGroupName = artifact.targetGroupId
    ? groups.find((g) => g.id === artifact.targetGroupId)?.name
    : undefined;

  return (
    <div className="space-y-6">
      {/* Breadcrumb */}
      <div className="flex items-center gap-2 text-sm text-muted-foreground mb-4">
        <button onClick={() => navigate("/artifacts")} className="hover:text-foreground">Artifacts</button>
        <span>/</span>
        <span className="text-foreground">{artifact.name || artifact.id.slice(0, 8)}</span>
      </div>

      {/* Header */}
      <div className="flex flex-wrap items-start gap-4">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            {editingName ? (
              <form
                className="flex items-center gap-2"
                onSubmit={async (e) => {
                  e.preventDefault();
                  await updateArtifact(id!, { name: nameInput });
                  setEditingName(false);
                  fetchArtifact();
                }}
              >
                <Input
                  value={nameInput}
                  onChange={(e) => setNameInput(e.target.value)}
                  className="h-9 text-2xl font-bold w-80"
                  autoFocus
                  placeholder="Artifact name..."
                />
                <Button type="submit" variant="ghost" size="icon" className="h-7 w-7">
                  <Check className="h-4 w-4" />
                </Button>
              </form>
            ) : (
              <>
                <h1 className="text-2xl font-bold truncate leading-tight">
                  {artifact.name || (
                    <span className="font-mono">{artifact.id.slice(0, 12)}</span>
                  )}
                </h1>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 shrink-0 text-muted-foreground hover:text-foreground"
                  title="Rename"
                  onClick={() => {
                    setNameInput(artifact.name || "");
                    setEditingName(true);
                  }}
                >
                  <Pencil className="h-3.5 w-3.5" />
                </Button>
              </>
            )}
          </div>
          {/* Identity subtitle — at-a-glance context for what's being built. */}
          {identityParts.length > 0 && (
            <p className="text-sm text-muted-foreground mt-1 flex items-center gap-2 flex-wrap">
              {identityParts.map((part, i) => (
                <span key={i} className="inline-flex items-center gap-1">
                  {i > 0 && <span className="text-muted-foreground/50">·</span>}
                  <span className="font-mono text-xs bg-muted/60 px-1.5 py-0.5 rounded">{part}</span>
                </span>
              ))}
            </p>
          )}
          {/* Build id + copy button, always present but unobtrusive. */}
          <div className="flex items-center gap-1.5 mt-1.5">
            <span className="text-[11px] text-muted-foreground font-mono">{artifact.id}</span>
            <button
              type="button"
              onClick={copyId}
              className="text-muted-foreground/60 hover:text-foreground transition-colors"
              title="Copy build ID"
            >
              <Copy className="h-3 w-3" />
            </button>
          </div>
        </div>
        {/* Actions: secondary, then the primary, then the ⋯ menu. Delete
            lives in the menu so the header never shows a red button. */}
        <div role="group" aria-label="Page actions" className="flex flex-wrap items-center gap-2">
          {artifact.saved && (
            <span className="inline-flex items-center gap-1 rounded-md border border-primary/30 bg-primary-soft px-2 py-0.5 text-xs font-medium text-primary">
              <Bookmark className="h-3 w-3 fill-current" aria-hidden="true" />
              Saved as template
            </span>
          )}
          <Button
            variant="outline"
            onClick={() => navigate(`/artifacts/new?clone=${artifact.id}`)}
          >
            <Copy aria-hidden="true" />
            Clone
          </Button>
          {isActive && (
            <Button
              variant="destructive-outline"
              onClick={handleCancel}
              loading={cancelling}
            >
              {!cancelling && <XCircle aria-hidden="true" />}
              {cancelling ? "Cancelling..." : "Cancel build"}
            </Button>
          )}
          {artifact.phase === "Ready" && (
            <SplitButton
              label="Deploy"
              icon={Rocket}
              onClick={() => openDeploy()}
              menuLabel="Deploy methods"
              items={deployMethods}
            />
          )}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="icon" aria-label="More actions">
                <MoreHorizontal aria-hidden="true" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={handleToggleSaved}>
                {artifact.saved ? "Remove template" : "Save as template"}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={handleExportConfig}>Export config</DropdownMenuItem>
              {!isActive && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    className="text-danger focus:text-danger"
                    disabled={deleting}
                    onSelect={() => setConfirmOpen(true)}
                  >
                    Delete
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {/* Status band */}
      <div className="flex items-center gap-4 flex-wrap">
        <StatusBadge status={artifact.phase} />
        {artifact.arch && (
          <div className="flex gap-1 flex-wrap">
            <span
              className="inline-flex items-center gap-1 text-xs font-mono px-2 py-0.5 rounded border bg-muted/50"
              title="Architecture"
            >
              <Cpu className="h-3 w-3 text-muted-foreground" />
              {artifact.arch.replace(/^linux\//, "")}
            </span>
          </div>
        )}
        {isActive && (
          <span className="inline-flex items-center gap-1.5 text-sm text-primary">
            <span className="relative flex h-2 w-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-primary opacity-75" />
              <span className="relative inline-flex rounded-full h-2 w-2 bg-primary" />
            </span>
            Building · {durationText}
          </span>
        )}
        {!isActive && durationSec > 0 && (
          <span className="inline-flex items-center gap-1.5 text-sm text-muted-foreground">
            <Clock className="h-3.5 w-3.5" />
            {artifact.phase === "Ready" ? "Built in" : "Ran for"} {durationText}
          </span>
        )}
        <span className="text-sm text-muted-foreground">
          Created {artifact.createdAt ? new Date(artifact.createdAt).toLocaleString() : "-"}
        </span>
      </div>

      {/* Error hero: promoted when a build failed, with the failure reason
          and a one-click "Clone & retry" path so the user isn't left
          staring at a log trace without a next step. */}
      {artifact.phase === "Error" && artifact.message && (
        <div className="rounded-xl border border-red-500/30 bg-red-500/5 p-5 animate-fade-up">
          <div className="flex items-start gap-4">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-red-500/15 text-red-600 dark:text-red-400">
              <AlertTriangle className="h-5 w-5" />
            </div>
            <div className="flex-1 min-w-0">
              <h3 className="font-semibold text-sm text-red-900 dark:text-red-300">
                Build failed
              </h3>
              <p className="mt-1 font-mono text-xs text-red-800/90 dark:text-red-300/90 break-words">
                {artifact.message}
              </p>
              <div className="mt-4 flex flex-wrap gap-2">
                <Button
                  size="sm"
                  onClick={() => navigate(`/artifacts/new?clone=${artifact.id}`)}
                >
                  <Copy className="h-4 w-4 mr-2" />
                  Clone &amp; retry
                </Button>
                <a
                  href="#build-logs"
                  className="inline-flex items-center text-sm text-red-700 dark:text-red-400 hover:underline"
                  onClick={(e) => {
                    e.preventDefault();
                    logsContainerRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
                  }}
                >
                  Jump to logs
                </a>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Success hero: when a build is Ready, elevate the downloadable
          outputs to a prominent card above the configuration. Users come
          here specifically to grab the files; it deserves the top slot. */}
      {artifact.phase === "Ready" && artifactFiles.length > 0 && (
        <Card data-slot="success-banner" className="border-emerald-500/30 overflow-hidden animate-fade-up">
          {/* High-contrast header: solid emerald-50 / emerald-950 pair
              (and emerald-950/80 / emerald-100 for dark mode) so the
              title and subtitle are actually legible against the fill. */}
          <div className="flex items-center justify-between gap-3 border-b border-emerald-500/30 bg-emerald-50 dark:bg-emerald-950/60 px-5 py-3">
            <div className="flex items-center gap-2.5">
              <div className="flex h-8 w-8 items-center justify-center rounded-full bg-emerald-600 text-white">
                <CheckCircle2 className="h-4 w-4" />
              </div>
              <div>
                <h3 className="font-semibold text-sm text-emerald-950 dark:text-emerald-100">
                  Build succeeded
                </h3>
                <p className="text-[11px] text-emerald-900/80 dark:text-emerald-200/80">
                  {artifactFiles.length} file{artifactFiles.length === 1 ? "" : "s"} ready to download
                </p>
              </div>
            </div>
          </div>
          <CardContent className="p-0">
            <ul className="divide-y">
              {artifactFiles.map((filePath) => {
                const filename = extractFilename(filePath);
                return (
                  <li key={filePath} className="flex items-center gap-2 pr-3">
                    <a
                      href={artifactDownloadUrl(id!, filename)}
                      download
                      className="flex min-w-0 flex-1 items-center gap-3 px-5 py-3 transition-colors hover:bg-muted/40 focus:outline-none focus-visible:bg-muted/60"
                    >
                      <Download className="h-4 w-4 text-emerald-600 dark:text-emerald-400 shrink-0" />
                      <span className="flex-1 font-mono text-xs truncate">{filename}</span>
                      <span className="text-[11px] text-muted-foreground">Download</span>
                    </a>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      title={`Copy a link to ${filename} that works for 1 hour without signing in. Scripts should download through the API with the admin password in an Authorization header.`}
                      onClick={() => copyDownloadLink(filename)}
                    >
                      <Link2 aria-hidden="true" />
                      Copy link
                    </Button>
                  </li>
                );
              })}
            </ul>
            {artifact.containerImage && (
              <div className="border-t px-5 py-3 bg-muted/20">
                <p className="text-[11px] uppercase tracking-wide text-muted-foreground mb-1.5">
                  Container image (for upgrades)
                </p>
                <a
                  href={artifactImageUrl(id!)}
                  download
                  className="inline-flex items-center gap-2 text-xs font-mono text-primary hover:underline break-all"
                >
                  <Download className="h-3.5 w-3.5" />
                  {artifact.containerImage}
                  <span className="text-muted-foreground">(docker tar)</span>
                </a>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Configuration — split into Source / Outputs / Provisioning sections
          so readers can scan each concern independently instead of hunting
          through a dense 6-column grid.
          Collapsed by default — the outputs at the top usually answer
          "what came out of this build?"; the config is secondary. */}
      <Card>
        <button
          type="button"
          onClick={() => setConfigOpen((o) => !o)}
          className="w-full flex items-center justify-between px-6 py-3 hover:bg-muted/40 transition-colors"
          aria-expanded={configOpen}
          aria-label={configOpen ? "Collapse configuration" : "Expand configuration"}
        >
          <span className="text-sm font-semibold flex items-center gap-2">
            <Box className="h-4 w-4 text-muted-foreground" />
            Configuration
            {!configOpen && (
              <span className="text-xs font-normal text-muted-foreground">
                — click to expand
              </span>
            )}
          </span>
          {configOpen ? (
            <ChevronDown className="h-4 w-4 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 text-muted-foreground" />
          )}
        </button>
        {configOpen && (
        <CardContent className="border-t p-4">
          <BuildSummary
            variant="full"
            data={{
              ...summaryFromArtifact(artifact),
              targetGroup: targetGroupName ?? artifact.targetGroupId ?? undefined,
            }}
          />
        </CardContent>
        )}
      </Card>

      {/* Build Logs — terminal with a toolbar, follow-tail, wrap toggle,
          copy + download, live indicator, and line numbers. */}
      <Card className="overflow-hidden">
        <div className="flex items-center justify-between gap-3 border-b bg-muted/30 px-4 py-2.5">
          <div className="flex items-center gap-2 min-w-0">
            <TerminalIcon className="h-4 w-4 text-muted-foreground shrink-0" />
            <h3
              className="text-sm font-semibold cursor-pointer hover:underline"
              onClick={() => setLogsOpen((o) => !o)}
              title={logsOpen ? "Collapse logs" : "Expand logs"}
            >
              Build logs
              {!logsOpen && (
                <span className="ml-2 text-xs font-normal text-muted-foreground">
                  — click to expand
                </span>
              )}
            </h3>
            {isActive && wsConnected && (
              <span className="inline-flex items-center gap-1.5 text-[11px] text-primary">
                <span className="relative flex h-1.5 w-1.5">
                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-primary opacity-75" />
                  <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-primary" />
                </span>
                Live
              </span>
            )}
            {isActive && !wsConnected && (
              <span
                className="inline-flex items-center gap-1.5 text-[11px] text-amber-700 dark:text-amber-400"
                title="Lost connection to the log stream. Auto-reconnecting…"
              >
                <Loader2 className="h-3 w-3 animate-spin" />
                Reconnecting…
              </span>
            )}
            {logLineCount > 0 && (
              <span className="text-[11px] text-muted-foreground tabular-nums">
                {logLineCount.toLocaleString()} lines
              </span>
            )}
          </div>
          <div className="flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              title={wrapLogs ? "Disable wrap" : "Enable wrap"}
              onClick={() => setWrapLogs((v) => !v)}
            >
              <WrapText className={`h-3.5 w-3.5 ${wrapLogs ? "text-primary" : "text-muted-foreground"}`} />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              title={followLogs ? "Pause auto-scroll" : "Jump to bottom"}
              onClick={() => {
                if (followLogs) {
                  setFollowLogs(false);
                } else {
                  scrollLogsToBottom();
                }
              }}
            >
              <ArrowDown className={`h-3.5 w-3.5 ${followLogs ? "text-primary" : "text-muted-foreground"}`} />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              title="Copy logs"
              disabled={!logs}
              onClick={handleCopyLogs}
            >
              <Copy className="h-3.5 w-3.5 text-muted-foreground" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              title="Download .log"
              disabled={!logs}
              onClick={handleDownloadLogs}
            >
              <FileDown className="h-3.5 w-3.5 text-muted-foreground" />
            </Button>
            {/* Collapse sits last and behind a divider, so the one control
                that changes the layout is not mistaken for another log action.
                Matches the Configuration header, where the chevron is also the
                rightmost element of the row. px-2 on top of the row's px-4
                lines the glyph up with that px-6 header, to the pixel. */}
            <button
              type="button"
              onClick={() => setLogsOpen((o) => !o)}
              className="ml-1 h-7 px-2 border-l flex items-center rounded-r-sm hover:bg-muted/60"
              title={logsOpen ? "Collapse logs" : "Expand logs"}
              aria-expanded={logsOpen}
              aria-label={logsOpen ? "Collapse logs" : "Expand logs"}
            >
              {logsOpen ? (
                <ChevronDown className="h-4 w-4 text-muted-foreground" />
              ) : (
                <ChevronRight className="h-4 w-4 text-muted-foreground" />
              )}
            </button>
          </div>
        </div>
        <div
          ref={logsContainerRef}
          className={`terminal-output font-mono text-[12.5px] leading-5 overflow-auto max-h-[32rem] min-h-[20rem] ${logsOpen ? "" : "hidden"}`}
        >
          {logLines.length > 0 ? (
            // Line-numbered log rendering. We pre-split the string once and
            // render each line with a sticky gutter column. ansiToHtml
            // runs per-line so the gutter number doesn't inherit SGR state
            // from a preceding line.
            <div className={`flex ${wrapLogs ? "" : "w-max min-w-full"}`}>
              <div className="sticky left-0 select-none text-right pr-3 pl-4 py-3 text-muted-foreground/50 bg-black/20 border-r border-white/5 tabular-nums">
                {logLines.map((_, i) => (
                  <LogGutterNumber key={i} n={i + 1} />
                ))}
              </div>
              <div className={`py-3 pr-4 pl-3 flex-1 ${wrapLogs ? "whitespace-pre-wrap break-words" : "whitespace-pre"}`}>
                {logLines.map((line, i) => (
                  <LogLine key={i} line={line} />
                ))}
              </div>
            </div>
          ) : (
            <div className="flex items-center justify-center gap-2 py-16 text-muted-foreground/80 text-sm">
              {isActive ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Waiting for logs…
                </>
              ) : (
                <span>No logs recorded for this build.</span>
              )}
            </div>
          )}
        </div>
      </Card>

      {/* Non-Ready downloads fallback: if a non-terminal build somehow
          has files attached (partial products, aborted mid-stream, etc.)
          still expose them, but in a plain card — the success hero above
          is reserved for Ready builds. */}
      {artifact.phase !== "Ready" && artifactFiles.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Partial outputs</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="space-y-2">
              {artifactFiles.map((filePath) => {
                const filename = extractFilename(filePath);
                return (
                  <li key={filePath} className="flex items-center gap-3">
                    <Download className="h-4 w-4 text-muted-foreground" />
                    <a
                      href={artifactDownloadUrl(id!, filename)}
                      className="min-w-0 flex-1 truncate text-sm font-mono hover:underline text-primary"
                      download
                    >
                      {filename}
                    </a>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      title={`Copy a link to ${filename} that works for 1 hour without signing in. Scripts should download through the API with the admin password in an Authorization header.`}
                      onClick={() => copyDownloadLink(filename)}
                    >
                      <Link2 aria-hidden="true" />
                      Copy link
                    </Button>
                  </li>
                );
              })}
            </ul>
          </CardContent>
        </Card>
      )}

      {showDeploy && (
        <DeployDialog
          artifactId={id!}
          artifactFiles={artifactFiles}
          hasNetboot={artifact.netboot}
          defaultMethod={deployMethod}
          onClose={() => setShowDeploy(false)}
        />
      )}

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title="Delete Artifact"
        description="Are you sure you want to delete this artifact? This cannot be undone."
        confirmLabel="Delete"
        destructive
        onConfirm={handleDelete}
      />
    </div>
  );
}
