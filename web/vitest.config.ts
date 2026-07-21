import { defineConfig } from 'vitest/config'
import { resolve } from 'path'
import vue from '@vitejs/plugin-vue'

// 前端单测配置:纯逻辑单测(node 环境),路径别名与应用一致。
// 组件测试需要 jsdom 环境和 vue 插件。
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts']
  }
})
