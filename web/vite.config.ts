import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  build: {
    // 直接输出到后端 go:embed 的源目录，构建后即可编译进单二进制
    outDir: '../cmd/server/web',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks: undefined
      }
    }
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true
      },
      // 订阅出口由后端处理,开发模式下同样代理到后端
      '/sub': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true
      }
    }
  }
})
