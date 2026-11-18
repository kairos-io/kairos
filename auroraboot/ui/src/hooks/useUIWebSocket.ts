import { useEffect, useRef, useState } from "react";

interface WSMessage {
  type: string;
  data: unknown;
}

// useUIWebSocket connects to the admin UI WebSocket at /api/v1/ws/ui and
// fans each inbound message to `onMessage`. It auto-reconnects on drop
// (3s backoff) and returns a `connected` flag so callers can surface a
// "reconnecting…" state in the UI when the socket is mid-reconnect.
export function useUIWebSocket(onMessage: (msg: WSMessage) => void): { connected: boolean } {
  const wsRef = useRef<WebSocket | null>(null);
  const onMessageRef = useRef(onMessage);
  useEffect(() => {
    onMessageRef.current = onMessage;
  });
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/api/v1/ws/ui`;

    let ws: WebSocket;
    let reconnectTimer: ReturnType<typeof setTimeout>;
    let cancelled = false;

    function connect() {
      ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        if (!cancelled) setConnected(true);
      };

      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data) as WSMessage;
          onMessageRef.current(msg);
        } catch {
          // ignore malformed frames
        }
      };

      ws.onclose = () => {
        if (!cancelled) setConnected(false);
        reconnectTimer = setTimeout(connect, 3000);
      };
    }

    connect();

    return () => {
      cancelled = true;
      clearTimeout(reconnectTimer);
      ws?.close();
    };
  }, []);

  return { connected };
}
