import node from "rollup-plugin-node-resolve";
import commonjs from "rollup-plugin-commonjs";
import eslint from "rollup-plugin-eslint";
import cleanup from "rollup-plugin-cleanup";
import scss from "rollup-plugin-scss";

/* global process */
let isDev = process.env["ROLLUP_DEV"] && process.env["ROLLUP_DEV"] != "";

export default [
    "sunburst",
].map(name => ({
    input: `assets/${name}.js`,
    output: {
        file: `assets/${name}.rollup.js`,
        format: "iife",
    },
    sourcemap: isDev ? "inline" : false,
    plugins: [
        scss({
            output: `assets/${name}.rollup.css`,
        }),
        eslint(),
        node(),
        commonjs(),
        cleanup({maxEmptyLines: 1}),
    ],
}));

