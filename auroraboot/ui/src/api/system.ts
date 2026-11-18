import { apiFetch } from "./client";

// SystemBuilder mirrors the server's GET /api/v1/system/builder response:
// which build backend is active and enough context for the UI to render a
// "Builds via cluster X" badge. cluster and namespace are empty on the
// local backend.
export interface SystemBuilder {
  backend: "local" | "operator";
  cluster?: string;
  namespace?: string;
  downloadSupported: boolean;
}

export function getSystemBuilder(): Promise<SystemBuilder> {
  return apiFetch<SystemBuilder>("/api/v1/system/builder");
}
