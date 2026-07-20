---
name: release
description: ProxyHub 发布管理——版本纪律、发布前演练、打 tag、GitHub Actions 发布、发布后验证与回滚
---

# 发布管理(release)

发布由 GitHub Actions 自动完成(`.github/workflows/release.yml`,tag `v*` 触发),本 skill 管理"人"的这一侧:版本、演练、验证。pipeline 内部的事(构建矩阵、打包、attest、GHCR)不要手工干预。

## 0. 版本纪律(不可违反)

- `VERSION` 文件是版本的**唯一事实源**;release.yml 会拒绝与 `v$(cat VERSION)` 不一致的 tag
- 严格 SemVer:`MAJOR.MINOR.PATCH`,预发布用 `-rc.N` 后缀;**禁止 `+` 构建元数据**(GHCR tag 不支持)
- 预发布版本(含 `-`)会自动标记为 GitHub prerelease;`proxyhubctl update --stable-only`(自动更新 timer)会跳过它
- 制品命名契约:`proxyhub_<version>_<os>_<arch>.tar.gz` + `SHA256SUMS`,下划线分隔——`install.sh` 和 `proxyhubctl update` 都按此消费,改命名必须三处同步(release/package.sh、install.sh、proxyhubctl)

## 1. 发布前演练(全本地,必做)

```bash
make check        # vet + Go 测试 + shell 套件,全绿
make build        # 完整构建可用

# 打包演练(单目标提速;完整矩阵由 CI 跑)
TARGETS="linux/amd64" bash scripts/release/package.sh
bash scripts/release/verify.sh dist/release
```

- 检查 `dist/release/` 制品名是否符合命名契约(下划线!)
- 确认 `VERSION` 已更新,且与想打的 tag 一致
- 过一遍自上次发布以来的 `git log --oneline`,确认没有半成品提交混进 main

## 2. 发布(remote 建好之后)

```bash
git tag -a v$(cat VERSION) -m "release v$(cat VERSION)"
git push origin main --tags
```

之后只做观察,不做补救式手改:

```bash
gh run watch                         # 盯 validate -> test -> package -> docker
gh release view v$(cat VERSION)      # 确认制品、SHA256SUMS、prerelease 标记
```

pipeline 阶段:validate(tag==VERSION、SemVer)→ test(make vet/test)→ package(矩阵打包 + checksum 验证 + attest + 上传)→ docker(GHCR 多架构镜像 + attest)。任一阶段失败,看日志修代码,**改完发新 tag,不删已推的 tag**。

## 3. 发布后验证(必做)

```bash
# 下载一个制品 + SHA256SUMS,用仓库自带工具校验
gh release download v$(cat VERSION) -p "proxyhub_$(cat VERSION)_linux_amd64.tar.gz" -p SHA256SUMS -D /tmp/ph-verify
bash scripts/release/verify.sh /tmp/ph-verify
```

- [ ] 制品数量与矩阵一致(linux amd64/arm64 + darwin + windows)
- [ ] attest provenance 存在(release 页面 / `gh attestation verify`)
- [ ] GHCR 镜像 `ghcr.io/<owner>/proxyhub:<tag>` 可拉取;稳定版还有 `:latest`
- [ ] 干净环境冒烟(可选但推荐):Linux 容器/VM 里跑 `install.sh --non-interactive`,或至少 `bash scripts/install/test_install.sh`

## 4. 回滚原则

- **已发布的 tag 和制品不可变**:发现缺陷,修复后发 `PATCH`/`rc.N+1` 新版,不删旧 tag(下游可能已 pin checksum)
- 极端情况(制品含秘密):`gh release delete`,但视为已泄露走轮换流程,而不是假装没发生

## 5. 首次推送前(一次性)

走 `pre-push` skill 全项,额外确认:仓库可见性(private/public)、LICENSE 文件、`secrets.GITHUB_TOKEN` 权限(contents: write / packages: write / attestations 由 workflow 声明,无需手动配)。

## 完成标准

VERSION/tag 一致 + 演练绿 + pipeline 绿 + 制品校验通过。任何一步存疑,停下来,不发。
