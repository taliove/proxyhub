// Monaco Editor 的 Web Worker 配置。
// Vite 用 ?worker 后缀把 worker 打包成独立 chunk；这里只加载编辑器核心 worker
// （不引 language-specific worker，因为 YAML 高亮由 Monaco 内置 tokenizer 处理，
// 无需语言服务 worker）。
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'

;(self as unknown as { MonacoEnvironment: unknown }).MonacoEnvironment = {
  getWorker() {
    return new EditorWorker()
  }
}
