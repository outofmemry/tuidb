import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// The Go binary embeds the build output from internal/web/dist (see `make web`).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { outDir: '../internal/web/dist', emptyOutDir: true },
  // changeOrigin must stay off: the API checks Origin against Host.
  server: { proxy: { '/api': { target: process.env.WEBTUIDB_API ?? 'http://127.0.0.1:7788', changeOrigin: false } } },
})
