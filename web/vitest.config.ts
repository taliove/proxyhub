import { defineConfig } from 'vitest/config'
import { resolve } from 'path'

// 前端单测配置:纯逻辑单测(node 环境),路径别名与应用一致。
export default defineConfig({
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts']
  }
})
