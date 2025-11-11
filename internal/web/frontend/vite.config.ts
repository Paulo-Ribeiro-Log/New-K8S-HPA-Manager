import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import path from "path";
import { fileURLToPath } from "url";
import { componentTagger } from "lovable-tagger";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => ({
  server: {
    host: "::",
    port: 5173, // Vite dev server (separado do Go backend em 8080)
    proxy: {
      // Proxy API requests para o backend Go durante desenvolvimento
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  plugins: [react(), mode === "development" && componentTagger()].filter(Boolean),
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    // Build para ser embedado e copiado para o Go
    outDir: "dist",
    emptyOutDir: true,
    // Gerar sourcemaps apenas em dev
    sourcemap: mode === "development",
    // Assets inline para facilitar embed
    assetsInlineLimit: 4096,
  },
}));
