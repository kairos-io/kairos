import { useEffect, useState } from "react";
import { Link } from "react-router";
import { ListChecks } from "lucide-react";
import { listBatches, type Batch } from "@/api/batches";
import { StatusBadge } from "@/components/StatusBadge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { commandLabel, phaseStatus } from "@/lib/batch";
import { timeAgo } from "@/lib/time";

const RECENT_LIMIT = 5;

// RecentBatches lists the newest batches of a group or a node. List results
// carry no node rows, so a node's entries show the batch phase.
export function RecentBatches({ groupID, nodeID }: { groupID?: string; nodeID?: string }) {
  const [batches, setBatches] = useState<Batch[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    listBatches({ groupID, nodeID, limit: RECENT_LIMIT })
      .then((res) => {
        if (!live) return;
        setBatches(res.batches);
        setError(null);
      })
      .catch((err) => {
        if (live) setError((err as Error).message);
      });
    return () => {
      live = false;
    };
  }, [groupID, nodeID]);

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <CardTitle className="flex items-center gap-2 text-sm font-medium">
          <ListChecks className="h-4 w-4" aria-hidden="true" />
          Batches
        </CardTitle>
        <Link to="/batches" className="text-sm text-muted-foreground hover:underline">
          View all
        </Link>
      </CardHeader>
      <CardContent>
        {error ? (
          <p className="text-sm text-danger-foreground">Failed to load batches: {error}</p>
        ) : batches === null ? null : batches.length === 0 ? (
          <p className="text-sm text-muted-foreground">No batches yet.</p>
        ) : (
          <ul className="divide-y">
            {batches.map((b) => (
              <li key={b.id} className="flex items-center justify-between gap-3 py-2 first:pt-0 last:pb-0">
                <div className="min-w-0">
                  <Link to={`/batches/${b.id}`} className="text-sm font-medium hover:underline">
                    {commandLabel(b.command)}
                  </Link>
                  <p className="truncate text-xs text-muted-foreground">{b.selection}</p>
                </div>
                <div className="flex shrink-0 flex-col items-end gap-1">
                  <StatusBadge status={phaseStatus(b.phase)} label={b.phase} />
                  <span
                    className="text-xs text-muted-foreground"
                    title={new Date(b.createdAt).toLocaleString()}
                  >
                    {timeAgo(b.createdAt)}
                  </span>
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
