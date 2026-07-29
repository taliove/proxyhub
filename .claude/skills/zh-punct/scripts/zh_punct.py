#!/usr/bin/env python3
"""zh_punct.py - 中文文档标点规整:用户向文档的中文行文半角标点转全角。

用法:
    python3 zh_punct.py <文件...>        规整指定文件(就地改写)
    python3 zh_punct.py --user-facing    规整四个用户向文档(README/FAQ/DEPLOY/SECURITY)
    python3 zh_punct.py --check <文件...> 只检查不改写;有需要规整的文件时退出码 1

规则(逐行处理,围栏代码块 ``` 内一律不动):
  1. 遮蔽:行内代码 `x`、Markdown 链接目标 ](url)、裸 URL。
  2. 半角 , ; : ? ! 前为中文字符(可隔 **)时转全角。
  3. 半角圆括号对内含中文且左括号紧随中文/** 时,整对转全角;纯英文/技术内容
     (如 (GHCR)、(--no-caddy))保持半角。
  4. 全角右括号后的 , ; : ? ! 转全角。
  5. 修复历史混杂括号对:右括号宽度跟随其左括号。
脚本幂等:全角字符不会被重复匹配。中文字符判定范围见 CJK(含 「」——…、)。
"""
import re
import sys
from pathlib import Path

CJK = r'[一-鿿「」『』——…、]'
FULL = {',': chr(0xFF0C), ';': chr(0xFF1B), ':': chr(0xFF1A), '?': chr(0xFF1F), '!': chr(0xFF01)}
LP = chr(0xFF08)
RP = chr(0xFF09)

MASK_PATTERNS = [
    re.compile(r'`[^`]+`'),                 # 行内代码
    re.compile(r'\]\([^)\s]*\)'),           # Markdown 链接目标 ](url)
    re.compile(r'https?://[^\s)\]]+'),      # 裸 URL
]
PUNCT_RE = re.compile(rf'({CJK})(\*{{0,2}})([,:;?!])')
PAREN_PAIR_RE = re.compile(rf'(?<={CJK}|\*)\(([^()]*{CJK}[^()]*)\)')
AFTER_RP_RE = re.compile(rf'({RP})([,:;?!])')
LEAD_RE = re.compile(rf'([,;])(?={CJK})')

USER_FACING = ['README.md', 'docs/FAQ.md', 'docs/DEPLOY.md', 'docs/SECURITY.md']


def convert_line(line: str) -> str:
    stash = []

    def mask(m):
        stash.append(m.group(0))
        return '\x00%d\x00' % (len(stash) - 1)

    for pat in MASK_PATTERNS:
        line = pat.sub(mask, line)

    line = PUNCT_RE.sub(lambda m: m.group(1) + m.group(2) + FULL[m.group(3)], line)
    line = LEAD_RE.sub(lambda m: FULL[m.group(1)], line)
    line = PAREN_PAIR_RE.sub(lambda m: LP + m.group(1) + RP, line)

    # 修复历史混杂括号对:右括号宽度跟随其左括号
    out, stack = [], []
    for ch in line:
        if ch == '(':
            stack.append(')')
            out.append(ch)
        elif ch == LP:
            stack.append(RP)
            out.append(ch)
        elif ch in ')' + RP:
            out.append(stack.pop() if stack else ch)
        else:
            out.append(ch)
    line = ''.join(out)

    line = AFTER_RP_RE.sub(lambda m: RP + FULL[m.group(2)], line)

    return re.sub(r'\x00(\d+)\x00', lambda m: stash[int(m.group(1))], line)


def convert_text(text: str) -> str:
    out, in_fence = [], False
    for line in text.split('\n'):
        if line.lstrip().startswith('```'):
            in_fence = not in_fence
            out.append(line)
            continue
        out.append(line if in_fence else convert_line(line))
    return '\n'.join(out)


def main(argv) -> int:
    check = '--check' in argv
    files = [a for a in argv if not a.startswith('--')]
    if '--user-facing' in argv:
        files.extend(USER_FACING)
    if not files:
        print(__doc__)
        return 2
    dirty = []
    for name in files:
        p = Path(name)
        if not p.is_file():
            print(f'skip(not found): {name}', file=sys.stderr)
            continue
        new = convert_text(p.read_text(encoding='utf-8'))
        if new != p.read_text(encoding='utf-8'):
            dirty.append(name)
            if not check:
                p.write_text(new, encoding='utf-8')
    for name in dirty:
        print(('would convert: ' if check else 'converted: ') + name)
    return 1 if check and dirty else 0


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
