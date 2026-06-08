import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      // 18080 to avoid colliding with k3d's host :8080 (mapped to traefik).
      // For local preview: kubectl -n <tenant> port-forward svc/<shop> 18080:8080
      '/api': {
        target: 'http://localhost:18080',
        changeOrigin: true,
      },
    },
  },
});
