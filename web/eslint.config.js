import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import pluginVue from 'eslint-plugin-vue'
import eslintConfigPrettier from 'eslint-config-prettier'

export default tseslint.config(
  {
    ignores: ['dist/**', 'node_modules/**']
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    files: ['**/*.vue'],
    languageOptions: {
      parserOptions: {
        parser: tseslint.parser
      }
    },
    rules: {
      // 设计规范门禁(docs/design-frontend.md):静态内联 style 与超长文件。
      // Phase 3 已收网(2026-07):存量清零,升 error,新增违规硬阻塞 make check
      'vue/no-static-inline-styles': 'error',
      'max-lines': ['error', 400]
    }
  },
  {
    // TS 已做未定义变量检查;模板编译器生成物也会误触 no-undef
    rules: {
      'no-undef': 'off'
    }
  },
  {
    // 存量代码以 warn 起步,不阻塞合入(对齐 design-frontend.md 的阶段策略);Phase 3 收网时升 error
    rules: {
      '@typescript-eslint/no-explicit-any': 'warn',
      '@typescript-eslint/no-unused-vars': 'warn'
    }
  },
  {
    // 路由视图与布局骨架是页面结构不是组件,文件名允许单词
    files: ['src/views/**/*.vue', 'src/layout/**/*.vue'],
    rules: {
      'vue/multi-word-component-names': 'off'
    }
  },
  eslintConfigPrettier
)
