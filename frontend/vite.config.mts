import { defineConfig } from "vite";
import tsConfigPaths from "vite-tsconfig-paths";
import { viteStaticCopy } from 'vite-plugin-static-copy';

const shoelaceThemePath = 'node_modules/@shoelace-style/shoelace/dist/themes';
const shoelaceIconAssetPath = 'node_modules/@shoelace-style/shoelace/dist/assets';
const shoelaceRangePath = 'node_modules/@shoelace-style/shoelace/dist/components/range';

export default defineConfig({
  plugins: [
    tsConfigPaths(),
    viteStaticCopy({
      targets: [
        {
          src: shoelaceThemePath,
          dest: 'shoelace',
        },
        {
          src: shoelaceIconAssetPath,
          dest: 'shoelace',
        },
        {
          src: shoelaceRangePath,
          dest: 'shoelace/components',
        },
      ],
    }),
  ],
    server: {
    hmr: {
      host: 'localhost',
      protocol: 'ws',
    },
  },
});
