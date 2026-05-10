import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  build: {
    // Go embeds the `web/` directory; output there directly
    outDir: resolve(__dirname, '../web'),
    emptyOutDir: true,
  },
  server: {
    // Dev: proxy API calls to the Go backend
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
