import path from "path";
import { defineConfig } from "vite";

export default defineConfig({
    resolve: {
        alias: {
            // v3 generates bindings nested by Go import path.  The
            // alias absorbs the constant prefix, so a site imports
            // "@go/library/library" rather than the full
            // "bindings/yellowjacket/backend/library/library".
            "@go": path.resolve(__dirname, "bindings/yellowjacket/backend"),
            "@components": path.resolve(__dirname, "src/components"),
            "@assets": path.resolve(__dirname, "src/assets"),
            "@pages": path.resolve(__dirname, "src/pages"),
            "@runtime": path.resolve(__dirname, "src/wails"),
            "@utils": path.resolve(__dirname, "src/utils"),
            "@store": path.resolve(__dirname, "src/store"),
        },
    },
    build: {
        // The bundled icons must be emitted as files, not inlined.
        // Vite inlines any asset under 4 kB, and 63 of the 64 icons are
        // under 4 kB, so the default put ~96 kB of base64 into the main
        // chunk — parsed at startup, for icons most views never render.
        // As files they are served same-origin (so still offline) and
        // fetched on first use.
        assetsInlineLimit: (filePath: string) =>
            filePath.includes('/assets/icons/fa/') ? false : undefined,
        rollupOptions: {
            input: {
                main: "index.html",
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
