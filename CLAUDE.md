# ProxyHub 工程原则

本文件是本仓库的工程宪法。任何会话、任何提交都必须遵守。
术语以 [CONTEXT.md](CONTEXT.md) 为准(机场/订阅地址/节点/聚合/刷新有严格定义)。

## 1. 仓库内容边界

**只签入**:源代码、测试、构建脚本、迁移 SQL、长期文档(见 §2)、CI 配置。

**永不签入**:
- 运行时产物:`xray_config.json`、`*.db`、`*.log`、`config.yaml`、`dist/`、二进制
- 凭证:任何真实节点密码/UUID/token/订阅 URL —— 测试 fixture 一律用 `example.com` + 全零 UUID
- 过程产物:合并分析、实施计划/总结/验证报告、AI 会话工作笔记、`.scratch/`
- 死备份:`*_old.*`、`*_backup.*`、注释掉的死代码文件
- 依赖目录:`node_modules/`、`vendor/`

判断标准:**这个文件对"下一个人理解系统"有帮助吗?** 没有就不进仓库。

## 2. 文档政策

| 留 | 删 |
|---|---|
| 术语表(CONTEXT.md) | 过程记录(计划/总结/验证/合并分析) |
| ADR(docs/adr/,为什么这么做) | AI 工作流产物(plans/superpowers) |
| 设计文档(架构/模型/规格) | 带日期的里程碑文档 |
| 运维文档(DEPLOY/SECURITY/ACCEPTANCE) | 旧版本死备份 |

文档过期即删,不留"考古层"。

## 3. 提交规范

- 消息:**纯英文、纯 ASCII**(不用 `—`/`→`,用 `-`/`->`),conventional commits 格式:`<type>: <description>`,type ∈ feat/fix/refactor/docs/test/chore/perf/ci
- 作者:`taliove2009 <taliove2009@gmail.com>`(repo 级 config)
- 一个提交一个语义;修复不混进功能提交

## 4. 构建与运行

前端嵌入二进制(go:embed),顺序不可逆:
`make build`(= web npm build → go build)→ `./start.sh` 重启。
改前端不重启 = 没生效。

## 5. 测试门槛

- 每次签入前:`go build ./...` + `go vet ./...` + 受影响包的测试
- 推送前:`go test ./...` 全量 + `scripts/install/test_*.sh` 六个套件
- 已知既有失败(3 处,勿"修"):2 个默认模板测试 + `TestHandleTestNode_MissingTarget`

## 6. 安全红线

- Xray 任何入站必须 `listen: 127.0.0.1`(`internal/xray/config.go` 与 `internal/distribution/routing.go` 两处生成器,改一必查二,有测试断言)
- 不禁用 TLS 验证(无 `InsecureSkipVerify`、无 `--insecure`)
- 管理面只走 Site Path + loopback;订阅地址 = 随机 path + token,公开但不可枚举
- 签入前必过 `gitleaks`(配置见 `.gitleaks.toml`)

## 7. 工作流 Skills

签入前走 `.claude/skills/pre-commit`,推送前走 `.claude/skills/pre-push`。
Skills 是流程,不是建议——逐条执行,不允许跳项。
