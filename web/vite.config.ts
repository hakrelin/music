import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import { fileURLToPath, URL } from 'node:url';

// 开发时 /api 与 /api/ws 都反代到本地 Go 服务（默认 :8080）。
const target = process.env.MUSIC_DEV_TARGET || 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) }
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': {
        target,
        changeOrigin: true,
        ws: true
      }
    }
  },
  build: {
    outDir: 'dist',
    assetsInlineLimit: 2048,
    chunkSizeWarningLimit: 900
  }
});
