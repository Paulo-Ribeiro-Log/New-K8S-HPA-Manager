import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist"] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
      "@typescript-eslint/no-unused-vars": "off",
      // Dívida de tipagem (centenas de usos): aviso, para a CI barrar só os erros novos de verdade.
      "@typescript-eslint/no-explicit-any": "warn",
    },
  },
  {
    // Arquivos em que componente e hook/utilitário convivem de propósito: shadcn/ui (código
    // gerado: buttonVariants etc.), providers com seu hook (padrão do React), stores e libs.
    // Em componentes comuns a regra continua valendo: helper vai para arquivo à parte.
    files: [
      "src/components/ui/**/*.{ts,tsx}",
      "src/components/theme-provider.tsx",
      "src/contexts/**/*.{ts,tsx}",
      "src/store/**/*.{ts,tsx}",
      "src/lib/**/*.{ts,tsx}",
    ],
    rules: {
      "react-refresh/only-export-components": "off",
    },
  },
);
