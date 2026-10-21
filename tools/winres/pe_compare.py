#!/usr/bin/env python3
"""Compare the PE layout of two Windows executables.

Used to find out which feature makes a scanner flag one binary and not the
other. The output lists the headers, the section table and a few flags that
heuristic engines look at.
"""
import struct
import sys


def analyze(path):
    with open(path, "rb") as f:
        d = f.read()
    pe = struct.unpack("<I", d[0x3C:0x40])[0]
    machine, nsec, ts, symptr, nsym, optsz, chars = struct.unpack(
        "<HHIIIHH", d[pe + 4 : pe + 24])
    opt = pe + 24
    magic = struct.unpack("<H", d[opt : opt + 2])[0]
    pe32p = magic == 0x20B
    print(f"--- {path} ---")
    print(f"  大小: {len(d) / 1024 / 1024:.2f} MB   节区数: {nsec}   PE32+: {pe32p}")
    if pe32p:
        entry = struct.unpack("<I", d[opt + 16 : opt + 20])[0]
        imagebase = struct.unpack("<Q", d[opt + 24 : opt + 32])[0]
        subsystem = struct.unpack("<H", d[opt + 68 : opt + 70])[0]
        dllchars = struct.unpack("<H", d[opt + 70 : opt + 72])[0]
        imp_rva, imp_sz = struct.unpack("<II", d[opt + 120 : opt + 128])
    else:
        entry = struct.unpack("<I", d[opt + 16 : opt + 20])[0]
        imagebase = struct.unpack("<I", d[opt + 28 : opt + 32])[0]
        subsystem = struct.unpack("<H", d[opt + 68 : opt + 70])[0]
        dllchars = struct.unpack("<H", d[opt + 70 : opt + 72])[0]
        imp_rva, imp_sz = struct.unpack("<II", d[opt + 104 : opt + 112])
    print(f"  入口点 RVA: 0x{entry:X}   ImageBase: 0x{imagebase:X}   Subsystem: {subsystem}")
    print(
        f"  DllCharacteristics: 0x{dllchars:04x} "
        f"(ASLR={bool(dllchars & 0x40)} DEP={bool(dllchars & 0x100)} "
        f"CFG={bool(dllchars & 0x4000)})"
    )
    sh = opt + optsz
    print("  节区:")
    for i in range(nsec):
        e = sh + i * 40
        name = d[e : e + 8].rstrip(b"\x00").decode("latin1")
        vsize, vaddr, rsize = struct.unpack("<III", d[e + 8 : e + 20])
        c = struct.unpack("<I", d[e + 36 : e + 40])[0]
        w = "X" if c & 0x80000000 else ("R" if c & 0x40000000 else "-")
        w += "X" if c & 0x20000000 else "-"
        w += "X" if c & 0x10000000 else "-"
        print(f"    {name:10s} vsize={vsize:9d} rsize={rsize:9d} {w} 0x{c:08x}")
    print(f"  导入表 RVA: 0x{imp_rva:X}  大小: {imp_sz}")
    # Count the imported DLL names.
    names = []
    for m in range(0, len(d) - 4):
        pass
    print()


if __name__ == "__main__":
    for p in sys.argv[1:]:
        analyze(p)
