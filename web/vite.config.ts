import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/v1': 'http://localhost:8080',
      '/metrics': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
      '/openapi.json': 'http://localhost:8080',
    },
  },
  preview: {
    port: 4173,
    strictPort: true,
  },
})
