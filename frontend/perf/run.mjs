// 千行表格性能实测执行器（S1-E 开工实测）：
//   1) 用 rsbuild.perf.config.ts 生产构建（真浏览器要跑的就是压缩后的产物）
//   2) 起一个静态服务器发 dist-perf/
//   3) playwright-core 开无头 chromium（缓存里的 chromium-1243），等页面把 window.__perf 跑完
//   4) 打印 JSON 结果 + 按验收线判红绿（1000 行首渲 < 1s；排序/筛选中位 < 300ms）
//
// 用法：bun perf/run.mjs

import { spawnSync } from 'node:child_process';
import { readFileSync, existsSync } from 'node:fs';
import path from 'node:path';
import { chromium } from 'playwright-core';

const ROOT = path.resolve(import.meta.dirname, '..');
const DIST = path.join(ROOT, 'dist-perf');
const PORT = 4273;

const FIRST_RENDER_LIMIT_MS = 1000;
const INTERACT_LIMIT_MS = 300;

function build() {
  const r = spawnSync('bunx', ['rsbuild', 'build', '-c', 'rsbuild.perf.config.ts'], {
    cwd: ROOT,
    stdio: 'inherit',
  });
  if (r.status !== 0) {
    console.error('构建失败，先把构建修绿再压测。');
    process.exit(1);
  }
}

function serve() {
  const mime = {
    '.html': 'text/html; charset=utf-8',
    '.js': 'text/javascript; charset=utf-8',
    '.css': 'text/css; charset=utf-8',
    '.map': 'application/json',
  };
  return Bun.serve({
    port: PORT,
    fetch(req) {
      const url = new URL(req.url);
      let file = url.pathname === '/' ? '/index.html' : url.pathname;
      const full = path.join(DIST, file);
      if (!full.startsWith(DIST) || !existsSync(full)) return new Response('not found', { status: 404 });
      return new Response(readFileSync(full), {
        headers: { 'content-type': mime[path.extname(full)] ?? 'application/octet-stream' },
      });
    },
  });
}

const median = (xs) => [...xs].sort((a, b) => a - b)[Math.floor(xs.length / 2)];

async function main() {
  build();
  const server = serve();

  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  await page.goto(`http://127.0.0.1:${PORT}/`);
  await page.waitForFunction(() => window.__perf?.done === true, null, { timeout: 60_000 });
  const perf = await page.evaluate(() => window.__perf);
  await browser.close();
  server.stop(true);

  const sortMed = median(perf.sortMs);
  const filterMed = median(perf.filterMs);
  const resetMed = median(perf.resetMs);

  console.log('\n=== 千行表格性能实测结果（headless chromium，生产构建）===');
  console.log(JSON.stringify({ ...perf, sortMedianMs: sortMed, filterMedianMs: filterMed, resetMedianMs: resetMed }, null, 2));

  const checks = [
    [`1000 行首次渲染 < ${FIRST_RENDER_LIMIT_MS}ms`, perf.firstRenderMs < FIRST_RENDER_LIMIT_MS, `${perf.firstRenderMs}ms`],
    [`排序（中位）< ${INTERACT_LIMIT_MS}ms`, sortMed < INTERACT_LIMIT_MS, `${sortMed}ms`],
    [`筛选（中位）< ${INTERACT_LIMIT_MS}ms`, filterMed < INTERACT_LIMIT_MS, `${filterMed}ms`],
    [`筛选复位（中位）< ${INTERACT_LIMIT_MS}ms`, resetMed < INTERACT_LIMIT_MS, `${resetMed}ms`],
  ];
  let pass = true;
  console.log('\n--- 验收线 ---');
  for (const [name, ok, val] of checks) {
    console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}  →  ${val}`);
    if (!ok) pass = false;
  }
  process.exit(pass ? 0 : 2);
}

main();
