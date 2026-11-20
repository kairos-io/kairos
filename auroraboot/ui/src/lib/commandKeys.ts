// The commands CommandDialog has a form for. Batches can carry other
// commands, such as the extension installs InstallExtensionDialog starts.
export const commandKeys = [
  "upgrade",
  "upgrade-recovery",
  "reset",
  "apply-cloud-config",
  "reboot",
  "exec",
] as const;

export type CommandKey = (typeof commandKeys)[number];

export function isCommandKey(command: string): command is CommandKey {
  return (commandKeys as readonly string[]).includes(command);
}
