/// <reference types="vitest/config" />
import { fileURLToPath } from "node:url";
import path from "node:path";
import { defineConfig, loadEnv, type Plugin, type ViteDevServer } from "vite";
import react from "@vitejs/plugin-react";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const defaultVitePort = 3917;

function strictPortMessage(port: number): Plugin {
  const msg = `Vite port ${port} is already in use. Set VITE_PORT to a free port (default ${defaultVitePort}; 5173/5174/5191 collide on the owner's PC).`;
  const attach = (server: ViteDevServer) => {
    server.httpServer?.on("error", (err: NodeJS.ErrnoException) => {
      if (err.code === "EADDRINUSE") {
        server.config.logger.error(msg);
      }
    });
  };
  return {
    name: "doctalk-strict-port-message",
    configureServer(server) {
      attach(server);
      return () => attach(server);
    },
  };
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, repoRoot, "");
  const apiPort = env.API_PORT || "8181";
  const vitePort = Number(env.VITE_PORT || defaultVitePort);
  const apiUrl = env.VITE_API_URL || `http://localhost:${apiPort}`;

  return {
    envDir: repoRoot,
    plugins: [react(), strictPortMessage(vitePort)],
    server: {
      host: "127.0.0.1",
      port: vitePort,
      strictPort: true,
      proxy: {
        "/health": { target: apiUrl, changeOrigin: true, cookieDomainRewrite: "" },
        "/documents": { target: apiUrl, changeOrigin: true, cookieDomainRewrite: "" },
        "/ask": { target: apiUrl, changeOrigin: true, cookieDomainRewrite: "" },
        "/quiz": { target: apiUrl, changeOrigin: true, cookieDomainRewrite: "" },
        "/quota": { target: apiUrl, changeOrigin: true, cookieDomainRewrite: "" },
      },
    },
    test: {
      environment: "node",
    },
  };
});
