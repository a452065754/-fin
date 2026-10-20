#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""生成项目状态快照的硬指标部分。

STATUS.md 里的数字如果靠人手维护，一定会过期。本脚本从代码里直接
统计，把结果写进 STATUS.md 的自动生成区块（用标记注释圈起来）。

用法：
    python3 tools/gen_status.py            # 写入 STATUS.md
    python3 tools/gen_status.py --print    # 只打印，不写文件
"""

import argparse
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

BEGIN = "AUTO:STATUS:BEGIN"
END = "AUTO:STATUS:END"


def run(*args):
    try:
        return subprocess.check_output(
            list(args), cwd=ROOT, stderr=subprocess.DEVNULL
        ).decode("utf-8", "replace")
    except Exception:
        return ""


def count_lines(paths):
    total = 0
    for p in paths:
        full = os.path.join(ROOT, p)
        if not os.path.isfile(full):
            continue
        with open(full, "rb") as f:
            total += f.read().count(b"\n")
    return total


def count_tests():
    total = 0
    for pkg in ("internal/fr", "internal/server"):
        pkgdir = os.path.join(ROOT, pkg)
        if not os.path.isdir(pkgdir):
            continue
        for name in os.listdir(pkgdir):
            if name.endswith("_test.go"):
                with open(os.path.join(pkgdir, name), encoding="utf-8") as f:
                    total += len(re.findall(r"^func Test", f.read(), re.M))
    return total


def count_api_endpoints():
    path = os.path.join(ROOT, "internal", "server", "server.go")
    if not os.path.isfile(path):
        return 0
    with open(path, encoding="utf-8") as f:
        return len(set(re.findall(r'HandleFunc\("(/api/[\w/]+)"', f.read())))


def count_chart_kinds():
    path = os.path.join(ROOT, "internal", "fr", "chart.go")
    if not os.path.isfile(path):
        return 0
    with open(path, encoding="utf-8") as f:
        return len(set(re.findall(r'spec\.Kind = "(\w+)"', f.read())))


def count_env_vars():
    """统计部署文档里记录的环境变量。"""
    found = set()
    for rel in ("internal/server", "cmd/reportviewer", "internal/fr"):
        pkgdir = os.path.join(ROOT, rel)
        if not os.path.isdir(pkgdir):
            continue
        for name in os.listdir(pkgdir):
            if not name.endswith(".go"):
                continue
            with open(os.path.join(pkgdir, name), encoding="utf-8") as f:
                found |= set(re.findall(r'os\.Getenv\("(\w+)"\)', f.read()))
    return sorted(found)


def build_block():
    go_lines = count_lines([
        "cmd/reportviewer/main.go",
    ] + ["internal/fr/" + n for n in os.listdir(os.path.join(ROOT, "internal", "fr"))
         if n.endswith(".go")] +
        ["internal/server/" + n for n in os.listdir(os.path.join(ROOT, "internal", "server"))
         if n.endswith(".go")])

    web_lines = count_lines([
        "web/app.js", "web/style.css", "web/index.html",
    ])

    tests = count_tests()
    endpoints = count_api_endpoints()
    kinds = count_chart_kinds()
    envs = count_env_vars()

    lines = [
        "<!-- " + BEGIN + " -->",
        "",
        "<!-- 本区块由 tools/gen_status.py 生成，请勿手改。改完跑一次即可。 -->",
        "",
        "| 指标 | 值 | 怎么测的 |",
        "|---|---|---|",
        "| 测试用例 | %d 例 | `go test ./...` |" % tests,
        "| 图表类型 | %d 种 | `internal/fr/chart.go` 的 `spec.Kind` 枚举 |" % kinds,
        "| HTTP 接口 | %d 个 | `internal/server/server.go` 的路由表 |" % endpoints,
        "| Go 代码 | %d 行 | `wc -l` |" % go_lines,
        "| 前端代码 | %d 行 | `wc -l` |" % web_lines,
        "| 可配置环境变量 | %d 个 | %s |" % (len(envs), "、".join("`%s`" % e for e in envs)),
        "",
        "<!-- " + END + " -->",
    ]
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--print", action="store_true", help="只打印，不写文件")
    args = ap.parse_args()

    block = build_block()
    if args.print:
        print(block)
        return 0

    status = os.path.join(ROOT, "project", "STATUS.md")
    if not os.path.isfile(status):
        print("找不到 project/STATUS.md", file=sys.stderr)
        return 1
    with open(status, encoding="utf-8") as f:
        content = f.read()
    if BEGIN not in content or END not in content:
        print("STATUS.md 里缺少自动生成区块的标记", file=sys.stderr)
        return 1
    start = content.index(BEGIN)
    end = content.index(END) + len(END)
    content = content[:start] + block + content[end:]
    with open(status, "w", encoding="utf-8") as f:
        f.write(content)
    print("已更新 project/STATUS.md 的硬指标")
    return 0


if __name__ == "__main__":
    sys.exit(main())
