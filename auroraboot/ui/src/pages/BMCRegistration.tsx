import { Fragment, useEffect, useState } from "react";
import { PageHeader } from "@/components/PageHeader";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Cpu,
  MemoryStick,
  Loader2,
  Pencil,
  Trash2,
  Search,
  Plus,
  ServerCog,
  RefreshCw,
  Wifi,
} from "lucide-react";
import {
  type BMCTarget,
  type InspectResult,
  listBMCTargets,
  createBMCTarget,
  updateBMCTarget,
  deleteBMCTarget,
  inspectHardware,
  pingBMCTarget,
  refreshAllBMCTargets,
} from "@/api/deployments";
import {
  type ImageSourceSettings,
  getImageSourceSettings,
  updateImageSourceSettings,
} from "@/api/settings";
import { type QuirkProfile, listQuirkProfiles } from "@/api/redfish";
import { toast } from "@/hooks/useToast";
import { PRODUCT_NAME } from "@/lib/branding";

// Vendor options are no longer a hardcoded list: they come from the quirk profiles
// the server actually loaded (built-in + operator-supplied via --redfish-quirks-dir),
// fetched from GET /api/v1/redfish/quirk-profiles. This is what makes an operator's
// own profile selectable here. "generic" always exists (the spec-default path) and
// is the safe default.
const DEFAULT_VENDOR = "generic";

// tierVariant maps a profile's support tier to a Badge variant: A (core-tested)
// reads as a positive default, B (community-validated) as secondary, C (unverified)
// as a muted outline.
function tierVariant(tier: string): "default" | "secondary" | "outline" {
  switch (tier) {
    case "A":
      return "default";
    case "B":
      return "secondary";
    default:
      return "outline";
  }
}

type FormState = {
  name: string;
  endpoint: string;
  vendor: string;
  username: string;
  password: string;
  verifySSL: boolean;
  systemId: string;
  imageUrl: string;
  ejectAfterInstall: boolean;
  ejectPowerCycle: boolean;
};

const EMPTY_FORM: FormState = {
  name: "",
  endpoint: "",
  vendor: DEFAULT_VENDOR,
  username: "",
  password: "",
  verifySSL: false,
  systemId: "",
  imageUrl: "",
  ejectAfterInstall: false,
  ejectPowerCycle: false,
};

// imageSourceBadge describes how a BMC row resolves its image source, given the
// per-BMC override and the global image-source settings.
function imageSourceBadge(
  target: BMCTarget,
  settings: ImageSourceSettings | null
): { label: string; variant: "default" | "secondary" | "outline" } {
  if (target.imageUrl) {
    return { label: "Per-BMC URL", variant: "default" };
  }
  if (settings?.defaultImageURL) {
    return { label: "Global default", variant: "secondary" };
  }
  if (settings?.localServe.configured && settings.localServe.enabled) {
    return { label: `${PRODUCT_NAME}-served`, variant: "outline" };
  }
  return { label: "Unset", variant: "outline" };
}

// statusBadge maps a target's cached LastStatus to a badge label/variant. The
// empty status ("" / undefined) renders as "Unknown" — the target has never been
// pinged or inspected, and we never auto-contact a BMC on page load.
function statusBadge(status: BMCTarget["lastStatus"]): {
  label: string;
  variant: "default" | "secondary" | "destructive" | "outline";
} {
  switch (status) {
    case "reachable":
      return { label: "Reachable", variant: "default" };
    case "unreachable":
      return { label: "Unreachable", variant: "destructive" };
    default:
      return { label: "Unknown", variant: "outline" };
  }
}

// relativeTime renders an ISO timestamp as a short, human relative string ("just
// now", "5m ago", "2h ago", "3d ago"). Returns "" for a missing/invalid time so
// the caller can omit it.
function relativeTime(iso?: string): string {
  if (!iso) return "";
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";
  const secs = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (secs < 45) return "just now";
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
}

// lastCheckedLabel describes the most recent reachability signal for a target,
// preferring the more-recent of the ping and inspect timestamps.
function lastCheckedLabel(t: BMCTarget): string {
  const ping = t.lastPingAt ? new Date(t.lastPingAt).getTime() : 0;
  const inspect = t.lastInspectAt ? new Date(t.lastInspectAt).getTime() : 0;
  if (!ping && !inspect) return "";
  if (ping >= inspect) return `pinged ${relativeTime(t.lastPingAt)}`;
  return `inspected ${relativeTime(t.lastInspectAt)}`;
}

export function BMCRegistration() {
  const [targets, setTargets] = useState<BMCTarget[]>([]);
  const [loading, setLoading] = useState(true);

  // Quirk profiles the server loaded (built-in + operator). Drives the vendor
  // selector and the per-row tier badge. Fetched once on mount; the registry is
  // load-at-start on the server, so it never changes during a session. Until
  // the fetch resolves (or if it fails), the selector falls back to the
  // spec-default generic profile so the form is never left without options:
  // generic always exists server-side.
  const [profiles, setProfiles] = useState<QuirkProfile[]>([]);
  const vendorOptions: QuirkProfile[] = profiles.length
    ? profiles
    : [
        {
          name: DEFAULT_VENDOR,
          tier: "A",
          tierDescription: "tier A: core-tested",
          origin: "builtin",
        },
      ];

  // Global image-source settings (model b): the top-of-page panel. `imgSource`
  // is null until the first fetch resolves.
  const [imgSource, setImgSource] = useState<ImageSourceSettings | null>(null);
  const [imgDefaultURL, setImgDefaultURL] = useState("");
  const [imgAdvertisedURL, setImgAdvertisedURL] = useState("");
  const [savingImgSource, setSavingImgSource] = useState(false);

  // Add/edit modal. `editing` holds the target being edited, or null for "add".
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<BMCTarget | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [saving, setSaving] = useState(false);

  // Delete confirmation.
  const [deleteTarget, setDeleteTarget] = useState<BMCTarget | null>(null);

  // Per-row inspect state, keyed by target id. Inspect is on-demand only.
  const [inspectingId, setInspectingId] = useState<string | null>(null);
  const [inspectResults, setInspectResults] = useState<
    Record<string, InspectResult>
  >({});
  const [inspectErrors, setInspectErrors] = useState<Record<string, string>>({});

  // Per-row ping state (the session-free reachability check) and the throttled
  // "Refresh all" sweep. Both are opt-in: status is rendered from the cached
  // fields the list endpoint returns, never auto-fetched on load.
  const [pingingId, setPingingId] = useState<string | null>(null);
  const [refreshingAll, setRefreshingAll] = useState(false);

  function fetchTargets() {
    listBMCTargets()
      .then(setTargets)
      .catch((err) =>
        toast(`Failed to load BMC targets: ${(err as Error).message}`, "error")
      )
      .finally(() => setLoading(false));
  }

  function applyImgSource(s: ImageSourceSettings) {
    setImgSource(s);
    setImgDefaultURL(s.defaultImageURL);
    setImgAdvertisedURL(s.localServe.advertisedURL);
  }

  function fetchImageSource() {
    getImageSourceSettings()
      .then(applyImgSource)
      .catch((err) =>
        toast(
          `Failed to load image-source settings: ${(err as Error).message}`,
          "error"
        )
      );
  }

  function fetchProfiles() {
    listQuirkProfiles()
      .then(setProfiles)
      .catch((err) =>
        toast(
          `Failed to load Redfish quirk profiles: ${(err as Error).message}`,
          "error"
        )
      );
  }

  useEffect(() => {
    fetchTargets();
    fetchImageSource();
    fetchProfiles();
  }, []);

  async function handleSaveImageSource(e: React.FormEvent) {
    e.preventDefault();
    setSavingImgSource(true);
    try {
      const updated = await updateImageSourceSettings({
        defaultImageURL: imgDefaultURL,
        // advertisedURL is only meaningful when a listener is configured.
        ...(imgSource?.localServe.configured
          ? { localServeAdvertisedURL: imgAdvertisedURL }
          : {}),
      });
      applyImgSource(updated);
      toast("Saved image source settings", "success");
    } catch (err) {
      toast(
        `Failed to save image-source settings: ${(err as Error).message}`,
        "error"
      );
    } finally {
      setSavingImgSource(false);
    }
  }

  async function handleToggleLocalServe(enabled: boolean) {
    try {
      const updated = await updateImageSourceSettings({
        localServeEnabled: enabled,
      });
      applyImgSource(updated);
    } catch (err) {
      toast(
        `Failed to update local serving: ${(err as Error).message}`,
        "error"
      );
    }
  }

  function openAdd() {
    setEditing(null);
    setForm(EMPTY_FORM);
    setFormOpen(true);
  }

  function openEdit(t: BMCTarget) {
    setEditing(t);
    // Password is never returned by the API; leave it blank to keep existing.
    setForm({
      name: t.name,
      endpoint: t.endpoint,
      vendor: t.vendor,
      username: t.username,
      password: "",
      verifySSL: t.verifySSL,
      systemId: t.systemId ?? "",
      imageUrl: t.imageUrl ?? "",
      ejectAfterInstall: t.ejectAfterInstall ?? false,
      ejectPowerCycle: t.ejectPowerCycle ?? false,
    });
    setFormOpen(true);
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      if (editing) {
        const payload: FormState = { ...form };
        // Omit a blank password so the backend keeps the stored credential.
        const updated = await updateBMCTarget(
          editing.id,
          payload.password
            ? payload
            : {
                name: payload.name,
                endpoint: payload.endpoint,
                vendor: payload.vendor,
                username: payload.username,
                verifySSL: payload.verifySSL,
                systemId: payload.systemId,
                imageUrl: payload.imageUrl,
                ejectAfterInstall: payload.ejectAfterInstall,
                ejectPowerCycle: payload.ejectPowerCycle,
              }
        );
        setTargets((prev) =>
          prev.map((t) => (t.id === updated.id ? updated : t))
        );
        toast(`Updated BMC "${updated.name}"`, "success");
      } else {
        const created = await createBMCTarget(form);
        setTargets((prev) => [...prev, created]);
        toast(`Added BMC "${created.name}"`, "success");
      }
      setFormOpen(false);
    } catch (err) {
      toast(
        `Failed to save BMC: ${(err as Error).message}`,
        "error"
      );
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return;
    const t = deleteTarget;
    try {
      await deleteBMCTarget(t.id);
      setTargets((prev) => prev.filter((x) => x.id !== t.id));
      // Drop any inspect state we held for this target.
      setInspectResults((prev) => {
        const next = { ...prev };
        delete next[t.id];
        return next;
      });
      setInspectErrors((prev) => {
        const next = { ...prev };
        delete next[t.id];
        return next;
      });
      toast(`Deleted BMC "${t.name}"`, "success");
    } catch (err) {
      toast(`Failed to delete BMC: ${(err as Error).message}`, "error");
    }
  }

  async function handleInspect(t: BMCTarget) {
    setInspectingId(t.id);
    setInspectErrors((prev) => {
      const next = { ...prev };
      delete next[t.id];
      return next;
    });
    try {
      const result = await inspectHardware(t.id);
      setInspectResults((prev) => ({ ...prev, [t.id]: result }));
      // Inspect persists the result into the server-owned status cache. Re-read
      // the list so the row's Status badge/timestamp reflect the fresh "reachable"
      // facts rather than stale cached values.
      fetchTargets();
    } catch (err) {
      setInspectErrors((prev) => ({
        ...prev,
        [t.id]: err instanceof Error ? err.message : "Inspection failed",
      }));
      // The failed inspect also persisted "unreachable"; refresh so the badge
      // reflects it.
      fetchTargets();
    } finally {
      setInspectingId(null);
    }
  }

  // handlePing runs the session-free reachability check for one target and folds
  // the freshly-persisted status into the row in place (no full re-fetch).
  async function handlePing(t: BMCTarget) {
    setPingingId(t.id);
    try {
      const status = await pingBMCTarget(t.id);
      setTargets((prev) =>
        prev.map((x) =>
          x.id === t.id
            ? {
                ...x,
                lastStatus: status.status,
                lastPingAt: status.lastPingAt,
                lastError: status.error ?? "",
              }
            : x
        )
      );
    } catch (err) {
      toast(`Ping failed: ${(err as Error).message}`, "error");
    } finally {
      setPingingId(null);
    }
  }

  // handleRefreshAll triggers the throttled, single-in-flight server sweep and
  // re-reads the list so every row reflects its new cached status. A concurrent
  // sweep is rejected by the server with 409, surfaced here as a toast.
  async function handleRefreshAll() {
    setRefreshingAll(true);
    try {
      const results = await refreshAllBMCTargets();
      fetchTargets();
      const reachable = results.filter((r) => r.status === "reachable").length;
      toast(
        `Refreshed ${results.length} BMC${results.length === 1 ? "" : "s"}: ${reachable} reachable`,
        "success"
      );
    } catch (err) {
      toast(`Refresh all failed: ${(err as Error).message}`, "error");
    } finally {
      setRefreshingAll(false);
    }
  }

  return (
    <div>
      <PageHeader
        title="BMC Registration"
        description="Register and manage baseboard management controllers for RedFish deployments"
      >
        <Button
          variant="outline"
          onClick={handleRefreshAll}
          disabled={refreshingAll || targets.length === 0}
        >
          {refreshingAll ? (
            <Loader2 className="h-4 w-4 mr-2 animate-spin" />
          ) : (
            <RefreshCw className="h-4 w-4 mr-2" />
          )}
          Refresh all
        </Button>
        <Button
          onClick={openAdd}
        >
          <Plus className="h-4 w-4 mr-2" />
          Add BMC
        </Button>
      </PageHeader>

      {/* Image source panel (model b global): the default image URL every BMC
          falls back to, plus — when a local-serve listener was configured at
          launch — the toggle and advertised URL for AuroraBoot-served ISOs. */}
      <Card className="mb-6">
        <CardContent className="p-5 space-y-4">
          <div>
            <h2 className="text-sm font-semibold">Image source</h2>
            <p className="text-xs text-muted-foreground mt-0.5">
              Where BMCs pull the install ISO from. A per-BMC override (below)
              takes precedence over the global default; a per-deploy URL takes
              precedence over both.
            </p>
          </div>
          <form onSubmit={handleSaveImageSource} className="space-y-4">
            <div className="space-y-1">
              <Label className="text-xs">Default image URL</Label>
              <Input
                value={imgDefaultURL}
                onChange={(e) => setImgDefaultURL(e.target.value)}
                placeholder="https://images.example.com/kairos.iso"
              />
              <p className="text-xs text-muted-foreground">
                The URL every BMC pulls the ISO from unless it has its own
                override. Leave blank to require a per-BMC or per-deploy URL.
              </p>
            </div>

            {imgSource?.localServe.configured && (
              <div className="rounded-md border p-4 space-y-3">
                <label className="flex items-center gap-2 cursor-pointer text-sm font-medium">
                  <input
                    type="checkbox"
                    checked={imgSource.localServe.enabled}
                    onChange={(e) => handleToggleLocalServe(e.target.checked)}
                  />
                  {PRODUCT_NAME} serves the built artifact ISO
                </label>
                <p className="text-xs text-muted-foreground">
                  When enabled, deploys with no operator-supplied URL serve the
                  artifact's own ISO over a tokenized, BMC-reachable URL
                  {imgSource.localServe.usesTLS ? " (HTTPS)." : " (HTTP)."}
                </p>
                <div className="space-y-1">
                  <Label className="text-xs">Advertised URL</Label>
                  <Input
                    value={imgAdvertisedURL}
                    onChange={(e) => setImgAdvertisedURL(e.target.value)}
                    placeholder="http://10.0.0.5:8090"
                  />
                  <p className="text-xs text-muted-foreground">
                    The base URL the BMC reaches the served ISO at (the bind
                    address is fixed at launch via --redfish-serve-addr).
                  </p>
                </div>
              </div>
            )}

            {imgSource && !imgSource.localServe.configured && (
              <p className="text-xs text-muted-foreground">
                {PRODUCT_NAME}-served ISOs are unavailable: no{" "}
                <code className="font-mono">--redfish-serve-addr</code> was set
                at launch. Use a default image URL or per-BMC override instead.
              </p>
            )}

            <div className="flex justify-end">
              <Button type="submit" disabled={savingImgSource}>
                {savingImgSource && (
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                )}
                Save image source
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="flex items-center justify-center py-16 text-muted-foreground">
              Loading...
            </div>
          ) : targets.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
              <ServerCog className="h-12 w-12 mb-3 text-muted-foreground/30" />
              <div className="text-center">
                <p className="font-medium text-foreground">
                  No BMCs registered yet
                </p>
                <p className="text-sm mt-1">
                  Add a baseboard management controller to deploy artifacts over
                  RedFish.
                </p>
              </div>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Endpoint</TableHead>
                  <TableHead>Vendor</TableHead>
                  <TableHead>System ID</TableHead>
                  <TableHead>Image source</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>TLS verify</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {targets.map((t) => {
                  const result = inspectResults[t.id];
                  const error = inspectErrors[t.id];
                  const busy = inspectingId === t.id;
                  return (
                    <Fragment key={t.id}>
                      <TableRow>
                        <TableCell className="font-medium">{t.name}</TableCell>
                        <TableCell className="font-mono text-xs">
                          {t.endpoint}
                        </TableCell>
                        <TableCell>
                          {(() => {
                            const p = profiles.find(
                              (pr) => pr.name === t.vendor
                            );
                            return (
                              <span className="flex items-center gap-2">
                                {t.vendor}
                                {p ? (
                                  <Badge
                                    variant={tierVariant(p.tier)}
                                    className="px-1 py-0 text-[10px]"
                                    title={p.tierDescription}
                                  >
                                    Tier {p.tier}
                                  </Badge>
                                ) : (
                                  <Badge
                                    variant="outline"
                                    className="px-1 py-0 text-[10px]"
                                    title="No matching profile loaded; falls back to generic"
                                  >
                                    generic fallback
                                  </Badge>
                                )}
                              </span>
                            );
                          })()}
                        </TableCell>
                        <TableCell>
                          {t.systemId ? (
                            <span className="font-mono text-xs">{t.systemId}</span>
                          ) : (
                            <span className="text-xs text-muted-foreground">
                              auto
                            </span>
                          )}
                        </TableCell>
                        <TableCell>
                          {(() => {
                            const badge = imageSourceBadge(t, imgSource);
                            return (
                              <Badge
                                variant={badge.variant}
                                title={t.imageUrl || undefined}
                              >
                                {badge.label}
                              </Badge>
                            );
                          })()}
                        </TableCell>
                        <TableCell>
                          {(() => {
                            const badge = statusBadge(t.lastStatus);
                            const checked = lastCheckedLabel(t);
                            const pinging = pingingId === t.id;
                            return (
                              <div className="flex items-center gap-1.5">
                                <Badge
                                  variant={badge.variant}
                                  title={t.lastError || undefined}
                                >
                                  {badge.label}
                                </Badge>
                                {checked && (
                                  <span className="text-xs text-muted-foreground">
                                    {checked}
                                  </span>
                                )}
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="h-6 w-6"
                                  aria-label={`Ping ${t.name}`}
                                  title="Reachability ping (no BMC session)"
                                  onClick={() => handlePing(t)}
                                  disabled={pinging}
                                >
                                  {pinging ? (
                                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                                  ) : (
                                    <Wifi className="h-3.5 w-3.5" />
                                  )}
                                </Button>
                              </div>
                            );
                          })()}
                        </TableCell>
                        <TableCell>
                          <Badge variant={t.verifySSL ? "default" : "secondary"}>
                            {t.verifySSL ? "Enabled" : "Disabled"}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center justify-end gap-1">
                            <Button
                              variant="outline"
                              size="sm"
                              className="gap-1.5 text-xs"
                              onClick={() => handleInspect(t)}
                              disabled={busy}
                            >
                              {busy ? (
                                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                              ) : (
                                <Search className="h-3.5 w-3.5" />
                              )}
                              Inspect
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8"
                              aria-label={`Edit ${t.name}`}
                              onClick={() => openEdit(t)}
                            >
                              <Pencil className="h-4 w-4" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8 text-destructive hover:text-destructive"
                              aria-label={`Delete ${t.name}`}
                              onClick={() => setDeleteTarget(t)}
                            >
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>

                      {/* Inspect error row */}
                      {error && (
                        <TableRow>
                          <TableCell colSpan={8} className="pt-0">
                            <div className="bg-red-500/10 border border-red-500/25 text-red-700 rounded-md p-3 text-sm whitespace-pre-wrap">
                              {error}
                            </div>
                          </TableCell>
                        </TableRow>
                      )}

                      {/* Inspect result row — same rendering style as DeployDialog */}
                      {result && (
                        <TableRow>
                          <TableCell colSpan={8} className="pt-0">
                            <div className="rounded-md border p-4 space-y-3">
                              <div className="text-sm font-medium">
                                Hardware inspection
                              </div>
                              <div className="grid grid-cols-2 gap-2 text-xs max-w-md">
                                <div className="text-muted-foreground">Model</div>
                                <div className="font-mono">
                                  {result.model || "-"}
                                </div>
                                <div className="text-muted-foreground">
                                  Manufacturer
                                </div>
                                <div className="font-mono">
                                  {result.manufacturer || "-"}
                                </div>
                                <div className="text-muted-foreground">Serial</div>
                                <div className="font-mono">
                                  {result.serialNumber || "-"}
                                </div>
                                <div className="text-muted-foreground flex items-center gap-1">
                                  <MemoryStick className="h-3.5 w-3.5" /> Memory
                                </div>
                                <div className="font-mono">
                                  {result.memoryGiB} GiB
                                </div>
                                <div className="text-muted-foreground flex items-center gap-1">
                                  <Cpu className="h-3.5 w-3.5" /> Processors
                                </div>
                                <div className="font-mono">
                                  {result.processorCount}
                                </div>
                              </div>
                              {result.supportedFeatures.length > 0 && (
                                <div className="space-y-1">
                                  <div className="text-xs text-muted-foreground">
                                    Supported features
                                  </div>
                                  <div className="flex flex-wrap gap-1">
                                    {result.supportedFeatures.map((f) => (
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
                            </div>
                          </TableCell>
                        </TableRow>
                      )}
                    </Fragment>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Add / Edit modal */}
      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{editing ? "Edit BMC" : "Add BMC"}</DialogTitle>
            <DialogDescription>
              {editing
                ? "Update the connection details for this baseboard management controller."
                : "Register a baseboard management controller for RedFish deployments."}
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <Label className="text-xs">Name</Label>
                <Input
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder="my-server"
                  required
                />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">Endpoint</Label>
                <Input
                  value={form.endpoint}
                  onChange={(e) =>
                    setForm({ ...form, endpoint: e.target.value })
                  }
                  placeholder="https://10.0.0.1"
                  required
                />
              </div>
              <div className="space-y-1">
                <Label className="text-xs">Vendor profile</Label>
                <Select
                  value={form.vendor}
                  onValueChange={(v) => setForm({ ...form, vendor: v })}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {vendorOptions.map((p) => (
                      <SelectItem key={p.name} value={p.name}>
                        <span className="flex items-center gap-2">
                          {p.name}
                          <Badge
                            variant={tierVariant(p.tier)}
                            className="px-1 py-0 text-[10px]"
                          >
                            Tier {p.tier}
                          </Badge>
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {(() => {
                  const sel = vendorOptions.find((p) => p.name === form.vendor);
                  if (!sel) {
                    return (
                      <p className="text-[11px] text-muted-foreground">
                        No profile named “{form.vendor}” is loaded — this BMC will
                        use the spec-default <code>generic</code> behaviour.
                      </p>
                    );
                  }
                  return (
                    <p className="text-[11px] text-muted-foreground">
                      {sel.tierDescription}
                      {sel.validatedFirmware ? ` · ${sel.validatedFirmware}` : ""}
                      {sel.origin === "operator" ? " · operator-supplied" : ""}
                    </p>
                  );
                })()}
              </div>
              <div className="space-y-1">
                <Label className="text-xs">Username</Label>
                <Input
                  value={form.username}
                  onChange={(e) =>
                    setForm({ ...form, username: e.target.value })
                  }
                  required
                />
              </div>
              <div className="space-y-1 col-span-2">
                <Label className="text-xs">Password</Label>
                <Input
                  type="password"
                  value={form.password}
                  onChange={(e) =>
                    setForm({ ...form, password: e.target.value })
                  }
                  placeholder={
                    editing ? "Leave blank to keep existing" : undefined
                  }
                  // Password is required on create only; on edit a blank value
                  // keeps the stored credential.
                  required={!editing}
                />
                {editing && (
                  <p className="text-xs text-muted-foreground">
                    Leave blank to keep the existing password.
                  </p>
                )}
              </div>
              <div className="space-y-1 col-span-2">
                <Label className="text-xs">System ID</Label>
                <Input
                  value={form.systemId}
                  onChange={(e) =>
                    setForm({ ...form, systemId: e.target.value })
                  }
                  placeholder="optional"
                />
                <p className="text-xs text-muted-foreground">
                  Leave blank for single-system BMCs; required when the BMC
                  exposes multiple ComputerSystems.
                </p>
              </div>
              <div className="space-y-1 col-span-2">
                <Label className="text-xs">Image URL override</Label>
                <Input
                  value={form.imageUrl}
                  onChange={(e) =>
                    setForm({ ...form, imageUrl: e.target.value })
                  }
                  placeholder="https://images.example.com/kairos.iso"
                />
                <p className="text-xs text-muted-foreground">
                  URL the BMC pulls the ISO from; overrides the global default.
                  Leave blank to use the global default image source.
                </p>
              </div>
            </div>
            <label className="flex items-center gap-2 cursor-pointer text-sm">
              <input
                type="checkbox"
                checked={form.verifySSL}
                onChange={(e) =>
                  setForm({ ...form, verifySSL: e.target.checked })
                }
              />
              Verify TLS certificate
            </label>
            <div className="space-y-1">
              <label className="flex items-center gap-2 cursor-pointer text-sm">
                <input
                  type="checkbox"
                  checked={form.ejectAfterInstall}
                  onChange={(e) =>
                    setForm({ ...form, ejectAfterInstall: e.target.checked })
                  }
                />
                Eject media after install
              </label>
              <p className="text-xs text-muted-foreground">
                When the freshly-installed node phones home, {PRODUCT_NAME} ejects the
                virtual media and boots from disk — breaking the install loop on
                BMCs that ignore the one-time boot override. Opt-in.
              </p>
            </div>
            <div className="space-y-1">
              <label className="flex items-center gap-2 cursor-pointer text-sm">
                <input
                  type="checkbox"
                  checked={form.ejectPowerCycle}
                  onChange={(e) =>
                    setForm({ ...form, ejectPowerCycle: e.target.checked })
                  }
                />
                Power-cycle on eject
              </label>
              <p className="text-xs text-muted-foreground">
                Power the machine off before ejecting and back on after — needed for
                BMCs/emulators that don't apply media eject to a running machine.
                Leave off for hardware that ejects live.
              </p>
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => setFormOpen(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={saving}>
                {saving && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                {editing ? "Save changes" : "Add BMC"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title="Delete BMC"
        description={
          deleteTarget
            ? `Delete BMC "${deleteTarget.name}" (${deleteTarget.endpoint})? This cannot be undone.`
            : ""
        }
        confirmLabel="Delete"
        destructive
        onConfirm={handleDelete}
      />
    </div>
  );
}
