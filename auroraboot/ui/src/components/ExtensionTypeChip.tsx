import type { ExtensionType } from "@/api/extensions";

const STYLES: Record<ExtensionType, string> = {
  sysext: "border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300",
  confext: "border-violet-500/30 bg-violet-500/10 text-violet-700 dark:text-violet-300",
};

export function ExtensionTypeChip({ type }: { type: ExtensionType }) {
  return (
    <span
      className={`inline-flex items-center text-[11px] font-medium px-2 py-0.5 rounded-full border ${STYLES[type]}`}
    >
      {type}
    </span>
  );
}
