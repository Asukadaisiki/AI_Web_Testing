import react from '@vitejs/plugin-react';
import { configDefaults, defineConfig } from 'vitest/config';

/** Go 控制面地址；前端不含任何执行逻辑，只通过代理调用它。 */
const GO_API = process.env['AGENTTRACE_API_URL'] ?? 'http://api:8080';

/**
 * 代理必须同时挂在 dev 和 preview 上：`server.proxy` 只对 `vite dev` 生效，
 * `vite preview`（跑构建产物）用的是 `preview.proxy`。少了这一份，
 * 预览出来的页面每个 `/api` 请求都会 404。
 */
const proxy = {
  '/api': { target: GO_API, changeOrigin: true },
  '/artifacts': { target: GO_API, changeOrigin: true },
};

export default defineConfig({
  plugins: [react()],
  test: {
    exclude: [...configDefaults.exclude, 'tests/**'],
  },
  server: {
    // 显式绑定 127.0.0.1：默认的 "localhost" 在 Windows 上可能只解析到 ::1，
    // 于是 http://127.0.0.1:5174 连不上，代理与文档里的地址也就对不上了。
    host: '127.0.0.1',
    port: 5174,
    strictPort: true,
    proxy,
  },
  preview: {
    host: '127.0.0.1',
    port: 5174,
    strictPort: true,
    proxy,
  },
});
