import { defineConfig } from "vite";
import tsConfigPaths from "vite-tsconfig-paths";

export default defineConfig({
    build: {
        rollupOptions: {
            input: {
                main: "index.html",
                config: "src/pages/config/config.html",
            },
        },
    },
    plugins: [
        tsConfigPaths(),
    ],
    server: {
        hmr: {
            host: 'localhost',
            protocol: 'ws',
        },
    },
});
