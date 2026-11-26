import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["node_modules/", "coverage/"] },
  js.configs.recommended,
  tseslint.configs.strictTypeChecked,
  reactHooks.configs.flat.recommended,
  {
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // AGENTS.md: an `any` needs an inline disable with a reason comment.
      "@typescript-eslint/no-explicit-any": "error",
    },
  },
  {
    // Plain JavaScript (this file and the Node scripts) is not part of the
    // TypeScript project, so type-aware rules are turned off for it.
    files: ["**/*.js", "**/*.mjs"],
    extends: [tseslint.configs.disableTypeChecked],
    languageOptions: {
      globals: { console: "readonly", process: "readonly", URL: "readonly" },
    },
  },
);
