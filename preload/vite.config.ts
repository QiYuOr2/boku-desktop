import { builtinModules } from "node:module";

import { defineConfig } from "vite-plus";

const nodeBuiltins = [...builtinModules, ...builtinModules.map((name) => `node:${name}`)];

export default defineConfig({
  build: {
    target: "node22",
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: true,
    // sandbox: true 的 preload 必须是 CommonJS
    lib: {
      entry: { index: "src/index.ts" },
      formats: ["cjs"],
      fileName: () => "index.cjs",
    },
    rollupOptions: {
      external: ["electron", ...nodeBuiltins],
    },
  },
});
