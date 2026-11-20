import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router";
import { AlertTriangle, MoreHorizontal, Plus } from "lucide-react";
import {
  listExtensions,
  listNodesForExtension,
  type Extension,
} from "@/api/extensions";
import { listArtifacts, listSecureBootKeySets } from "@/api/artifacts";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { PageHeader } from "@/components/PageHeader";
import { StatusBadge } from "@/components/StatusBadge";
import { ExtensionTypeChip } from "@/components/ExtensionTypeChip";
import { InstallExtensionDialog } from "@/components/InstallExtensionDialog";
import { isBuilding, isFailed, isReady } from "@/lib/phase";
import { timeAgo } from "@/lib/time";

const TEMPLATES = ["Tailscale", "Fluent-bit", "Nvidia container toolkit"];

const DESCRIPTION =
  "System and config extensions, installed on nodes next to the OS image.";

type Names = Record<string, string>;

export function Extensions() {
  const navigate = useNavigate();
  const [rows, setRows] = useState<Extension[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [artifactNames, setArtifactNames] = useState<Names>({});
  const [keySetNames, setKeySetNames] = useState<Names>({});
  const [installExt, setInstallExt] = useState<Extension | null>(null);

  useEffect(() => {
    listExtensions()
      .then(setRows)
      .catch((e) => setErr(String(e)));
    // Names only decorate the table; a failed lookup falls back to the id.
    listArtifacts()
      .then((as) =>
        setArtifactNames(Object.fromEntries(as.map((a) => [a.id, a.name || a.id]))),
      )
      .catch(() => {});
    listSecureBootKeySets()
      .then((ks) => setKeySetNames(Object.fromEntries(ks.map((k) => [k.id, k.name]))))
      .catch(() => {});
  }, []);

  if (err) {
    return <div className="text-danger-foreground p-4">Failed to load: {err}</div>;
  }
  if (rows === null) {
    return <div className="p-4 text-muted-foreground text-sm">Loading…</div>;
  }
  if (rows.length === 0) {
    return (
      <div>
        <PageHeader title="Extensions" description={DESCRIPTION} />
        <div className="text-center py-16">
          <p className="text-xl font-semibold mb-1.5">No extensions yet</p>
          <p className="text-sm text-muted-foreground max-w-md mx-auto mb-5">
            System and config extensions extend a running Kairos node without
            re-imaging — ship a binary, a config drop-in, or a whole agent on
            top of an OS artifact.
          </p>
          <Button onClick={() => navigate("/extensions/new")}>
            <Plus aria-hidden="true" />
            Build extension
          </Button>
          <div className="flex gap-2 justify-center mt-4 text-xs text-muted-foreground items-center flex-wrap">
            <span>Or start from a template:</span>
            {TEMPLATES.map((t) => (
              <Link
                key={t}
                to={`/extensions/new?template=${encodeURIComponent(t)}`}
                className="px-2.5 py-0.5 rounded-full border border-dashed"
              >
                {t}
              </Link>
            ))}
          </div>
        </div>
      </div>
    );
  }

  const failed = rows.filter((r) => isFailed(r.phase));

  return (
    <div>
      <PageHeader title="Extensions" description={DESCRIPTION}>
        <Button onClick={() => navigate("/extensions/new")}>
          <Plus aria-hidden="true" />
          Build extension
        </Button>
      </PageHeader>

      {failed.map((r) => (
        <div
          key={r.id}
          role="alert"
          className="mb-3 flex flex-wrap items-center gap-3 rounded-md border border-danger/25 bg-danger/10 px-3 py-2 text-sm text-danger-foreground"
        >
          <AlertTriangle className="h-4 w-4 shrink-0" aria-hidden="true" />
          <span className="min-w-0 flex-1">
            <b className="font-semibold">{r.name}</b> failed to build
            {firstLine(r.message) ? `: ${firstLine(r.message)}` : "."}
          </span>
          <Button asChild variant="outline" size="sm">
            <Link to={`/extensions/${r.id}#logs`}>View log</Link>
          </Button>
        </div>
      ))}

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow className="bg-muted/50 hover:bg-muted/50">
              <TableHead>Extension</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Version</TableHead>
              <TableHead>Installed on</TableHead>
              <TableHead>Signed</TableHead>
              <TableHead>Updated</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <ExtensionRow
                key={r.id}
                ext={r}
                artifactNames={artifactNames}
                keySetNames={keySetNames}
                onInstall={() => setInstallExt(r)}
              />
            ))}
          </TableBody>
        </Table>
      </div>

      {installExt && (
        <InstallExtensionDialog
          open
          onOpenChange={(o) => !o && setInstallExt(null)}
          extension={installExt}
        />
      )}
    </div>
  );
}

function ExtensionRow({
  ext,
  artifactNames,
  keySetNames,
  onInstall,
}: {
  ext: Extension;
  artifactNames: Names;
  keySetNames: Names;
  onInstall: () => void;
}) {
  const navigate = useNavigate();
  const detail = `/extensions/${ext.id}`;
  return (
    <TableRow className="cursor-pointer" onClick={() => navigate(detail)}>
      <TableCell>
        <div className="flex items-center gap-2.5">
          <ExtensionTypeChip type={ext.type} />
          <div className="min-w-0">
            <Link to={detail} className="font-medium hover:underline">
              {ext.name}
            </Link>
            <div className="text-xs text-muted-foreground">
              {sourceLabel(ext, artifactNames)} · {ext.arch}
            </div>
          </div>
        </div>
      </TableCell>
      <TableCell>
        <StatusCell ext={ext} />
      </TableCell>
      <TableCell>
        <code className="text-xs">{ext.version}</code>
      </TableCell>
      <TableCell>
        <InstalledOn id={ext.id} />
      </TableCell>
      <TableCell>
        {ext.signingKeySetId ? (
          <span>{keySetNames[ext.signingKeySetId] ?? ext.signingKeySetId}</span>
        ) : (
          <span className="text-muted-foreground">Unsigned</span>
        )}
      </TableCell>
      <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
        {timeAgo(ext.updatedAt)}
      </TableCell>
      {/* The menu and the dialog render in portals, but React still bubbles
          their clicks through this cell, so stop them before the row. */}
      <TableCell onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-end gap-1">
          {isReady(ext.phase) && (
            <Button variant="outline" size="sm" onClick={onInstall}>
              Install
            </Button>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon-sm" aria-label={`More actions for ${ext.name}`}>
                <MoreHorizontal aria-hidden="true" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => navigate(detail)}>View details</DropdownMenuItem>
              <DropdownMenuItem onSelect={() => navigate(`${detail}#logs`)}>View log</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </TableCell>
    </TableRow>
  );
}

function StatusCell({ ext }: { ext: Extension }) {
  if (!isBuilding(ext.phase)) return <StatusBadge status={ext.phase} />;
  const progress = parseProgress(ext.message);
  return (
    <div className="grid min-w-[130px] gap-1">
      <StatusBadge
        status={progress === null ? ext.phase : `${ext.phase} · ${progress}%`}
        className="justify-self-start"
      />
      <div
        role="progressbar"
        aria-label={`${ext.name} build progress`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={progress ?? undefined}
        className="h-1 overflow-hidden rounded bg-muted"
      >
        <div
          className={progress === null ? "h-1 w-1/3 animate-pulse rounded bg-info" : "h-1 rounded bg-info"}
          style={progress === null ? undefined : { width: `${progress}%` }}
        />
      </div>
    </div>
  );
}

// InstalledOn loads the install rows for one extension and shows how many
// distinct nodes have it; a node with it in several boot scopes counts once.
function InstalledOn({ id }: { id: string }) {
  const [count, setCount] = useState<number | null>(null);
  useEffect(() => {
    let cancelled = false;
    listNodesForExtension(id)
      .then((rows) => {
        if (!cancelled) setCount(new Set(rows.map((r) => r.nodeId)).size);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [id]);
  if (count === null) return <span className="text-muted-foreground">—</span>;
  if (count === 0) return <span className="text-muted-foreground">Not installed</span>;
  return <span>{count === 1 ? "1 node" : `${count} nodes`}</span>;
}

// The builder reports progress as a leading percentage in the message.
function parseProgress(message: string): number | null {
  const n = parseInt(message, 10);
  if (Number.isNaN(n)) return null;
  return Math.min(100, Math.max(0, n));
}

function firstLine(message?: string): string {
  return (message ?? "").split("\n")[0].trim();
}

function sourceLabel(ext: Extension, artifactNames: Names): string {
  switch (ext.sourceMode) {
    case "artifact": {
      const id = ext.sourceArtifactId ?? "";
      const name = artifactNames[id] ?? "an artifact";
      return `from ${name}${ext.extraSteps ? " + steps" : ""}`;
    }
    case "dockerfile":
      return "from Dockerfile";
    case "image":
      return `from ${ext.sourceImage ?? "an image"}`;
    default:
      return "";
  }
}
