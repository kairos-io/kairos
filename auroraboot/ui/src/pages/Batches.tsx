import { useEffect, useState } from "react";
import { Link } from "react-router";
import { ListChecks } from "lucide-react";
import { listBatches, type Batch } from "@/api/batches";
import { PageHeader } from "@/components/PageHeader";
import { StatusBadge } from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useArtifactNames } from "@/hooks/useArtifactNames";
import {
  commandDetails,
  commandLabel,
  commandSummary,
  countsText,
  phaseStatus,
  targetsText,
} from "@/lib/batch";
import { timeAgo } from "@/lib/time";

const PAGE_SIZE = 50;

export function Batches() {
  const [batches, setBatches] = useState<Batch[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const artifactName = useArtifactNames();

  useEffect(() => {
    let live = true;
    listBatches({ limit: PAGE_SIZE, offset })
      .then((res) => {
        if (!live) return;
        setBatches(res.batches);
        setTotal(res.total);
        setError(null);
      })
      .catch((err) => {
        if (live) setError((err as Error).message);
      })
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => {
      live = false;
    };
  }, [offset]);

  return (
    <div>
      <PageHeader
        title="Batches"
        description="Commands rolled out to several nodes, newest first"
      />
      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="flex items-center justify-center py-16 text-muted-foreground">
              Loading...
            </div>
          ) : error ? (
            <div className="py-16 text-center text-sm text-danger-foreground">
              Failed to load batches: {error}
            </div>
          ) : batches.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
              <ListChecks className="h-12 w-12 mb-3 text-muted-foreground/30" />
              <p className="font-medium text-foreground">No batches yet</p>
              <p className="text-sm mt-1">
                Send a command to a group or to several nodes to start one.
              </p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Command</TableHead>
                  <TableHead>Targets</TableHead>
                  <TableHead>Phase</TableHead>
                  <TableHead>Nodes</TableHead>
                  <TableHead>Started</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {batches.map((b) => {
                  const summary = commandSummary(
                    commandDetails(b.command, b.args, artifactName),
                  );
                  const targets = targetsText(b);
                  return (
                    <TableRow key={b.id}>
                      <TableCell className="max-w-xs">
                        <Link
                          to={`/batches/${b.id}`}
                          className="font-medium hover:underline"
                        >
                          {commandLabel(b.command)}
                        </Link>
                        {summary && (
                          <div
                            className="truncate font-mono text-xs text-muted-foreground"
                            title={summary}
                          >
                            {summary}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className="max-w-xs">
                        <div>{b.selection}</div>
                        {targets && (
                          <div
                            className="truncate text-xs text-muted-foreground"
                            title={targets}
                          >
                            {targets}
                          </div>
                        )}
                      </TableCell>
                      <TableCell>
                        <StatusBadge
                          status={phaseStatus(b.phase)}
                          label={b.phase}
                        />
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {countsText(b)}
                      </TableCell>
                      <TableCell
                        className="text-xs text-muted-foreground"
                        title={new Date(b.createdAt).toLocaleString()}
                      >
                        {timeAgo(b.createdAt)}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
      {total > PAGE_SIZE && (
        <div className="mt-4 flex items-center justify-between text-sm text-muted-foreground">
          <span>
            {offset + 1} to {Math.min(offset + PAGE_SIZE, total)} of {total}
          </span>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
            >
              Previous
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={offset + PAGE_SIZE >= total}
              onClick={() => setOffset(offset + PAGE_SIZE)}
            >
              Next
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
