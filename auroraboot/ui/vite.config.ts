import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: "/",
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    outDir: "../internal/ui/dist",
    emptyOutDir: true,
  },
  server: {
    // Vite rejects requests whose Host header isn't recognized. Set
    // VITE_ALLOWED_HOSTS (comma-separated) to reach the dev server via a
    // LAN or mDNS name, e.g. VITE_ALLOWED_HOSTS=my-machine.local
    allowedHosts: process.env.VITE_ALLOWED_HOSTS?.split(",")
      .map((h) => h.trim())
      .filter((h) => h.length > 0),
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        // The browser's Origin is the dev server; keeping Host as the dev
        // server too lets the backend's Origin check accept it.
        ws: true,
      },
    },
  },
});
