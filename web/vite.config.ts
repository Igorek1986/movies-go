import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  css: {
    preprocessorOptions: {
      scss: {
        // Переменные нужно явно импортировать в каждом .scss файле:
        // @use "@/styles/variables" as *;
      },
    },
  },
  build: {
    outDir: '../internal/web/dist',  // Go embed подхватит этот каталог
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8888',
      '/timecode': 'http://localhost:8888',
      '/device': 'http://localhost:8888',
      '/login': 'http://localhost:8888',
      '/register': 'http://localhost:8888',
      '/myshows': 'http://localhost:8888',
      '/telegram': 'http://localhost:8888',
      '/sessions': 'http://localhost:8888',
      '/tg-app': 'http://localhost:8888',
      '/static': 'http://localhost:8888',
      '/bot': 'http://localhost:8888',
    },
  },
})
