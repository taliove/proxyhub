# ADR 0051: SQLite 启用 busy_timeout 与 WAL 模式

## 状态

accepted

## 日期

2026-09-08

## 上下文

节点池写入是单事务逐行 upsert(整个池),配合 `SetMaxOpenConns(1)` 的单写者模型,任何长时间写事务都会阻塞所有并发 DB 访问——而每个认证请求都要查库(`ipFilterMiddleware` 的 `IsDenied`、`requireAuth` 的 `GetUserByID`)。生产 DSN 此前不带任何 pragma:默认 `busy_timeout=0`(锁竞争立即报错)、`journal_mode=DELETE`(读写互斥)。issue #143 的两个症状(大订阅刷新失败、测速后全站超时)都被这一层放大。

## 决策

生产连接 DSN 增加 `_pragma=busy_timeout(5000)` 与 `_pragma=journal_mode(WAL)`:

1. **busy_timeout(5000)**:瞬时锁竞争等待 5 秒而非立即返回 busy 错误。进程内单连接(`SetMaxOpenConns(1)`)下不存在连接间竞争,此 pragma 的实际受益者是进程外访问者(备份工具、CLI 查看器)与未来可能的只读连接池。
2. **WAL 模式**:读写并发收益同样只对进程外/多连接读者成立;单连接模型下 `database/sql` 严格串行,长写事务期间读 API 排队与 journal mode 无关。WAL 在本决策中的定位是:为外部读者解除读阻塞 + 为将来提升连接数铺路,顺带获得更轻的 fsync 代价。

**机制澄清**:issue #143「测速后全站超时」的真正修复点是全局 `WriteTimeout` 归零与分片局部 upsert 缩短写事务;busy_timeout/WAL 是配套加固,不是该症状的机理修复。若未来要兑现"读不阻塞写"的进程内收益,需引入独立只读连接池,届时重读本 ADR。

## 后果

- 数据目录会多出 `-wal` / `-shm` 两个伴随文件,备份与运维脚本需一并纳入(单机部署形态无其他影响)。
- WAL 的 checkpoint 由 SQLite 自动管理;长事务会推迟 checkpoint 使 `-wal` 文件增长,配合分片局部 upsert(issue #143 修复的配套改动)后事务时长已收敛,可接受。
- `synchronous` 保持默认 `FULL`(WAL 下的 FULL 仍保证崩溃安全,不降为 `NORMAL` 换性能)。

## 已否决的替代方案

- **只加 busy_timeout 不开 WAL**:对外部读者的读阻塞依旧,且失去将来多连接的演进空间。
- **本次直接提升连接数(独立只读连接池)**:能真正兑现进程内读写并发,但改连接模型超出本次 bug 修复范围,留作后续决策。
- **换 embedded 数据库(如 Pebble/BoltDB)**:迁移成本与风险远超收益,单写者模型已够用。
