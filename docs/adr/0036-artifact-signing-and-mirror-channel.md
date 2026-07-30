# ADR 0036: 制品签名信任锚与国内镜像下载通道

## 状态

accepted

## 上下文

一键安装器的下载链全部锚在 GitHub:入口脚本与伴侣库(raw.githubusercontent.com)、连通性自检(github.com,硬编码)、latest 解析与制品下载(github.com/releases)、`proxyhubctl update`(api.github.com)。国内用户在这些点上大面积不可达,功能等同不可用。

简单答案是"加个镜像参数",但它动摇安全模型的根基:现有模型的信任锚是 **GitHub HTTPS 本身**——SHA256SUMS 与 tarball 同源,校验和只防传输损坏,防不了源被替换。第三方镜像(ghproxy 类)可同时替换 tarball 与校验和,且这类公益镜像生命周期短、有劫持前科。支持镜像的前提是把信任锚从传输通道挪到密码学签名上。

关键权衡:

1. **签名方案**:minisign(单私钥,签名格式简单)vs cosign keyless(验证依赖 rekor 等在线服务,国内同样不可达)vs GPG(密钥分发与验证都重)。minisign 胜出:私钥一把存 GitHub Secrets,签名在 release.yml 内一步完成。
2. **验签依赖**:目标机是全新 Ubuntu/Debian,minisign 二进制不在默认源,让安装器先装验签工具等于再造一条供应链。minisign 签名本质是 Ed25519——openssl(既有基础依赖)可从 `.minisig` 拆出 64 字节签名、用内嵌公钥拼 PEM 完成验签,零新增依赖。
3. **镜像默认值的信任**:任何具体第三方镜像都不该成为默认值(churn、劫持前科),默认值永远 GitHub 官方;镜像只做显式参数,信任决策留给运维——与安装器 fail-closed 性格一致。有了签名,镜像选择不再影响制品真实性。
4. **latest 解析的通道依赖**:latest tag 解析走 GitHub 重定向/api,镜像无法可靠代理;镜像模式要求显式 `--version`,fail-closed 胜过发明不可靠的解析链。
5. **静默回退**:GitHub 不通自动回退镜像列表,违背 fail-closed 性格且回退顺序可被攻击者诱导,否决。

## 决策

1. **签名信任锚**:release.yml 用 minisign 私钥(GitHub Secrets)给 `SHA256SUMS` 签名,产物新增 `SHA256SUMS.minisig` 资产。install.sh 与 proxyhubctl 内嵌 minisign 公钥,下载后用 openssl 验签(从 `.minisig` 拆签名、公钥拼 PEM,不引入 minisign 依赖);缺签名文件或验签失败 fail closed。信任链:内嵌公钥 → `SHA256SUMS.minisig` → `SHA256SUMS` → tarball;传输通道不再承担信任。
2. **下载基参数**:`--download-base URL`(及 `PROXYHUB_DOWNLOAD_BASE` 环境变量)覆盖连通性自检、制品与校验和/签名下载、伴侣库拉取;默认值永远 GitHub 官方,不内置任何第三方镜像为默认。镜像模式必须显式 `--version`,缺省 fail closed 并指引。无静默回退:GitHub 不通时报错并给出 `--download-base` 指引。
3. **update 同源**:`proxyhubctl update` 走同一下载基与验签;下载基写入安装档案 `DOWNLOAD_BASE=` 字段,更新自动沿用(显式参数优先于档案值)。
4. **国内入口**:jsDelivr 文档化入口(`cdn.jsdelivr.net/gh/<owner>/<repo>@main/install.sh`);脚本拉取伴侣库(install.sh 的 lib.sh 与 proxyhubctl)优先同 ref jsDelivr、GitHub 后备——这两个文件不携带信任,真正的信任锚是验签。
5. **文档**:DEPLOY.md 增「国内部署」节(jsDelivr 入口、`--download-base` 用法与镜像配方、caddy 镜像的 Docker 加速器、GeoIP 无碍说明),FAQ 增问答。

## 理由

- 签名把"敢不敢用镜像"从信任判断变成密码学验证,这是镜像通道唯一负责任的开法;minisign 是该约束下最轻的签名体系(单 key、无在线验证依赖)。
- openssl 验签免去目标机装验签工具的供应链套娃;openssl 是 TLS 栈既有依赖,全新 Ubuntu/Debian 必有。
- 参数覆盖而非内置镜像,把 churn 与信任决策移出代码;安装器的默认值永远指向它能担保的官方源。
- 显式 `--version` 要求与无静默回退,延续安装器"永不猜测"的既定性格(域名、凭证、容器选择的同款处理)。

## 后果

- 发布流程新增一次性运维:生成 minisign 密钥对,私钥入 GitHub Secrets,公钥嵌进 install.sh/proxyhubctl(公钥轮换即发新版,旧公钥验不了新制品属预期)。
- release 资产新增 `SHA256SUMS.minisig`;verify.sh 与打包演练同步覆盖签名产出与验签(本地临时密钥)。
- 镜像模式下 latest 自动解析不可用,`--version` 成为必选项;`proxyhubctl update`(无显式版本时)在镜像模式下同样要求显式版本或回退 GitHub。
- 术语见 CONTEXT.md「下载基」「签名信任锚」。
