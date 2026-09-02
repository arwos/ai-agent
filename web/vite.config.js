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
        manualChunks(id) {
          if (!id.includes("node_modules")) return undefined;
          if (
            id.includes("react-markdown") ||
            id.includes("remark-") ||
            id.includes("mdast") ||
            id.includes("micromark")
          )
            return "markdown-vendor";
          if (id.includes("framer-motion")) return "motion-vendor";
          if (id.includes("lucide-react")) return "icons-vendor";
          if (id.includes("react-syntax-highlighter") || id.includes("refractor") || id.includes("prismjs"))
            return "syntax-vendor";
          if (id.includes("react-dom") || id.includes("/react/") || id.includes("scheduler")) return "react-vendor";
          return "vendor";
        },
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
