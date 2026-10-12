import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueDevTools from 'vite-plugin-vue-devtools'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    vueDevTools(),
    // Element Plus 元件與指令(v-loading)按需載入,元件樣式隨元件引入
    Components({ resolvers: [ElementPlusResolver({ importStyle: 'css' })], dts: false }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    host: true,
    port: 5173,
    proxy: {
      // docker compose 內設為 http://api:8080;本機直接跑則用預設值
      '/api': process.env.API_PROXY_TARGET || 'http://localhost:8080',
    },
  },
})
