# ADR 0039: 单文件 Caddyfile 挂载的内联受管块(file 布局)

## 状态

accepted

## 上下文

ADR 0035 的配置投递只认"/etc/caddy 目录持久挂载"(bind 或 named volume),把**单文件挂载**(`-v /path/Caddyfile:/etc/caddy/Caddyfile`)fail closed——但该形态是 Docker 部署 Caddy 最主流的教程写法,实测用户(国内部署真实案例)直接撞上拒绝文案,功能对一大类真实部署形同虚设。

关键权衡:

1. **写进容器层 conf.d**:容器重建即丢,与"受管安装"的持久性承诺冲突,ADR 0035 已否决。
2. **要求用户改挂载**:当前行为,实测证明摩擦不可接受——用户的 Caddyfile 挂载本身已经是持久层,只是形状不同。
3. **内联进 Caddyfile**:用户的 Caddyfile 是持久挂载文件,把站点块以标记段内联进去(`# >>> proxyhub managed` / `# <<< proxyhub managed`),配置随文件持久,不依赖 conf.d。
4. **写入方式**:单文件 bind 挂载 pin 住 inode,atomic-rename(tmp+mv)会让容器继续读旧 inode 的内容——必须原位写入(`cat >`),安全性由"安装前备份 + validate + 失败回滚"链承担而非原子性。

## 决策

1. **配置布局二分**:docker 模式新增配置布局维度——`root`(/etc/caddy 目录/卷挂载,沿用 ADR 0035 的 fragment + import 机制)与 `file`(仅 /etc/caddy/Caddyfile 文件挂载,内联受管块)。`docker_caddy_config_layout` 统一判定;其余形态(无挂载、/etc/caddy 不可用挂载、其他子路径挂载)维持 fail closed。
2. **file 布局的受管块**:站点块由 `_caddy_site_block` 统一生成(与 fragment 同模板),经 `write_caddy_siteblock` 原位拼接进 Caddyfile 标记段;幂等(先摘除旧块再插入),operator 其余内容字节不动。不追加 conf.d import 行、不写 fragment、不做 `caddy fmt --overwrite`(避免重排用户整个文件;validate 已覆盖语法)。
3. **生命周期对齐**:rotate-path 重写受管块(同一函数);uninstall 用 `remove_caddy_siteblock` 摘除块而非删除文件;backup/restore 经 `caddy_managed_config_path` 备份 Caddyfile 本体(staging 按 basename 落名);回滚恢复安装前 Caddyfile 备份。
4. **布局不落安装档案**:与桥接网关同理由——总能从活容器 inspect 重推导,档案只记 CADDY_MODE/CADDY_CONTAINER。

## 理由

- 单文件挂载的 Caddyfile 本身就是用户自认的持久层,内联是把"受管"适配进用户既有拓扑而非要求用户改拓扑——与 ADR 0035 "识别并集成既有部署"的初衷一致,只是补上了当时漏掉的最常见形态。
- 原位写入换来 bind-file 正确性;其原子性损失由既有的备份-校验-回滚链覆盖,语义不降级。
- 布局由活容器重推导,避免档案与容器挂载漂移。

## 后果

- fmt 跳过意味着 operator Caddyfile 的既有格式不被整理(只校验),属刻意保守。
- 内联块与 conf.d fragment 不可同时生效;布局判定在每次 Caddy 触点重跑,容器挂载形状变更(用户把 file 改成 root)会被下次操作自然吸收。
- 术语见 CONTEXT.md「Docker Caddy 模式」的布局段。
