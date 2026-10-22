/// <reference types="vitest/config" />
import { fileURLToPath } from "node:url";
import path from "node:path";
import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, repoRoot, "");
  const apiPort = env.API_PORT || "8081";
  const vitePort = Number(env.VITE_PORT || "5174");
  const apiUrl = env.VITE_API_URL || `http://localhost:${apiPort}`;

  return {
    envDir: repoRoot,
    plugins: [react()],
    server: {
      port: vitePort,
      strictPort: true,
      proxy: {
        "/health": { target: apiUrl, changeOrigin: true },
        "/documents": { target: apiUrl, changeOrigin: true },
        "/ask": { target: apiUrl, changeOrigin: true },
        "/quiz": { target: apiUrl, changeOrigin: true },
      },
    },
    test: {
      environment: "node",
    },
  };
});
