import { useState, useCallback, useEffect, useRef } from "react";
import { cn } from "@/lib/utils";
import { useToastListener, type Toast } from "@/hooks/useToast";
import { X } from "lucide-react";

const typeStyles: Record<Toast["type"], string> = {
  success:
    "bg-green-600 text-white",
  error:
    "bg-red-600 text-white",
  info:
    "bg-zinc-800 text-zinc-100",
};

export function Toaster() {
  const [toasts, setToasts] = useState<Toast[]>([]);
  // Pending hide timers, cleared on unmount so none fires after the toaster
  // is gone.
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());

  const handleToast = useCallback((toast: Toast) => {
    setToasts((prev) => [...prev, toast]);
    const timer = setTimeout(() => {
      timers.current.delete(timer);
      setToasts((prev) => prev.filter((t) => t.id !== toast.id));
    }, 3000);
    timers.current.add(timer);
  }, []);

  useEffect(() => {
    const pending = timers.current;
    return () => {
      pending.forEach(clearTimeout);
      pending.clear();
    };
  }, []);

  useToastListener(handleToast);

  function dismiss(id: string) {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }

  if (toasts.length === 0) return null;

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={cn(
            "flex items-center gap-3 rounded-lg px-4 py-3 text-sm font-medium shadow-lg",
            "animate-in slide-in-from-right-full fade-in duration-200",
            typeStyles[t.type]
          )}
        >
          <span className="flex-1">{t.message}</span>
          <button
            onClick={() => dismiss(t.id)}
            className="shrink-0 rounded p-0.5 opacity-70 hover:opacity-100 transition-opacity"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      ))}
    </div>
  );
}
