import { useEffect, useState, type FormEvent } from "react";
import {
  getExtensionCatalogSettings,
  updateExtensionCatalogSettings,
  type ExtensionCatalogSettings,
} from "@/api/settings";
import { SUGGESTED_EXTENSION_CATALOGS } from "@/lib/catalogExtensions";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Plus, Trash2 } from "lucide-react";

// ExtensionCatalogsCard manages the extension catalogs the Artifact Builder
// offers. Hadron builds always have the hadron-layers catalog; every other
// flavor only has the catalogs listed here.
export function ExtensionCatalogsCard() {
  const [catalogs, setCatalogs] = useState<ExtensionCatalogSettings>({ launch: [], saved: [] });
  const [draft, setDraft] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    getExtensionCatalogSettings()
      .then(setCatalogs)
      .catch(() => {});
  }, []);

  async function save(saved: string[]) {
    setSaving(true);
    setError("");
    try {
      setCatalogs(await updateExtensionCatalogSettings(saved));
      return true;
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      return false;
    } finally {
      setSaving(false);
    }
  }

  async function handleAdd(e: FormEvent) {
    e.preventDefault();
    const url = draft.trim();
    if (url === "") return;
    if (await save([...catalogs.saved, url])) setDraft("");
  }

  const known = new Set([...catalogs.launch, ...catalogs.saved]);
  const suggestions = SUGGESTED_EXTENSION_CATALOGS.filter((s) => !known.has(s.url));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium">Extension Catalogs</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4">
        <p className="text-sm text-muted-foreground">
          The Artifact Builder offers these catalogs for system extensions. Hadron
          builds always have the Kairos hadron-layers catalog. Other flavors only
          have the catalogs listed here.
        </p>

        {catalogs.launch.length + catalogs.saved.length === 0 ? (
          <p className="text-sm text-muted-foreground italic">No catalog is configured.</p>
        ) : (
          <ul className="grid gap-2">
            {catalogs.launch.map((url) => (
              <li key={`launch-${url}`} className="flex items-center gap-2 rounded-md border p-2">
                <span className="font-mono text-xs truncate flex-1" title={url}>
                  {url}
                </span>
                <Badge variant="secondary" title="Given with --extensions-catalog at launch">
                  launch flag
                </Badge>
              </li>
            ))}
            {catalogs.saved.map((url) => (
              <li key={`saved-${url}`} className="flex items-center gap-2 rounded-md border p-2">
                <span className="font-mono text-xs truncate flex-1" title={url}>
                  {url}
                </span>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  disabled={saving}
                  aria-label={`Remove ${url}`}
                  onClick={() => save(catalogs.saved.filter((u) => u !== url))}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </li>
            ))}
          </ul>
        )}

        <form className="flex gap-2" onSubmit={handleAdd}>
          <Input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="https://example.com/extensions/releases.json"
            className="font-mono text-xs"
            aria-label="New extension catalog URL"
          />
          <Button type="submit" variant="outline" disabled={saving || draft.trim() === ""}>
            <Plus className="h-4 w-4 mr-2" />
            Add
          </Button>
        </form>
        {error && <p className="text-sm text-destructive">{error}</p>}

        {suggestions.length > 0 && (
          <div className="grid gap-2">
            <div className="text-xs text-muted-foreground">Suggested catalogs</div>
            {suggestions.map((s) => (
              <div key={s.url} className="flex items-start gap-2 rounded-md border border-dashed p-2">
                <div className="grid gap-0.5 min-w-0 flex-1">
                  <span className="text-xs font-medium">{s.name}</span>
                  <span className="font-mono text-xs text-muted-foreground truncate">{s.url}</span>
                  <span className="text-xs text-muted-foreground">{s.description}</span>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={saving}
                  aria-label={`Add ${s.name}`}
                  onClick={() => save([...catalogs.saved, s.url])}
                >
                  Add
                </Button>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
