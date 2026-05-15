import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const proxyTarget =
    env.VITE_ARX_API_PROXY_TARGET || 'https://127.0.0.1:8080'

  return {
    plugins: [react(), tailwindcss()],
    server: {
      port: 5173,
      proxy: {
        '/v1': {
          target: proxyTarget,
          changeOrigin: true,
          secure: false,
          ws: true,
        },
      },
    },
  }
})
