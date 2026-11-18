import { useEffect, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router";
import {
  getExtension,
  getExtensionLogs,
  deleteExtension,
  cancelExtension,
  extensionDownloadUrl,
  listNodesForExtension,
  type Extension,
  type NodeExtensionRow,
} from "@/api/extensions";
import { getArtifact, listSecureBootKeySets } from "@/api/artifacts";
import { listNodes, type Node } from "@/api/nodes";
import { ApiError } from "@/api/client";
import { MoreHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { StatusBadge } from "@/components/StatusBadge";
import { isBuilding, isFailed, isReady } from "@/lib/phase";
import { timeAgo } from "@/lib/time";
import { PageHeader } from "@/components/PageHeader";
import { ExtensionTypeChip } from "@/components/ExtensionTypeChip";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { InstallExtensionDialog } from "@/components/InstallExtensionDialog";

export function ExtensionDetail() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const { hash } = useLocation();
  const [ext, setExt] = useState<Extension | null>(null);
  const [logs, setLogs] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [installOpen, setInstallOpen] = useState(false);
  const [installs, setInstalls] = useState<NodeExtensionRow[] | null>(null);
  const [nodesByID, setNodesByID] = useState<Record<string, Node>>({});
  const [artifactName, setArtifactName] = useState<string | null>(null);
  const [keySetName, setKeySetName] = useState<string | null>(null);
  // When delete is blocked by a 409, we look up the names of the referencing
  // artifacts and surface a proper banner instead of the raw error string.
  const [deleteBlocker, setDeleteBlocker] = useState<
    { artifacts: { id: string; name: string }[] } | null
  >(null);

  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    async function load() {
      try {
        const [e, l] = await Promise.all([
          getExtension(id),
          getExtensionLogs(id).catch(() => ""),
        ]);
        if (!cancelled) {
          setExt(e);
          setLogs(l);
        }
      } catch (e) {
        if (!cancelled) setErr(String(e));
      }
    }
    load();
    // Poll while not in a terminal phase.
    const t = setInterval(() => {
      if (cancelled) return;
      if (ext && (ext.phase === "Ready" || ext.phase === "Error")) return;
      load();
    }, 3000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);

  // Where the extension is installed, with each node's hostname and phase.
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    listNodesForExtension(id)
      .then((rows) => !cancelled && setInstalls(rows))
      .catch(() => !cancelled && setInstalls([]));
    listNodes()
      .then((ns) => !cancelled && setNodesByID(Object.fromEntries(ns.map((n) => [n.id, n]))))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [id]);

  // "View log" on the list links to #logs; the router does not scroll to a
  // hash on its own, so do it once the card exists.
  const loaded = ext !== null;
  useEffect(() => {
    if (!loaded || hash !== "#logs") return;
    document.getElementById("logs")?.scrollIntoView?.({ block: "start" });
  }, [loaded, hash]);

  // Show the source artifact and the key set by name rather than by UUID.
  const sourceArtifactId = ext?.sourceArtifactId;
  const signingKeySetId = ext?.signingKeySetId;
  useEffect(() => {
    if (!sourceArtifactId) return;
    let cancelled = false;
    getArtifact(sourceArtifactId)
      .then((a) => !cancelled && setArtifactName(a.name || null))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [sourceArtifactId]);
  useEffect(() => {
    if (!signingKeySetId) return;
    let cancelled = false;
    listSecureBootKeySets()
      .then((ks) => {
        if (cancelled) return;
        setKeySetName(ks.find((k) => k.id === signingKeySetId)?.name ?? null);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [signingKeySetId]);

  async function onDelete() {
    try {
      await deleteExtension(id);
      navigate("/extensions");
    } catch (e) {
      // Server returns 409 with {error, artifacts:[artifactId,...]} when the
      // extension is still bundled. Look the names up so we can surface a
      // friendly message + links, instead of dumping the raw JSON on the user.
      if (
        e instanceof ApiError &&
        e.status === 409 &&
        e.body &&
        typeof e.body === "object" &&
        Array.isArray((e.body as { artifacts?: unknown }).artifacts)
      ) {
        const ids = (e.body as { artifacts: string[] }).artifacts;
        const named = await Promise.all(
          ids.map(async (aid) => {
            try {
              const a = await getArtifact(aid);
              return { id: aid, name: a.name || aid };
            } catch {
              return { id: aid, name: aid };
            }
          }),
        );
        setDeleteBlocker({ artifacts: named });
        return;
      }
      setErr(String(e));
    }
  }

  async function onCancel() {
    try {
      await cancelExtension(id);
    } catch (e) {
      setErr(String(e));
    }
  }

  if (err) {
    return <div className="text-danger-foreground p-4">Failed: {err}</div>;
  }
  if (!ext) {
    return <div className="p-4 text-muted-foreground text-sm">Loading…</div>;
  }

  return (
    <div>
      <PageHeader
        title={ext.name}
        description={`${ext.name} · ${ext.type}`}
        breadcrumb={[{ label: "Extensions", to: "/extensions" }, { label: ext.name }]}
        status={
          <span className="flex items-center gap-2">
            <ExtensionTypeChip type={ext.type} />
            <StatusBadge status={ext.phase} />
          </span>
        }
      >
        {isBuilding(ext.phase) && (
          <Button variant="outline" onClick={onCancel}>
            Cancel build
          </Button>
        )}
        {isReady(ext.phase) && ext.rawFilename && (
          <Button asChild variant="outline">
            <a href={extensionDownloadUrl(ext.id, ext.rawFilename)} download>
              Download .raw
            </a>
          </Button>
        )}
        <Button disabled={!isReady(ext.phase)} onClick={() => setInstallOpen(true)}>
          Install
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="icon" aria-label="More actions">
              <MoreHorizontal aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              className="text-danger focus:text-danger"
              onSelect={() => setConfirmDelete(true)}
            >
              Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </PageHeader>

      {isFailed(ext.phase) && ext.message && (
        <div
          role="alert"
          className="mb-4 rounded-md border border-danger/25 bg-danger/10 px-3 py-2 text-sm text-danger-foreground whitespace-pre-wrap"
        >
          {ext.message}
        </div>
      )}

      <div className="grid md:grid-cols-2 gap-4 mb-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Details</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-1.5 text-sm">
            <KV k="Type" v={ext.type} />
            <KV k="Arch" v={ext.arch} />
            <KV k="Version" v={ext.version} />
            <KV k="Source mode" v={ext.sourceMode} />
            {ext.sourceImage && <KV k="Source image" v={ext.sourceImage} />}
            {ext.sourceArtifactId && (
              <KV k="Source artifact" v={artifactName ?? ext.sourceArtifactId} />
            )}
            {ext.containerImage && (
              <KV k="Container image" v={ext.containerImage} />
            )}
            {ext.rawFilename && <KV k="Raw filename" v={ext.rawFilename} />}
            {ext.hierarchies && ext.hierarchies.length > 0 && (
              <KV k="Hierarchies" v={ext.hierarchies.join(", ")} />
            )}
            {ext.serviceReload && <KV k="Service reload" v="yes" />}
            {ext.signingKeySetId && (
              <KV k="Signing key set" v={keySetName ?? ext.signingKeySetId} />
            )}
          </CardContent>
        </Card>

        <Card data-slot="installed-on">
          <CardHeader>
            <CardTitle className="text-sm">Installed on</CardTitle>
          </CardHeader>
          <CardContent className="text-sm">
            {installs === null ? (
              <p className="text-muted-foreground">Loading…</p>
            ) : installs.length === 0 ? (
              <p className="text-muted-foreground">Not installed on any node.</p>
            ) : (
              <ul className="divide-y">
                {groupByNode(installs).map((g) => {
                  const node = nodesByID[g.nodeId];
                  return (
                    <li
                      key={g.nodeId}
                      className="flex flex-wrap items-center justify-between gap-2 py-2"
                    >
                      <span className="flex min-w-0 items-center gap-2">
                        <Link to={`/nodes/${g.nodeId}`} className="truncate font-medium hover:underline">
                          {node?.hostname || g.nodeId.slice(0, 8)}
                        </Link>
                        {node?.phase && <StatusBadge status={node.phase} />}
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {g.bootStates.join(", ")} · v{g.version} · installed {timeAgo(g.installedAt)}
                      </span>
                    </li>
                  );
                })}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>

      <Card id="logs">
        <CardHeader>
          <CardTitle className="text-sm">Build logs</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="text-xs font-mono bg-muted/40 rounded p-3 max-h-[40vh] overflow-auto whitespace-pre-wrap">
            {logs || "(no logs yet)"}
          </pre>
        </CardContent>
      </Card>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title="Delete extension"
        description={`Delete ${ext.name}? Artifacts that bundle this extension by name will block the deletion.`}
        onConfirm={onDelete}
      />

      <DeleteBlockedDialog
        open={deleteBlocker !== null}
        onOpenChange={(o) => !o && setDeleteBlocker(null)}
        extensionName={ext.name}
        artifacts={deleteBlocker?.artifacts ?? []}
      />

      <InstallExtensionDialog
        open={installOpen}
        onOpenChange={setInstallOpen}
        extension={ext}
      />
    </div>
  );
}

function DeleteBlockedDialog({
  open,
  onOpenChange,
  extensionName,
  artifacts,
}: {
  open: boolean;
  onOpenChange: (next: boolean) => void;
  extensionName: string;
  artifacts: { id: string; name: string }[];
}) {
  if (!open) return null;
  return (
    <div
      role="dialog"
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      onClick={() => onOpenChange(false)}
    >
      <div
        className="bg-background rounded-lg shadow-xl border max-w-[480px] w-[92%] p-5"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-base font-semibold flex items-center gap-2">
          <span className="inline-block h-2 w-2 rounded-full bg-warning" />
          Can&apos;t delete <code className="text-sm font-mono">{extensionName}</code> yet
        </h2>
        <p className="text-sm text-muted-foreground mt-2">
          It&apos;s currently bundled into
          {artifacts.length === 1 ? " this OS artifact" : ` these ${artifacts.length} OS artifacts`}.
          Detach it from each, then try deleting again.
        </p>
        <ul className="mt-3 border rounded-md divide-y max-h-56 overflow-y-auto">
          {artifacts.map((a) => (
            <li key={a.id} className="px-3 py-2 text-sm flex items-center justify-between gap-3">
              <span className="truncate font-medium">{a.name}</span>
              <Link
                to={`/artifacts/${a.id}`}
                className="text-xs text-primary hover:underline shrink-0"
              >
                Open →
              </Link>
            </li>
          ))}
        </ul>
        <div className="mt-4 flex justify-end">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Got it
          </Button>
        </div>
      </div>
    </div>
  );
}

// groupByNode folds the per-boot-scope install rows into one entry per node,
// keeping the latest version and install time.
function groupByNode(rows: NodeExtensionRow[]) {
  const byNode = new Map<string, { nodeId: string; bootStates: string[]; version: string; installedAt: string }>();
  for (const r of rows) {
    const g = byNode.get(r.nodeId);
    if (!g) {
      byNode.set(r.nodeId, { nodeId: r.nodeId, bootStates: [r.bootState], version: r.version, installedAt: r.installedAt });
      continue;
    }
    g.bootStates.push(r.bootState);
    if (r.installedAt > g.installedAt) {
      g.installedAt = r.installedAt;
      g.version = r.version;
    }
  }
  return [...byNode.values()];
}

function KV({ k, v }: { k: string; v: string }) {
  return (
    <div className="grid grid-cols-[140px_1fr] gap-2 text-sm">
      <span className="text-muted-foreground">{k}</span>
      <span className="font-mono break-all">{v}</span>
    </div>
  );
}
