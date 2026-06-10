import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig(({ mode }) => ({
  base: "./",
  plugins: [react()],
  resolve: {
    alias: [
      { find: /^@authman\/shared\/tokens\.css$/, replacement: path.resolve(__dirname, "src/shared/theme/tokens.css") },
      { find: /^@authman\/shared\/components\.css$/, replacement: path.resolve(__dirname, "src/shared/theme/components.css") },
      { find: /^@authman\/shared\/layout\.css$/, replacement: path.resolve(__dirname, "src/shared/theme/layout.css") },
      { find: /^@authman\/shared\/admin_sections\.css$/, replacement: path.resolve(__dirname, "src/shared/theme/admin_sections.css") },
      { find: /^@authman\/shared\/animations\.css$/, replacement: path.resolve(__dirname, "src/shared/theme/animations.css") },
      { find: /^@authman\/shared$/, replacement: path.resolve(__dirname, "src/shared/index.ts") },
    ],
  },
  server: {
    port: Number(process.env.CORE_PORT ?? process.env.ADMIN_PORT ?? 5173),
    strictPort: true,
    host: "127.0.0.1",
    proxy: process.env.AUTHMAN_API_PROXY
      ? {
          "/api": {
            target: process.env.AUTHMAN_API_PROXY,
            changeOrigin: true,
          },
        }
      : undefined,
  },
  preview: {
    port: Number(process.env.CORE_PREVIEW_PORT ?? process.env.ADMIN_PREVIEW_PORT ?? 4173),
    strictPort: true,
    host: "127.0.0.1",
  },
  define: {
    __APP_KIND__: JSON.stringify("core"),
    __BUILD_MODE__: JSON.stringify(mode),
  },
  build: {
    outDir: "dist",
    sourcemap: true,
  },
}));
