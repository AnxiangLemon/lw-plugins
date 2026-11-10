import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    port: 5178,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8766",
        changeOrigin: true,
        // 仅开发代理：保持 Go 的同源校验，生产环境仍是 Go 单进程同源服务。
        configure(proxy) {
          proxy.on("proxyReq", (request) => {
            if (request.getHeader("Origin"))
              request.setHeader("Origin", "http://127.0.0.1:8766");
          });
        },
      },
    },
  },
  build: { target: "es2022", sourcemap: false, chunkSizeWarningLimit: 650 },
  test: {
    environment: "jsdom",
    setupFiles: ["./tests/setup.ts"],
    include: ["tests/**/*.test.{ts,tsx}"],
  },
});
