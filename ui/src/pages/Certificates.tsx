import { useEffect, useRef, useState } from "react";
import { PageHeader } from "@/components/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Trash2, KeyRound, Loader2, Download, FileUp } from "lucide-react";
import {
  listSecureBootKeySets,
  generateSecureBootKeys,
  deleteSecureBootKeySet,
  exportSecureBootKeySet,
  importSecureBootKeySet,
  type SecureBootKeySet,
} from "@/api/artifacts";
import { toast } from "@/hooks/useToast";

export function Certificates() {
  const [keySets, setKeySets] = useState<SecureBootKeySet[]>([]);
  const [loading, setLoading] = useState(true);
  const [generating, setGenerating] = useState(false);
  const [name, setName] = useState("");
  const [confirmState, setConfirmState] = useState<{ open: boolean; action: () => void }>({ open: false, action: () => {} });
  const [importing, setImporting] = useState(false);
  const importInputRef = useRef<HTMLInputElement>(null);

  async function handleExport(ks: SecureBootKeySet) {
    try {
      const { blob, filename } = await exportSecureBootKeySet(ks.id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      a.click();
      URL.revokeObjectURL(url);
      toast(`Exported ${filename}`, "success");
    } catch (err) {
      toast(`Export failed: ${(err as Error).message}`, "error");
    }
  }

  async function handleImportFile(ev: React.ChangeEvent<HTMLInputElement>) {
    const file = ev.target.files?.[0];
    if (ev.target) ev.target.value = "";
    if (!file) return;
    setImporting(true);
    try {
      const ks = await importSecureBootKeySet(file);
      toast(`Imported key set "${ks.name}"`, "success");
      fetchKeySets();
    } catch (err) {
      // Prompt for a rename if the server reported a name collision.
      const msg = (err as Error).message || "";
      if (msg.includes("already exists")) {
        const override = window.prompt(
          "A key set with this name already exists. Enter a new name to import it as:",
          "",
        );
        if (override && override.trim()) {
          try {
            const ks = await importSecureBootKeySet(file, override.trim());
            toast(`Imported key set "${ks.name}"`, "success");
            fetchKeySets();
          } catch (err2) {
            toast(`Import failed: ${(err2 as Error).message}`, "error");
          }
        }
      } else {
        toast(`Import failed: ${msg}`, "error");
      }
    } finally {
      setImporting(false);
    }
  }

  function fetchKeySets() {
    listSecureBootKeySets()
      .then(setKeySets)
      .catch(() => {})
      .finally(() => setLoading(false));
  }

  useEffect(() => {
    fetchKeySets();
  }, []);

  async function handleGenerate(e: React.FormEvent) {
    e.preventDefault();
    setGenerating(true);
    try {
      await generateSecureBootKeys(name);
      setName("");
      fetchKeySets();
    } catch (err) {
      toast(`Generate failed: ${(err as Error).message}`, "error");
    } finally {
      setGenerating(false);
    }
  }

  async function handleDelete(id: string) {
    setConfirmState({
      open: true,
      action: async () => {
        try {
          await deleteSecureBootKeySet(id);
          fetchKeySets();
        } catch (err) {
          toast(`Remove failed: ${(err as Error).message}`, "error");
        }
      },
    });
  }

  return (
    <div>
      <PageHeader
        title="SecureBoot keys"
        description="Key sets used to sign UKI images and enroll Trusted Boot."
      >
        <input
          ref={importInputRef}
          type="file"
          accept=".tar.gz,.tgz,application/gzip,application/x-gzip"
          className="hidden"
          onChange={handleImportFile}
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={importing}
          onClick={() => importInputRef.current?.click()}
        >
          {importing ? (
            <Loader2 className="h-4 w-4 mr-2 animate-spin" />
          ) : (
            <FileUp className="h-4 w-4 mr-2" />
          )}
          Import
        </Button>
      </PageHeader>

      {/* Generate new key set — compact inline form, not a hero */}
      <div className="mb-6 rounded-lg border bg-card px-4 py-3">
        <form onSubmit={handleGenerate} className="flex items-center gap-3 flex-wrap">
          <KeyRound className="h-4 w-4 text-muted-foreground" />
          <Label className="text-sm font-medium shrink-0">Generate new key set</Label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Name, e.g. production"
            required
            className="h-9 max-w-xs"
          />
          <Button
            type="submit"
            size="sm"
            disabled={generating || !name.trim()}
            className="disabled:opacity-60"
          >
            {generating && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
            Generate
          </Button>
          <p className="text-xs text-muted-foreground ml-auto max-w-[28rem]">
            Creates PK, KEK and db key pairs plus a TPM PCR policy key.
          </p>
        </form>
      </div>

      {/* Key sets table */}
      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="flex items-center justify-center py-16 text-muted-foreground">
              Loading…
            </div>
          ) : keySets.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-muted-foreground">
              <KeyRound className="h-12 w-12 mb-3 text-muted-foreground/30" />
              <p className="font-medium text-foreground">No key sets yet</p>
              <p className="text-sm mt-1 max-w-sm text-center">
                Generate your first key set above to enable signed UKI builds and Trusted Boot.
              </p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Contents</TableHead>
                  <TableHead>Enroll mode</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-24" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {keySets.map((ks) => (
                  <TableRow key={ks.id} className="hover:bg-muted/40 transition-colors">
                    <TableCell className="font-medium">
                      <div className="flex items-center gap-2">
                        <KeyRound className="h-4 w-4 text-primary" />
                        {ks.name}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        <span className="inline-flex items-center gap-1 rounded border border-border bg-muted/40 px-1.5 py-0.5 font-mono text-[10px]">
                          PK
                        </span>
                        <span className="inline-flex items-center gap-1 rounded border border-border bg-muted/40 px-1.5 py-0.5 font-mono text-[10px]">
                          KEK
                        </span>
                        <span className="inline-flex items-center gap-1 rounded border border-border bg-muted/40 px-1.5 py-0.5 font-mono text-[10px]">
                          db
                        </span>
                        {ks.hasTpmPcrKey && (
                          <span className="inline-flex items-center gap-1 rounded border border-border bg-muted/40 px-1.5 py-0.5 font-mono text-[10px]">
                            TPM
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs">
                      <span className="inline-flex items-center rounded-md border border-border bg-muted/40 px-2 py-0.5 font-mono text-[11px]">
                        {ks.secureBootEnroll || "if-safe"}
                      </span>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {new Date(ks.createdAt).toLocaleString()}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8"
                          title="Export"
                          onClick={() => handleExport(ks)}
                        >
                          <Download className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-red-500 hover:text-red-700"
                          title="Delete"
                          onClick={() => handleDelete(ks.id)}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <ConfirmDialog
        open={confirmState.open}
        onOpenChange={(open) => setConfirmState(prev => ({ ...prev, open }))}
        title="Remove Key Set"
        description="Remove this key set reference? Key files on disk are not deleted."
        confirmLabel="Remove"
        destructive
        onConfirm={confirmState.action}
      />
    </div>
  );
}
