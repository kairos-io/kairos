import { useEffect, useState, useCallback, useRef } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { listArtifacts, deleteArtifact, clearFailedArtifacts, updateArtifact, type Artifact } from "@/api/artifacts";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { StatusBadge } from "@/components/StatusBadge";
import { PageHeader } from "@/components/PageHeader";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Plus, Trash2, Package, Bookmark, Copy } from "lucide-react";
import { Badge } from "@/components/ui/badge";

// Valid values for each URL-driven axis. Any other value falls back to the
// first option so a malformed share link (or a stale schema from a rename)
// still renders a sensible page.
const TAB_VALUES = ["all", "saved"] as const;
const STATUS_VALUES = ["all", "building", "ready", "error"] as const;
type TabValue = (typeof TAB_VALUES)[number];
type StatusValue = (typeof STATUS_VALUES)[number];

const STATUS_LABELS: Record<StatusValue, string> = {
  all: "All statuses",
  building: "Building",
  ready: "Ready",
  error: "Failed",
};

function coerceTab(raw: string | null): TabValue {
  return (TAB_VALUES as readonly string[]).includes(raw ?? "")
    ? (raw as TabValue)
    : "all";
}

function coerceStatus(raw: string | null): StatusValue {
  return (STATUS_VALUES as readonly string[]).includes(raw ?? "")
    ? (raw as StatusValue)
    : "all";
}

export function Artifacts() {
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [sp, setSp] = useSearchParams();
  const tab = coerceTab(sp.get("tab"));
  const status = coerceStatus(sp.get("status"));
  // Local mirror of the search query so typing stays snappy; the URL gets
  // debounced-updated so the browser history doesn't fill with keystrokes.
  const search = sp.get("q") ?? "";
  const [searchDraft, setSearchDraft] = useState(search);
  // If the URL's ?q= changes externally (back/forward, cleared filter, etc.),
  // reconcile the local draft with it. This is the React canonical "adjust
  // state during rendering" pattern — the conditional setState converges in
  // one extra render and avoids the setState-in-effect anti-pattern.
  const [prevUrlSearch, setPrevUrlSearch] = useState(search);
  if (search !== prevUrlSearch) {
    setPrevUrlSearch(search);
    setSearchDraft(search);
  }
  const [confirmState, setConfirmState] = useState<{ open: boolean; action: () => void; title: string; description: string }>({ open: false, action: () => {}, title: "", description: "" });
  const navigate = useNavigate();

  // Keep the search URL param in sync with the draft, but debounced. Using
  // `replace: true` avoids pushing a new history entry on every keystroke.
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => {
      patchParams({ q: searchDraft || undefined });
    }, 250);
    return () => {
      if (searchTimer.current) clearTimeout(searchTimer.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchDraft]);

  // patchParams merges into the existing URLSearchParams and drops keys
  // whose value is the default/empty so the URL stays short when no
  // filters are active.
  function patchParams(patch: Record<string, string | undefined>) {
    const next = new URLSearchParams(sp);
    for (const [k, v] of Object.entries(patch)) {
      if (!v || v === "all") next.delete(k);
      else next.set(k, v);
    }
    setSp(next, { replace: true });
  }

  const fetchArtifacts = useCallback(() => {
    listArtifacts().then(setArtifacts).catch(() => {});
  }, []);

  useEffect(() => {
    fetchArtifacts();
  }, [fetchArtifacts]);

  // Auto-poll every 5s while any artifact is Pending or Building
  useEffect(() => {
    const hasActive = artifacts.some(
      (a) => a.phase === "Pending" || a.phase === "Building"
    );
    if (!hasActive) return;

    const interval = setInterval(fetchArtifacts, 5000);
    return () => clearInterval(interval);
  }, [artifacts, fetchArtifacts]);

  const filtered = artifacts
    .filter((a) => (tab === "saved" ? a.saved : true))
    .filter((a) => {
      if (status === "all") return true;
      if (status === "building") return a.phase === "Pending" || a.phase === "Building";
      if (status === "ready") return a.phase === "Ready";
      if (status === "error") return a.phase === "Error";
      return true;
    })
    .filter((a) => {
      if (!search) return true;
      const q = search.toLowerCase();
      return (
        (a.name || "").toLowerCase().includes(q) ||
        a.baseImage.toLowerCase().includes(q)
      );
    });
  const savedCount = artifacts.filter((a) => a.saved).length;
  const hasAnyFilter = tab !== "all" || status !== "all" || !!search;

  return (
    <div>
      <PageHeader title="Artifacts" description="Build and manage OS artifacts">
        {artifacts.some((a) => a.phase === "Error") && (
          <Button
            variant="outline"
            onClick={() => {
              setConfirmState({
                open: true,
                title: "Delete Failed Artifacts",
                description: "Are you sure you want to delete all failed artifacts? This cannot be undone.",
                action: async () => {
                  await clearFailedArtifacts();
                  fetchArtifacts();
                },
              });
            }}
          >
            <Trash2 className="h-4 w-4 mr-2" />
            Clear Failed
          </Button>
        )}
        <Button
          onClick={() => navigate("/artifacts/new")}
        >
          <Plus className="h-4 w-4 mr-2" />
          Build New
        </Button>
      </PageHeader>

      <div className="flex flex-wrap items-center gap-3 mb-4">
        <Tabs value={tab} onValueChange={(v) => patchParams({ tab: v === "all" ? undefined : v })}>
          <TabsList>
            <TabsTrigger value="all">All Builds</TabsTrigger>
            <TabsTrigger value="saved">
              Saved{savedCount > 0 && ` (${savedCount})`}
            </TabsTrigger>
          </TabsList>
        </Tabs>

        <Select
          value={status}
          onValueChange={(v) => patchParams({ status: v === "all" ? undefined : v })}
        >
          <SelectTrigger className="h-9 w-[11rem]">
            <SelectValue placeholder="All statuses" />
          </SelectTrigger>
          <SelectContent>
            {STATUS_VALUES.map((s) => (
              <SelectItem key={s} value={s}>
                {STATUS_LABELS[s]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <Input
          placeholder="Search artifacts..."
          value={searchDraft}
          onChange={(e) => setSearchDraft(e.target.value)}
          className="max-w-sm"
        />
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-10"></TableHead>
            <TableHead>Name</TableHead>
            <TableHead>Base Image</TableHead>
            <TableHead>Phase</TableHead>
            <TableHead>Created</TableHead>
            <TableHead>Artifacts</TableHead>
            <TableHead className="w-12"></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {filtered.length === 0 ? (
            <TableRow>
              <TableCell colSpan={7} className="text-center py-12">
                {hasAnyFilter ? (
                  <div className="flex flex-col items-center gap-3 py-10">
                    <Package className="h-10 w-10 text-muted-foreground/40" />
                    <div className="text-center">
                      <p className="text-sm font-medium text-muted-foreground">
                        {status !== "all"
                          ? `No ${STATUS_LABELS[status].toLowerCase()} builds`
                          : tab === "saved"
                          ? "No saved artifacts match this filter"
                          : "No matching artifacts"}
                      </p>
                      <p className="text-xs text-muted-foreground/70 mt-1">
                        {search
                          ? "Try adjusting your search query."
                          : "Clear the active filter to see all builds."}
                      </p>
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setSearchDraft("");
                        setSp(new URLSearchParams(), { replace: true });
                      }}
                    >
                      Clear filters
                    </Button>
                  </div>
                ) : (
                  <div className="flex flex-col items-center gap-3 py-16">
                    <Package className="h-12 w-12 text-muted-foreground/30" />
                    <div className="text-center">
                      <p className="font-medium">No artifacts yet</p>
                      <p className="text-sm text-muted-foreground mt-1">
                        Build your first OS image to deploy across your fleet.
                      </p>
                    </div>
                    <Button
                      className="mt-2"
                      onClick={() => navigate("/artifacts/new")}
                    >
                      <Plus className="h-4 w-4 mr-2" /> Build First Artifact
                    </Button>
                  </div>
                )}
              </TableCell>
            </TableRow>
          ) : (
            filtered.map((artifact) => (
              <TableRow
                key={artifact.id}
                className="cursor-pointer hover:bg-primary-soft"
                onClick={() => navigate(`/artifacts/${artifact.id}`)}
              >
                <TableCell>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-7 w-7"
                    onClick={async (e) => {
                      e.stopPropagation();
                      await updateArtifact(artifact.id, { saved: !artifact.saved });
                      fetchArtifacts();
                    }}
                  >
                    <Bookmark
                      className={`h-4 w-4 ${
                        artifact.saved
                          ? "fill-primary text-primary"
                          : "text-muted-foreground/40"
                      }`}
                    />
                  </Button>
                </TableCell>
                <TableCell>
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate max-w-xs">
                      {artifact.name || (
                        <span className="text-muted-foreground font-mono text-xs">
                          {artifact.id.slice(0, 8)}
                        </span>
                      )}
                    </p>
                    {artifact.name && (
                      <p className="text-xs text-muted-foreground font-mono">
                        {artifact.id.slice(0, 8)}
                      </p>
                    )}
                  </div>
                </TableCell>
                <TableCell className="text-sm max-w-xs">
                  <div className="flex flex-col gap-1 min-w-0">
                    <span className="truncate">
                      {artifact.hadronBase || artifact.baseImage || "-"}
                    </span>
                    <div className="flex gap-1 flex-wrap">
                      {artifact.arch && (
                        <Badge variant="secondary" className="text-[10px] font-mono" title="Architecture">
                          {artifact.arch}
                        </Badge>
                      )}
                      {artifact.hadronBase && (
                        <Badge
                          variant="outline"
                          className="text-[10px] font-mono border-primary text-primary"
                          title="Hadron composite (firmware + layers)"
                        >
                          Hadron
                        </Badge>
                      )}
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <div className="flex flex-col gap-1">
                    <StatusBadge status={artifact.phase} />
                    {artifact.phase === "Error" && artifact.message && (
                      <span className="text-xs text-red-600 truncate max-w-xs">
                        {artifact.message}
                      </span>
                    )}
                  </div>
                </TableCell>
                <TableCell className="text-xs">
                  {artifact.createdAt
                    ? new Date(artifact.createdAt).toLocaleDateString()
                    : "-"}
                </TableCell>
                <TableCell className="text-xs">
                  {(artifact.artifacts || []).length > 0
                    ? `${artifact.artifacts.length} file(s)`
                    : "-"}
                </TableCell>
                <TableCell>
                  <div className="flex gap-1">
                    {artifact.phase === "Ready" && (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7"
                        title={artifact.hadronBase ? "Clone as Hadron" : "Clone"}
                        onClick={(e) => {
                          e.stopPropagation();
                          navigate(`/artifacts/new?clone=${artifact.id}`);
                        }}
                      >
                        <Copy className="h-4 w-4" />
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7"
                      onClick={(e) => {
                        e.stopPropagation();
                        setConfirmState({
                          open: true,
                          title: "Delete Artifact",
                          description: "Are you sure you want to delete this artifact? This cannot be undone.",
                          action: async () => {
                            await deleteArtifact(artifact.id);
                            fetchArtifacts();
                          },
                        });
                      }}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>

      <ConfirmDialog
        open={confirmState.open}
        onOpenChange={(open) => setConfirmState(prev => ({ ...prev, open }))}
        title={confirmState.title}
        description={confirmState.description}
        confirmLabel="Delete"
        destructive
        onConfirm={confirmState.action}
      />
    </div>
  );
}
