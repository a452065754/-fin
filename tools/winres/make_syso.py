#!/usr/bin/env python3
"""Generate a Windows .syso file that embeds version information.

.. warning::
   **实验性，暂未接入构建流程。** Go 的链接器要求 .syso 是一个带完整
   COFF 头的对象文件，本脚本生成的资源节区目前还不能被链接器接受
   （报 "unexpected read from section with uninitialized data"）。

   杀软误报的问题已经由 ``-ldflags "-s -w"`` 解决，见
   ``tools/build_windows.sh``。版本资源只是锦上添花，等 COFF 格式对齐
   后再启用。

A Go binary without a digital signature and without PE version information
looks anonymous to heuristic scanners, so they flag it as an obfuscator.
Embedding the standard version resource would give the file the metadata a
release build is expected to have.
"""
import struct
import sys

# Fixed timestamp keeps the output reproducible.
TIMESTAMP = 0
TIME_DATE = 0x3A2E5C00  # 2000-01-01 00:00:00 UTC

# The metadata written into the exe.
PRODUCT_NAME = "报表查看器"
FILE_DESCRIPTION = "FineReport 11 报表离线查看器"
COMPANY_NAME = "QDAKE"
FILE_VERSION = "1.0.0.0"
PRODUCT_VERSION = "1.0.0.0"
LEGAL_COPYRIGHT = "Copyright (c) QDAKE"

# A fixed GUID for the version resource.
VS_VERSION_INFO_ID = (
    0xFEEF04BD, 0xA1F34C0F, 0x4E1F3B2A, 0x9C2D5E60
)
VAR_FILE_INFO_ID = 0xFEEF04BD
VARIABLE_NAME = "00000000-0000-0000-0000-000000000000"

# UTF-16LE strings, NUL terminated.
def utf16(text):
    return text.encode("utf-16-le") + b"\x00\x00"


def align(data, boundary=4):
    pad = (-len(data)) % boundary
    return data + b"\x00" * pad


def build_version_info():
    """Build the VS_VERSIONINFO structure."""
    # Fixed file info.
    file_info = struct.pack(
        "<IIIIIIIIII",
        0xFEEF04BD,       # signature
        0x00010000,       # struct version
        0x00010000,       # file version MS
        0x00000000,       # file version LS
        0x00010000,       # product version MS
        0x00000000,       # product version LS
        0x0000003F,       # file flags mask
        0x00000000,       # file flags
        0x00000004,       # file OS: VOS_NT_WINDOWS32
        0x00000001,       # file type: VFT_APP
    )
    file_info = align(file_info, 4)

    # String table with the fixed keys.
    strings = [
        ("CompanyName", COMPANY_NAME),
        ("FileDescription", FILE_DESCRIPTION),
        ("FileVersion", FILE_VERSION),
        ("InternalName", PRODUCT_NAME),
        ("LegalCopyright", LEGAL_COPYRIGHT),
        ("OriginalFilename", "reportviewer.exe"),
        ("ProductName", PRODUCT_NAME),
        ("ProductVersion", PRODUCT_VERSION),
    ]
    table = b""
    for key, value in strings:
        # A string entry: length, value length, type, then the UTF-16 text.
        key_b = utf16(key)
        val_b = utf16(value)
        entry = struct.pack("<HH", len(key_b) + 6, len(val_b))
        entry += key_b + b"\x00\x00"  # NUL terminated key
        entry = align(entry, 4)
        entry += struct.pack("<HH", len(val_b) + 6, 1)  # type 1 = text
        entry += val_b
        entry = align(entry, 4)
        table += entry
    table = align(table, 4)

    # StringFileInfo: header, then the table, then the children.
    children = b""
    children += struct.pack("<HH", 4 + 6, 1)
    children += struct.pack("<I", VAR_FILE_INFO_ID)
    children = align(children, 4)
    children += table
    children = align(children, 4)

    sfi = b""
    sfi += struct.pack("<HH", 0, 1)  # type 1 = text
    sfi += utf16("040904B0")  # language: US English, Unicode
    sfi = align(sfi, 4)
    sfi += children
    sfi = align(sfi, 4)

    # VarFileInfo: the translation entry.
    var_children = struct.pack("<HH", 4, 0)
    var_children += struct.pack("<I", 0x04B00409)  # 0x0409, 0x04B0
    var_children = align(var_children, 4)
    var_file_info = b""
    var_file_info += struct.pack("<HH", 0, 1)
    var_file_info += utf16("Translation")
    var_file_info = align(var_file_info, 4)
    var_file_info += var_children
    var_file_info = align(var_file_info, 4)

    # VS_VERSIONINFO: length, value length, type, then the key.
    key = utf16("VS_VERSION_INFO")
    head = struct.pack("<HHH", 0, len(FILE_VERSION), 0)
    head += key
    head = align(head, 4)
    head += struct.pack("<I", 0xFEEF04BD)  # value length
    head = align(head, 4)
    head += file_info
    head = align(head, 4)
    head += sfi
    head = align(head, 4)
    head += var_file_info
    head = align(head, 4)

    # The length field covers the whole structure.
    head = struct.pack("<H", len(head) + 4 - 6) + head[2:]
    return head


def build_manifest():
    """Build the application manifest."""
    manifest = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="QDAKE.ReportViewer" version="1.0.0.0" processorArchitecture="amd64"/>
  <description>FineReport 11 报表离线查看器</description>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
    </windowsSettings>
  </application>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
    </application>
  </compatibility>
</assembly>""".replace("\n", "").replace("\r", "")
    return manifest.encode("utf-8")


def build_resource_section():
    """Build the .rsrc section content."""
    entries = []  # (name, data)

    # Resource type 16 = VERSION, 24 = MANIFEST.
    version_data = build_version_info()
    manifest_data = build_manifest()

    # A resource directory is a tree of type -> name -> language -> data.
    def dir_entry(identifier, offset):
        name = struct.pack("<I", identifier)
        entry = name + struct.pack("<I", offset)
        return entry

    def dir_header(count):
        return struct.pack("<IIH", 0, 0, count) + b"\x00\x00"

    # Layout: type directory, name directory, language directory, data entries.
    # Everything is relative to the section start.
    header_size = 16
    type_entry_size = 8
    name_entry_size = 8
    lang_entry_size = 8
    data_entry_size = 16

    # Resource type 16 = VERSION, 24 = MANIFEST. Each type owns one blob.
    types = [(16, version_data), (24, manifest_data)]
    n_types = len(types)
    type_dir_size = header_size + type_entry_size * n_types
    name_dir_size = header_size + name_entry_size * n_types
    lang_dir_size = header_size + lang_entry_size * n_types
    data_entries_size = data_entry_size * n_types

    payload_start = type_dir_size + name_dir_size + lang_dir_size + data_entries_size
    payload_start = (payload_start + 7) & ~7

    # Place the payload blobs and remember where each one landed.
    payloads = []
    offset = payload_start
    for _, blob in types:
        payloads.append((blob, offset))
        offset += len(blob)
        offset = (offset + 7) & ~7

    # Type directory.
    type_dir = dir_header(n_types)
    for i, (rtype, _) in enumerate(types):
        name_off = type_dir_size + i * name_entry_size
        type_dir += dir_entry(rtype, name_off)
    type_dir += b"\x00" * (type_dir_size - len(type_dir))

    # Name directory.
    name_dir = dir_header(n_types)
    for i, (_, _) in enumerate(types):
        lang_off = type_dir_size + name_dir_size + i * lang_entry_size
        name_dir += dir_entry(1, lang_off)
    name_dir += b"\x00" * (name_dir_size - len(name_dir))

    # Language directory.
    lang_dir = dir_header(n_types)
    for i, (_, _) in enumerate(types):
        data_off = type_dir_size + name_dir_size + lang_dir_size + i * data_entry_size
        lang_dir += dir_entry(0x0409, data_off)
    lang_dir += b"\x00" * (lang_dir_size - len(lang_dir))

    # Data entries point at the payload.
    data_entries = b""
    for i, (blob, blob_off) in enumerate(payloads):
        data_entries += struct.pack("<III", blob_off, len(blob), 0)
    data_entries += b"\x00" * (data_entries_size - len(data_entries))

    section = type_dir + name_dir + lang_dir + data_entries
    section += b"\x00" * (payload_start - len(section))
    for blob, _ in payloads:
        section += blob
        pad = (-len(blob)) % 8
        section += b"\x00" * pad
    return section


def build_coff_object(res_data):
    """Wrap the resource section in a COFF object file for the Go linker."""
    # The linker expects a .syso: a COFF object with a .rsrc section.
    section_name = b".rsrc\x00\x00\x00"
    header_size = 20
    section_header_size = 40
    symbol_table_size = 0
    string_table_size = 4

    # The payload of .rsrc starts with a directory whose offsets are
    # relative to the start of the resource data.
    res = res_data

    file_header = struct.pack(
        "<HHIIIHH",
        0x8664,          # machine: AMD64
        1,               # number of sections
        TIMESTAMP,       # time stamp
        0,               # pointer to symbol table
        0,               # number of symbols
        0,               # optional header size
        0,               # characteristics
    )

    section = struct.pack(
        "<8sIIIIIIHHI",
        section_name,
        len(res),        # virtual size
        len(res),        # virtual address
        len(res),        # size of raw data
        0,               # pointer to raw data (patched below)
        0,               # pointer to relocations
        0,               # pointer to line numbers
        0,               # number of relocations
        0,               # number of line numbers
        0x40000040,      # characteristics: initialized data, readable
    )

    raw_offset = header_size + section_header_size
    section = section[:16] + struct.pack("<I", raw_offset) + section[20:]

    out = file_header + section + res
    out += struct.pack("<I", string_table_size)  # string table size
    return out


def main():
    out_path = sys.argv[1] if len(sys.argv) > 1 else "version.syso"
    res = build_resource_section()
    obj = build_coff_object(res)
    with open(out_path, "wb") as f:
        f.write(obj)
    print(f"已生成 {out_path} ({len(obj)} 字节)")


if __name__ == "__main__":
    main()
