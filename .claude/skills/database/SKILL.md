---
name: database
description: ProxyHub 数据库纪律——涉及 SQLite schema 变更、迁移 SQL、store 层查询、数据文件路径、测试 fixture 数据时执行
---

# 数据库纪律(database)

存储层在 `internal/store/`,SQLite 单文件。本 skill 是所有碰数据动作的硬约束集合。

## 1. 路径与文件纪律

- 数据库默认路径必须是 `var/data/data.db`(AGENTS.md §2);**禁止**把默认路径指向仓库根;写入方负责 `MkdirAll`(见 `store.Open`)
- `*.db` 永不签入;集成测试的数据只能落 `.test/`(gitignored),单元测试用 `t.TempDir()`
- 生产环境路径(`/var/lib/proxyhub`)只属于 `install.sh`/`proxyhubctl`,代码默认路径不得引用

## 2. 迁移 SQL

- 迁移文件在 `internal/store/migrations/NNNN_<名称>.sql`,编号递增取现有最大 +1
- 迁移 SQL 是唯一允许签入的 SQL 文件(AGENTS.md §1);落库即不可变——要改已发布行为,写新迁移,不改旧文件
- schema 变更必须同时考虑**旧数据兼容**:已有行在新语义下是什么状态,需不需要 backfill(参考 `019_user_id_backfill.sql`)

## 3. 测试 fixture 脱敏(安全红线)

- 任何 fixture/测试数据只用 `example.com` + 全零 UUID 合成值
- 禁止任何形式的真实节点信息(密码/UUID/token/订阅 URL)进测试与 testdata

## 4. 已知坑

- **时间字段格式**:曾有 `time.Time` 值直接落库、导致 SQL `datetime()` 解析出 NULL 的教训。时间列的写入格式与 SQL 比较函数必须对齐;排查时间相关问题时先用代码路径验证,别拿裸 SQL 的结果当结论。
- 改表结构前,先查这张表的**其他读写方**(dashboard 告警面板读机场测试分、`pull_logs` 喂端点统计、节点可用性被订阅生成与 `internal/nodetag/` 消费)——一个 writer 变更,全部 reader 重新确认。

## 5. 收尾检查

- [ ] 新迁移编号唯一、递增、落库后未再改
- [ ] 旧数据兼容已考虑(backfill 或默认行为)
- [ ] 无 `*.db` / 真实数据进入 git 暂存区
- [ ] fixture 全合成
