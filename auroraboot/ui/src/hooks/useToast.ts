import { useEffect } from "react";

export type ToastType = "success" | "error" | "info";

export type Toast = {
  id: string;
  message: string;
  type: ToastType;
};

const listeners: Set<(toast: Toast) => void> = new Set();

export function toast(
  message: string,
  type: ToastType = "info"
) {
  const t: Toast = { id: Date.now().toString(), message, type };
  listeners.forEach((fn) => fn(t));
}

export function useToastListener(callback: (toast: Toast) => void) {
  useEffect(() => {
    listeners.add(callback);
    return () => {
      listeners.delete(callback);
    };
  }, [callback]);
}
