import node from "rollup-plugin-node-resolve";
import commonjs from "rollup-plugin-commonjs";
import eslint from "rollup-plugin-eslint";
import cleanup from "rollup-plugin-cleanup";

export default [
    "sunburst",
].map(name => ({
    input: `assets/${name}.js`,
    output: {
        file: `assets/${name}.rollup.js`,
        format: "iife",
    },
    sourcemap: false,
    plugins: [
        eslint(),
        node(),
        commonjs(),
        cleanup({maxEmptyLines: 1}),
    ],
}));

