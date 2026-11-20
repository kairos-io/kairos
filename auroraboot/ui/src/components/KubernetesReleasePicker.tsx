import { useCallback, useState } from "react";
import { Download, Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  fetchKubernetesDistroReleases,
  type KubernetesDistro,
} from "@/lib/githubReleases";

type Props = {
  distro: KubernetesDistro;
  onSelect: (tag: string) => void;
  limit?: number;
};

export function KubernetesReleasePicker({ distro, onSelect, limit = 5 }: Props) {
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [tags, setTags] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await fetchKubernetesDistroReleases(distro, limit);
      setTags(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to fetch releases.");
    } finally {
      setLoading(false);
    }
  }, [distro, limit]);

  const handleOpenChange = (next: boolean) => {
    setOpen(next);
    if (next && tags === null && !loading) {
      void load();
    }
  };

  return (
    <DropdownMenu open={open} onOpenChange={handleOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button type="button" variant="outline" size="sm">
          {loading ? (
            <Loader2 className="h-4 w-4 mr-2 animate-spin" />
          ) : (
            <Download className="h-4 w-4 mr-2" />
          )}
          Fetch releases
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-[16rem]">
        <DropdownMenuLabel>
          Latest {distro.toUpperCase()} releases
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {loading && (
          <div className="flex items-center gap-2 px-2 py-2 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            Fetching from GitHub
          </div>
        )}
        {!loading && error && (
          <div className="px-2 py-2 space-y-2">
            <p className="text-sm text-destructive">{error}</p>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => void load()}
            >
              Retry
            </Button>
          </div>
        )}
        {!loading && !error && tags && tags.length === 0 && (
          <div className="px-2 py-2 text-sm text-muted-foreground">
            No releases found.
          </div>
        )}
        {!loading &&
          !error &&
          tags &&
          tags.length > 0 &&
          tags.map((tag) => (
            <DropdownMenuItem
              key={tag}
              onSelect={() => {
                onSelect(tag);
                setOpen(false);
              }}
              className="font-mono text-xs"
            >
              {tag}
            </DropdownMenuItem>
          ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
