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
      // В dev режиме проксируем API на Go сервер
      '/api': 'http://localhost:8080',
      '/timecode': 'http://localhost:8080',
      '/device': 'http://localhost:8080',
      '/login': 'http://localhost:8080',
      '/register': 'http://localhost:8080',
      '/myshows': 'http://localhost:8080',
      '/telegram': 'http://localhost:8080',
      '/sessions': 'http://localhost:8080',
      '/tg-app': 'http://localhost:8080',
      '/static': 'http://localhost:8080',
      '/bot': 'http://localhost:8080',
    },
  },
})
