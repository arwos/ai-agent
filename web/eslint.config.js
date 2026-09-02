import eslint from "@eslint/js";
import { FlatCompat } from "@eslint/eslintrc";
import path from "node:path";
import { fileURLToPath } from "node:url";
import globals from "globals";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import importPlugin from "eslint-plugin-import";
import reactPlugin from "eslint-plugin-react";
import jsxA11y from "eslint-plugin-jsx-a11y";

const compat = new FlatCompat({
  baseDirectory: path.dirname(fileURLToPath(import.meta.url)),
});

export default tseslint.config(
  {
    ignores: ["dist/**", "node_modules/**", "eslint.config.js", "vite.config.js"],
  },
  eslint.configs.recommended,
  {
    plugins: { import: importPlugin, react: reactPlugin, "jsx-a11y": jsxA11y },
  },
  ...compat.extends("airbnb-typescript"),
  ...compat.extends("prettier"),
  reactHooks.configs.flat.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      globals: { ...globals.browser, ...globals.es2022 },
      parserOptions: { project: "./tsconfig.json", tsconfigRootDir: path.dirname(fileURLToPath(import.meta.url)) },
    },
    plugins: { import: importPlugin, react: reactPlugin, "jsx-a11y": jsxA11y, "react-refresh": reactRefresh },
    rules: {
      "@typescript-eslint/no-unused-vars": ["warn", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
      // Existing screens intentionally coordinate async state through effects;
      // these rules are too noisy for the current architecture.
      "react-hooks/immutability": "off",
      "react-hooks/set-state-in-effect": "off",
      "react-hooks/preserve-manual-memoization": "off",
      "react-hooks/static-components": "off",
      "@typescript-eslint/no-explicit-any": "off",
      "no-case-declarations": "off",
      "no-useless-assignment": "off",
      "@typescript-eslint/no-use-before-define": "off",
      "@typescript-eslint/no-shadow": "off",
      "@typescript-eslint/lines-between-class-members": "off",
      "import/extensions": "off",
      "padding-line-between-statements": [
        "warn",
        { blankLine: "always", prev: "*", next: "export" },
        { blankLine: "always", prev: "function", next: "function" },
      ],
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
    },
  },
);
