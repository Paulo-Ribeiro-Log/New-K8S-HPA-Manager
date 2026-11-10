import { defineConfig } from "vitest/config";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Testes da lógica pura do frontend (sem navegador): `npm test`. Config separada do vite.config.ts de
// propósito — aquele carrega o plugin do Monaco e o tagger, pesados e inúteis para testar funções.
// Um arquivo que precisa de DOM/localStorage declara `// @vitest-environment jsdom` na primeira linha.
export default defineConfig({
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
    restoreMocks: true,
  },
});
