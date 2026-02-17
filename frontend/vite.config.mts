import path from "path";
import { defineConfig } from "vite";

export default defineConfig({
    resolve: {
        alias: {
            "@go": path.resolve(__dirname, "wailsjs/go"),
            "@components": path.resolve(__dirname, "src/components"),
            "@assets": path.resolve(__dirname, "src/assets"),
            "@pages": path.resolve(__dirname, "src/pages"),
            "@runtime": path.resolve(__dirname, "wailsjs/runtime"),
            "@utils": path.resolve(__dirname, "src/utils"),
            "@store": path.resolve(__dirname, "src/store"),
        },
    },
    build: {
        rollupOptions: {
            input: {
                main: "index.html",
                config: "src/pages/config/index.html",
            },
        },
    },
    server: {
        hmr: {
            host: 'localhost',
            protocol: 'ws',
        },
    },
});
