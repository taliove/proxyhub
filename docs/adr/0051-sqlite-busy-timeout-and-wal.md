# ADR 0051: SQLite 启用 busy_timeout 与 WAL 模式

## 状态

accepted

## 日期

2026-09-08

## 上下文

节点池写入是单事务逐行 upsert(整个池),配合 `SetMaxOpenConns(1)` 的单写者模型,任何长时间写事务都会阻塞所有并发 DB 访问——而每个认证请求都要查库(`ipFilterMiddleware` 的 `IsDenied`、`requireAuth` 的 `GetUserByID`)。生产 DSN 此前不带任何 pragma:默认 `busy_timeout=0`(锁竞争立即报错)、`journal_mode=DELETE`(读写互斥)。issue #143 的两个症状(大订阅刷新失败、测速后全站超时)都被这一层放大。

## 决策

生产连接 DSN 增加 `_pragma=busy_timeout(5000)` 与 `_pragma=journal_mode(WAL)`:

1. **busy_timeout(5000)**:瞬时锁竞争等待 5 秒而非立即返回 busy 错误。
2. **WAL 模式**:读不阻塞写、写不阻塞读,缓解单连接模型下读 API 排队。

## 后果

- 数据目录会多出 `-wal` / `-shm` 两个伴随文件,备份与运维脚本需一并纳入(单机部署形态无其他影响)。
- WAL 的 checkpoint 由 SQLite 自动管理;长事务会推迟 checkpoint 使 `-wal` 文件增长,配合分片局部 upsert(issue #143 修复的配套改动)后事务时长已收敛,可接受。
- `synchronous` 保持默认 `FULL`(WAL 下的 FULL 仍保证崩溃安全,不降为 `NORMAL` 换性能)。

## 已否决的替代方案

- **只加 busy_timeout 不开 WAL**:读 API 在长写事务期间仍然排队,issue #143 的"测速后全站超时"只能缓解不能消除。
- **换 embedded 数据库(如 Pebble/BoltDB)**:迁移成本与风险远超收益,单写者模型在 WAL 下已够用。
