import { resolve } from 'node:path';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist-ops',
    rollupOptions: {
      input: resolve(import.meta.dirname, 'ops.html'),
    },
  },
  server: {
    port: 3011,
    proxy: {
      '/api': {
        target: 'http://localhost:8643',
        changeOrigin: true,
      },
    },
  },
});
