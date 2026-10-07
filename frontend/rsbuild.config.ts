import path from 'path';
import { defineConfig } from '@rsbuild/core';
import { pluginReact } from '@rsbuild/plugin-react';

// 后端地址：本地开发把 /api 代理到 Go（端口取自 backend/config/config.example.yaml 的 8080）。
// 生产由 Caddy 同源反代（deploy/，S1-F），前端代码里一律写相对路径 /api/...。
const API_PROXY_TARGET = process.env.API_PROXY_TARGET || 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [pluginReact()],
  source: {
    entry: { index: './src/index.tsx' },
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  html: {
    template: './src/index.html',
    title: '履约中台',
  },
  // 构建产物固定 dist/（S1-F 的 Caddy 指向它，见总纲 §13.1 / 子 spec §6）。
  output: {
    distPath: { root: 'dist' },
  },
  server: {
    port: Number(process.env.PORT) || 3000,
    proxy: {
      '/api': { target: API_PROXY_TARGET, changeOrigin: true },
    },
  },
});
