import { apiFetch } from "./client";

export interface Command {
  id: string;
  managedNodeID: string;
  command: string;
  args: Record<string, string>;
  phase: string;
  result: string;
  expiresAt: string | null;
  deliveredAt: string | null;
  completedAt: string | null;
  createdAt: string;
}

export function listNodeCommands(nodeID: string): Promise<Command[]> {
  return apiFetch<Command[]>(`/api/v1/nodes/${nodeID}/commands`);
}

export function deleteCommand(nodeID: string, commandID: string): Promise<void> {
  return apiFetch(`/api/v1/nodes/${nodeID}/commands/${commandID}`, { method: "DELETE" });
}

export function clearCommandHistory(nodeID: string): Promise<void> {
  return apiFetch(`/api/v1/nodes/${nodeID}/commands`, { method: "DELETE" });
}
