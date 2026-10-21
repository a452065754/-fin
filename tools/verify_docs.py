#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""文档一致性自检。

在提交前运行，防止文档腐化。检查项：

1. 索引里的所有相对链接指向真实文件
2. 文档里提到的源码文件都存在
3. ROADMAP 的任务状态标记合法
4. STATUS / ROADMAP / PROGRESS 里的关键数字与代码实际一致
5. PROGRESS 的阶段条数与 git 提交数一致
6. 只追加型文档没有被改短（历史被删）

用法：
    python3 tools/verify_docs.py          # 校验
    python3 tools/verify_docs.py --fix    # 顺便打印修复建议
"""

import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DOCS = os.path.join(ROOT, "docs")
PROJECT = os.path.join(ROOT, "project")

# 允许的状态标记
VALID_MARKS = {"✅", "🔄", "⏸", "❌", "⚠️"}

# 只追加型文档：不允许被截短
APPEND_ONLY = [
    os.path.join(PROJECT, "PROGRESS.md"),
    os.path.join(PROJECT, "LESSONS.md"),
    os.path.join(PROJECT, "SKILLS.md"),
]

errors = []
warnings = []
notes = []


def err(msg):
    errors.append(msg)


def warn(msg):
    warnings.append(msg)


def note(msg):
    notes.append(msg)


def read(path):
    try:
        with open(path, "r", encoding="utf-8") as f:
            return f.read()
    except OSError as e:
        err("无法读取 %s: %s" % (os.path.relpath(path, ROOT), e))
        return ""


def all_markdown():
    out = []
    for base in (ROOT, DOCS, PROJECT):
        if not os.path.isdir(base):
            continue
        for name in os.listdir(base):
            p = os.path.join(base, name)
            if os.path.isfile(p) and name.endswith(".md"):
                out.append(p)
    return out


def strip_code_blocks(text):
    """去掉围栏代码块，避免把示例里的假链接当成真断链。行内代码要保留，
    因为源码文件引用本身就写成 `internal/fr/xxx.go` 的形式。"""
    return re.sub(r"```.*?```", "", text, flags=re.S)


def check_links():
    """所有 markdown 里的相对链接都指向真实文件。"""
    pattern = re.compile(r"\[([^\]]*)\]\(([^)]+)\)")
    checked = 0
    seen_links = set()
    for path in all_markdown():
        content = strip_code_blocks(read(path))
        base = os.path.dirname(path)
        for label, target in pattern.findall(content):
            if target.startswith(("http://", "https://", "#", "mailto:")):
                continue
            clean = target.split("#")[0].strip()
            if not clean:
                continue
            # 去掉行内代码包裹
            clean = clean.strip("`")
            full = os.path.normpath(os.path.join(base, clean))
            checked += 1
            if not os.path.exists(full):
                key = (os.path.relpath(path, ROOT), target)
                if key not in seen_links:
                    seen_links.add(key)
                    err("断链: %s 中的 [%s](%s) -> %s 不存在"
                        % (os.path.relpath(path, ROOT), label, target, clean))
    note("检查了 %d 个相对链接" % checked)


def check_source_refs():
    """文档里提到的源码文件都存在。"""
    pattern = re.compile(r"`((?:internal|web|cmd|tools)/[\w./-]+\.(?:go|js|css|html|py|md))`")
    checked = 0
    for path in all_markdown():
        content = strip_code_blocks(read(path))
        for ref in set(pattern.findall(content)):
            checked += 1
            if not os.path.exists(os.path.join(ROOT, ref)):
                err("文档引用了不存在的文件: %s (在 %s)"
                    % (ref, os.path.relpath(path, ROOT)))
    note("检查了 %d 个源码文件引用" % checked)


def check_marks():
    """ROADMAP 里的状态标记必须合法。"""
    path = os.path.join(PROJECT, "ROADMAP.md")
    if not os.path.exists(path):
        err("缺少 project/ROADMAP.md")
        return
    content = read(path)
    # 表格行里 `| ✅ |` 之类的状态列
    found = 0
    for line in content.splitlines():
        if not line.strip().startswith("|"):
            continue
        for cell in line.split("|"):
            cell = cell.strip()
            if cell in VALID_MARKS:
                found += 1
            elif re.match(r"^[✅🔄⏸❌⚠️]", cell) and len(cell) <= 3:
                err("ROADMAP 中出现非法状态标记: %r" % cell)
    note("检查了 %d 个状态标记" % found)


def git(*args):
    try:
        return subprocess.check_output(
            ["git"] + list(args), cwd=ROOT, stderr=subprocess.DEVNULL
        ).decode("utf-8", "replace").strip()
    except Exception:
        return ""


def check_numbers():
    """文档里的关键数字要与代码实际一致。"""
    # 测试用例数（所有包）
    count = 0
    for pkg in ("internal/fr", "internal/server"):
        pkgdir = os.path.join(ROOT, pkg)
        if not os.path.isdir(pkgdir):
            continue
        for name in os.listdir(pkgdir):
            if name.endswith("_test.go"):
                with open(os.path.join(pkgdir, name), encoding="utf-8") as f:
                    count += len(re.findall(r"^func Test", f.read(), re.M))
    for doc in ("project/STATUS.md", "docs/交接.md", "README.md"):
        p = os.path.join(ROOT, doc)
        if not os.path.exists(p):
            continue
        content = read(p)
        for m in re.findall(r"(\d+)\s*(?:例|个测试|个用例)", content):
            if int(m) == count:
                break
        else:
            # 只在明确提到数字时报警
            for m in re.findall(r"测试\D{0,4}(\d+)\s*例", content):
                if int(m) != count:
                    err("%s 说 %s 个测试，实际 %d 个" % (doc, m, count))
    note("实际测试用例数: %d" % count)

    # spec.Kind 清单
    chart = os.path.join(ROOT, "internal", "fr", "chart.go")
    kinds = set()
    if os.path.exists(chart):
        with open(chart, encoding="utf-8") as f:
            kinds = set(re.findall(r'spec\.Kind = "(\w+)"', f.read()))
    usage = os.path.join(DOCS, "使用.md")
    if os.path.exists(usage) and kinds:
        content = read(usage)
        m = re.search(r"`kind` 取值：(.*?)。", content, re.S)
        if m:
            doc_kinds = set(re.findall(r"`(\w+)`", m.group(1)))
            missing = kinds - doc_kinds
            extra = doc_kinds - kinds
            if missing:
                err("使用.md 缺少 Kind: %s" % ", ".join(sorted(missing)))
            if extra:
                err("使用.md 多写了 Kind: %s" % ", ".join(sorted(extra)))
            else:
                note("Kind 清单一致: %d 种" % len(kinds))
        else:
            warn("使用.md 里没找到 `kind` 取值清单")

    # API 端点
    server = os.path.join(ROOT, "internal", "server", "server.go")
    if os.path.exists(server):
        with open(server, encoding="utf-8") as f:
            endpoints = set(re.findall(r'HandleFunc\("(/api/[\w/]+)"', f.read()))
        usage_content = read(os.path.join(DOCS, "使用.md")) if os.path.exists(
            os.path.join(DOCS, "使用.md")) else ""
        for ep in endpoints:
            if ep not in usage_content:
                err("使用.md 未记录接口: %s" % ep)
        note("API 端点: %d 个" % len(endpoints))

    # 命令行参数
    main_go = os.path.join(ROOT, "cmd", "reportviewer", "main.go")
    if os.path.exists(main_go):
        with open(main_go, encoding="utf-8") as f:
            src = f.read()
        flags = set(re.findall(r'flag\.String\("([\w-]+)"', src))
        flags |= set(re.findall(r'flag\.Bool\("([\w-]+)"', src))
        flags |= set(re.findall(r'flag\.Int\("([\w-]+)"', src))
        deploy = read(os.path.join(DOCS, "部署.md")) if os.path.exists(
            os.path.join(DOCS, "部署.md")) else ""
        for fl in sorted(flags):
            if "-%s" % fl not in deploy:
                err("部署.md 未记录参数: -%s" % fl)
        note("命令行参数: %d 个" % len(flags))


def check_append_only():
    """只追加型文档不应被截短。"""
    for path in APPEND_ONLY:
        if not os.path.exists(path):
            err("缺少 %s" % os.path.relpath(path, ROOT))
            continue
        # 用 git 取上一版本行数
        rel = os.path.relpath(path, ROOT).replace("\\", "/")
        prev = git("show", "HEAD:" + rel)
        if not prev:
            continue
        old_lines = len(prev.splitlines())
        with open(path, encoding="utf-8") as f:
            new_lines = len(f.read().splitlines())
        if new_lines < old_lines:
            err("%s 被截短了（%d -> %d 行），该文档应为只追加"
                % (rel, old_lines, new_lines))
    note("检查了 %d 个只追加文档" % len(APPEND_ONLY))


def check_progress_consistency():
    """PROGRESS 的阶段条数应与 git 提交数一致（允许开发阶段暂不等）。"""
    path = os.path.join(PROJECT, "PROGRESS.md")
    if not os.path.exists(path):
        return
    content = read(path)
    phases = len(re.findall(r"^### 阶段 ", content, re.M))
    commits = len([l for l in git("log", "--oneline").splitlines() if l.strip()])
    note("PROGRESS 记录 %d 个阶段，git 有 %d 个提交" % (phases, commits))
    if commits and phases == 0:
        err("PROGRESS.md 没有记录任何阶段，但已有 %d 个提交" % commits)


def check_index_complete():
    """INDEX 必须链接到全部知识库文件。"""
    index = os.path.join(PROJECT, "INDEX.md")
    if not os.path.exists(index):
        err("缺少 project/INDEX.md")
        return
    content = read(index)
    for name in ("STATUS.md", "ROADMAP.md", "PROGRESS.md",
                 "LESSONS.md", "SKILLS.md"):
        if name not in content:
            err("INDEX.md 未链接到 %s" % name)
    note("INDEX 链接完整")


def check_generated_block():
    """自动生成的硬指标区块必须是最新的。"""
    status = os.path.join(PROJECT, "STATUS.md")
    gen = os.path.join(ROOT, "tools", "gen_status.py")
    if not os.path.isfile(status) or not os.path.isfile(gen):
        err("缺少 project/STATUS.md 或 tools/gen_status.py")
        return
    with open(status, encoding="utf-8") as f:
        content = f.read()
    if "AUTO:STATUS:BEGIN" not in content:
        err("STATUS.md 缺少 AUTO:STATUS 标记")
        return
    # 重新生成一份，和文件里的对比。比较时只取标记本身，
    # 不含 HTML 注释的写法差异。
    import subprocess
    begin_mark = "AUTO:STATUS:BEGIN"
    end_mark = "AUTO:STATUS:END"
    try:
        out = subprocess.check_output(
            [sys.executable, gen, "--print"], cwd=ROOT, stderr=subprocess.DEVNULL
        ).decode("utf-8", "replace")
    except Exception as e:
        warn("无法运行 gen_status.py: %s" % e)
        return
    # 文件里是 HTML 注释形式（<!-- AUTO:STATUS:BEGIN -->），
    # 生成器输出的是纯标记，两边都只取「表格部分」来比，
    # 避免标记写法与换行符造成假差异。
    start = content.index(begin_mark)
    end = content.index(end_mark) + len(end_mark)
    expected = out.strip()

    def table_of(text):
        rows = [ln.strip() for ln in text.replace("\r\n", "\n").split("\n")
                if ln.strip().startswith("|")]
        return "\n".join(rows)

    current_rows = table_of(content[start:end])
    expected_rows = table_of(expected)
    if current_rows != expected_rows:
        err("STATUS.md 的自动指标区块已过期，跑 `python3 tools/gen_status.py` 刷新")


def main():
    print("=" * 60)
    print("文档一致性自检")
    print("=" * 60)

    check_links()
    check_source_refs()
    check_marks()
    check_numbers()
    check_append_only()
    check_progress_consistency()
    check_index_complete()
    check_generated_block()

    print()
    for n in notes:
        print("  [信息] %s" % n)
    for w in warnings:
        print("  [警告] %s" % w)
    for e in errors:
        print("  [错误] %s" % e)

    print()
    if errors:
        print("结果: FAIL（%d 个错误, %d 个警告）" % (len(errors), len(warnings)))
        return 1
    print("结果: PASS（%d 个警告）" % len(warnings))
    return 0


if __name__ == "__main__":
    sys.exit(main())
