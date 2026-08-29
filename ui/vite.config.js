import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { viteSingleFile } from 'vite-plugin-singlefile'

export default defineConfig({
  plugins: [vue(), tailwindcss(), viteSingleFile()],
  // Builds straight into the Go embed root. emptyOutDir stays false: that
  // directory is embedded by internal/web, so the build must overwrite
  // index.html without wiping the directory it lives in.
  build: { outDir: '../internal/web/static', emptyOutDir: false },
  server: {
    proxy: {
      '/api': process.env.XFA_UI_PROXY || 'http://127.0.0.1:8787',
    },
  },
  test: { environment: 'jsdom' },
})
