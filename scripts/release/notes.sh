#!/usr/bin/env bash
# notes.sh - 生成 GitHub Release 发布说明(markdown,输出到 stdout)。
#
# 用法: bash scripts/release/notes.sh <tag>   # 如 v0.1.0(须在 git checkout 内)
#
# 结构:定位语 -> 安装(一键/tarball/Docker)-> 校验 -> 分组变更记录
# (conventional commits 按 type 分组)-> Full Changelog 链接。
# 发布后仍可在 Release 页手工精修"亮点"段落。
set -Eeuo pipefail

tag="${1:?usage: notes.sh <tag>}"
repo="${GITHUB_REPOSITORY:-taliove/proxyhub}"
version="${tag#v}"

prev_tag=$(git tag --sort=-creatordate | grep -v "^$tag\$" | head -1 || true)
if [[ -n $prev_tag ]]; then
    range="${prev_tag}..${tag}"
    compare="https://github.com/${repo}/compare/${prev_tag}...${tag}"
else
    range="$tag"
    compare="https://github.com/${repo}/releases/tag/${tag}"
fi

# 按 conventional type 分组收集 subject 行。
declare -a feats fixes perfs refactors docs tests chores
while IFS= read -r subject; do
    case "$subject" in
        feat:*|feat\(*\):*) feats+=("${subject}") ;;
        fix:*|fix\(*\):*) fixes+=("${subject}") ;;
        perf:*|perf\(*\):*) perfs+=("${subject}") ;;
        refactor:*|refactor\(*\):*) refactors+=("${subject}") ;;
        docs:*|docs\(*\):*) docs+=("${subject}") ;;
        test:*|test\(*\):*) tests+=("${subject}") ;;
        chore:*|chore\(*\):*|ci:*|ci\(*\):*) chores+=("${subject}") ;;
    esac
done < <(git log --pretty=%s "$range" | head -100)

emit_group() { # TITLE ITEMS...
    local title=$1; shift
    (($# == 0)) && return 0
    printf '### %s\n\n' "$title"
    printf -- '- %s\n' "$@"
    printf '\n'
}

cat <<EOF
把多个机场订阅聚合成一个统一订阅地址 —— 自动筛选最优节点,一个链接喂饱所有设备。

## 安装

### 一键安装(生产环境,Ubuntu/Debian)

\`\`\`bash
bash <(curl -fsSL https://raw.githubusercontent.com/${repo}/main/install.sh)
\`\`\`

自动配置 systemd 服务、Caddy HTTPS 反代与 \`proxyhubctl\` 运维工具。详见 [生产部署指南](https://github.com/${repo}/blob/main/docs/DEPLOY.md)。

### 直接下载

发布包命名 \`proxyhub_${version}_<os>_<arch>.tar.gz\`,含可执行文件与示例配置。解包后 \`./proxyhub\` 启动,访问 \`http://localhost:8080\` 完成初始化向导。制品版本可用 \`./proxyhub version\` 核对。

### Docker(开发/测试)

\`\`\`bash
docker run -d -p 127.0.0.1:8080:8080 -v ./data:/data --name proxyhub ghcr.io/${repo}:${tag}
\`\`\`

## 校验

\`\`\`bash
# 校验制品完整性
bash scripts/release/verify.sh <下载目录>

# 校验构建溯源(SLSA provenance)
gh attestation verify proxyhub_${version}_linux_amd64.tar.gz --repo ${repo}
\`\`\`

## 变更记录

EOF

# ${arr[@]+...} 写法:空数组在 bash 3.2 下直接 "${arr[@]}" 会触发 unbound。
emit_group "✨ 新功能" ${feats[@]+"${feats[@]}"}
emit_group "🐛 修复" ${fixes[@]+"${fixes[@]}"}
emit_group "⚡ 性能" ${perfs[@]+"${perfs[@]}"}
emit_group "🔧 重构" ${refactors[@]+"${refactors[@]}"}
emit_group "📝 文档" ${docs[@]+"${docs[@]}"}
emit_group "🧪 测试" ${tests[@]+"${tests[@]}"}
emit_group "🧹 杂项与 CI" ${chores[@]+"${chores[@]}"}

printf '**Full Changelog**: %s\n' "$compare"
