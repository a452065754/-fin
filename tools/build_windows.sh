#!/usr/bin/env bash
# Build the report viewer for Windows.
#
# The link flags matter: without "-s -w" the Go linker keeps a .symtab
# section full of runtime symbol names, and heuristic scanners on Chinese
# Windows installs (Huorong, 360) flag the result as
# "HEUR:VirTool/Obfuscator.a" and delete it. Stripping the symbol table and
# the debug information removes the pattern they match on; the binary is
# otherwise unchanged and still runs.
#
# Usage:
#   tools/build_windows.sh [output.exe]
set -euo pipefail

cd "$(dirname "$0")/.."

OUT="${1:-bin/reportviewer.exe}"
mkdir -p "$(dirname "$OUT")"

echo "[1/2] 生成 Windows 可执行文件: $OUT"
go build -trimpath -ldflags "-s -w" -o "$OUT" ./cmd/reportviewer

echo "[2/2] 校验产物"
if [ ! -f "$OUT" ]; then
  echo "构建失败：产物不存在" >&2
  exit 1
fi
SIZE=$(wc -c < "$OUT")
echo "      大小: $((SIZE / 1024 / 1024)) MB"

# The stripped binary must not carry a symbol table, otherwise the scanner
# may flag it again. The section itself stays in the file (it is aligned to
# 512 bytes on disk), so the virtual size is what tells the truth: a real
# symbol table is tens of kilobytes, a stripped one is a few bytes.
python - "$OUT" <<'PY'
import struct
import sys

path = sys.argv[1]
with open(path, "rb") as f:
    d = f.read()
pe = struct.unpack("<I", d[0x3C:0x40])[0]
_, nsec, _, _, _, optsz, _ = struct.unpack("<HHIIIHH", d[pe + 4 : pe + 24])
sh = pe + 24 + optsz
sym = None
for i in range(nsec):
    e = sh + i * 40
    name = d[e : e + 8].rstrip(b"\x00").decode("latin1")
    if name == ".symtab":
        sym = struct.unpack("<I", d[e + 8 : e + 12])[0]
if sym is None:
    print("      未找到 .symtab 节区（正常）")
elif sym > 1024:
    print(f"      警告：.symtab 仍有 {sym} 字节符号，杀软可能再次误报", file=sys.stderr)
    sys.exit(1)
else:
    print(f"      .symtab 已清空（有效 {sym} 字节）")
PY

echo "完成: $OUT"
