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
    // Build para ser embedado no Go binary
    outDir: "../static",
    emptyOutDir: true,
    // Gerar sourcemaps sempre para debug
    sourcemap: true,
    // Não inline assets para debug
    assetsInlineLimit: 0,
    // Target ES2020 para compatibilidade
    target: 'es2020',
    // Minify false para debug
    minify: false,
  },
}));
