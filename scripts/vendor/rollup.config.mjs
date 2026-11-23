import { nodeResolve } from "@rollup/plugin-node-resolve";
import terser from "@rollup/plugin-terser";

export default {
    input: "editor.js",
    output: {
        dir: "dist",
        format: "es",
        entryFileNames: "editor.js",
        chunkFileNames: "[name]-[hash].js",
    },
    plugins: [nodeResolve(), terser()],
};
