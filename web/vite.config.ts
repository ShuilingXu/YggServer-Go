import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'YGG_')
  const backend = env.YGG_BACKEND_URL || 'http://127.0.0.1:8080'
  return {
    base: '/web/',
    plugins: [react()],
    server: {
      proxy: {
        '/healthz': backend,
        '/readyz': backend,
        '/authserver': backend,
        '/sessionserver': backend,
        '/api': backend,
        '/textures': backend,
        '/minecraft': backend,
      },
    },
  }
})
