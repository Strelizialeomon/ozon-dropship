// 千行表格性能实测专用构建（S1-E 开工实测，不进生产 dist/）。
// 单独入口、单独产物目录 dist-perf/，由 perf/run.mjs 驱动跑一遍。
import path from 'path';
import { defineConfig } from '@rsbuild/core';
import { pluginReact } from '@rsbuild/plugin-react';

export default defineConfig({
  plugins: [pluginReact()],
  source: {
    entry: { index: './perf/main.tsx' },
    alias: { '@': path.resolve(__dirname, './src') },
  },
  html: { template: './perf/index.html' },
  output: { distPath: { root: 'dist-perf' } },
});
