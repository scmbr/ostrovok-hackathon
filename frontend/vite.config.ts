import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

// В разработке интерфейс идёт против стенда demo (данные жюри): `docker compose -p demo ... up -d`, порт 8081.
// Другой бэкенд — переменная API_TARGET.
export default defineConfig(({ mode }) => {
  const target = loadEnv(mode, '.', '').API_TARGET || 'http://localhost:8081'
  return {
    plugins: [react()],
    resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
    server: { proxy: { '/api': target, '/health': target } },
  }
})
