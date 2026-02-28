import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:18790',
        changeOrigin: true,
        ws: true,
      },
      '/ws': {
        target: 'ws://localhost:18790',
        ws: true,
      },
    },
  },
})
