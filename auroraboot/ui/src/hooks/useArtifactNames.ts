import { useCallback, useEffect, useState } from "react";
import { listArtifacts } from "@/api/artifacts";

// useArtifactNames returns a lookup from artifact ID to artifact name. It
// returns undefined until the artifacts load, for an unnamed artifact, and
// when the list could not be fetched, so callers fall back to the ID.
export function useArtifactNames(): (id: string) => string | undefined {
  const [names, setNames] = useState<Map<string, string>>(new Map());

  useEffect(() => {
    let live = true;
    listArtifacts()
      .then((artifacts) => {
        if (!live) return;
        setNames(new Map(artifacts.filter((a) => a.name).map((a) => [a.id, a.name as string])));
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, []);

  return useCallback((id: string) => names.get(id), [names]);
}
