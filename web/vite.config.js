import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    // Keep source maps next to the hashed production assets so browser
    // diagnostics can resolve the original TypeScript/React sources.
    sourcemap: true,
    rollupOptions: {
      output: {
        // Keep the embedded SPA in one JavaScript asset. This prevents a
        // stale dynamically loaded chunk from surviving a page refresh.
        inlineDynamicImports: true,
      },
    },
  },
  server: {
    host: "0.0.0.0",
    port: 3000,
    strictPort: true,
    hmr: {
      port: 3000,
    },
  },
});
