"""
===========================================================================

extract_client_cursors.py - the native cursors from SRO_Client.exe

The native cursor manager (sub_a15af0 / CursorManager_SetCursorByParam) calls
LoadCursorA(hInstance, MAKEINTRESOURCE(param)) - the interaction cursor param
IS the RT_GROUP_CURSOR resource ordinal. The port serves these as
/assets/cursors/sro_client_cursor_0x<id>.png, decoded from the native DIB:
every browser draws a PNG cursor, while .cur support differs (Safari drew no
cursor at all, BUG-068). 0x95 is the arrow; 0x99 pickup / 0x9a
pickup-pressed are the sub_67a410 params the ground-drop hover uses.

Usage: py scripts/extract_client_cursors.py [ids...]
Defaults to every cursor referenced by the browser client. SRO_Client.exe
is read from the game root (sro_paths.GAME_ROOT).

Roots come from sro_paths.py.

===========================================================================
"""

import struct
import sys
import zlib
from pathlib import Path

import pefile

from sro_paths import GAME_ROOT, PUBLIC_ROOT

RT_CURSOR = 1
RT_GROUP_CURSOR = 12

CLIENT_EXE = GAME_ROOT / "SRO_Client.exe"
OUT_DIR = PUBLIC_ROOT / "assets" / "cursors"

PNG_SIGNATURE = bytes([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A])
PNG_FILTER_NONE = bytes([0])
PNG_RGBA = 6


# ================
# resource_entries
# ================
def resource_entries(pe, resource_type):
    for entry in pe.DIRECTORY_ENTRY_RESOURCE.entries:
        if entry.id == resource_type:
            return {
                child.id: child.directory.entries[0].data.struct
                for child in entry.directory.entries
            }
    return {}


# ================
# read_resource
# ================
def read_resource(pe, data_struct) -> bytes:
    return pe.get_data(data_struct.OffsetToData, data_struct.Size)


# ================
# extract_cursor
#
# The cursor group as a PNG, or None when the executable lacks it. The
# browser draws one image: the group's first entry, the 32x32 cursor
# LoadCursorA picks on a standard display.
# ================
def extract_cursor(pe, group_id: int) -> bytes | None:
    groups = resource_entries(pe, RT_GROUP_CURSOR)
    cursors = resource_entries(pe, RT_CURSOR)
    group_struct = groups.get(group_id)
    if group_struct is None:
        return None
    group = read_resource(pe, group_struct)

    # GRPCURSORDIR: idReserved, idType(2), idCount, then per entry (14 bytes):
    # wWidth, wHeight (full height), wPlanes, wBitCount, dwBytesInRes, wOrdinal.
    _, id_type, count = struct.unpack_from("<HHH", group, 0)
    if id_type != 2 or count == 0:
        return None
    _, _, _, _, _, ordinal = struct.unpack_from("<HHHHIH", group, 6)
    cursor_struct = cursors.get(ordinal)
    if cursor_struct is None:
        return None
    # RT_CURSOR payload: WORD xHotspot, WORD yHotspot, then the DIB. The
    # hotspots live beside the cursor ids in the client (platform/ui/cursor.ts).
    return cursor_png(read_resource(pe, cursor_struct)[4:])


# ================
# dib_rgba
#
# An RT_CURSOR DIB is a BITMAPINFOHEADER whose height counts the colour rows
# and the AND mask rows together, a palette for indexed depths, the bottom-up
# colour rows, then the bottom-up 1-bpp mask. A set mask bit is a transparent
# pixel (or an inverted one, which a browser cannot draw; there it is
# transparent too). Rows pad to 4 bytes.
# ================
def dib_rgba(dib: bytes):
    header_size, width, double_height, _, bit_count, compression = struct.unpack_from("<IiiHHI", dib, 0)
    if compression != 0 or bit_count not in (1, 4, 8, 24, 32):
        raise ValueError(f"unsupported cursor DIB: {bit_count} bpp, compression {compression}")
    height = double_height // 2
    colors_used = struct.unpack_from("<I", dib, 32)[0]
    palette_count = (colors_used or (1 << bit_count)) if bit_count <= 8 else 0
    palette = [dib[header_size + 4 * i:header_size + 4 * i + 3] for i in range(palette_count)]
    color_offset = header_size + 4 * palette_count
    color_stride = ((width * bit_count + 31) // 32) * 4
    mask_offset = color_offset + color_stride * height
    mask_stride = ((width + 31) // 32) * 4
    rgba = bytearray(width * height * 4)
    for y in range(height):
        source = height - 1 - y
        row = dib[color_offset + source * color_stride:color_offset + (source + 1) * color_stride]
        mask = dib[mask_offset + source * mask_stride:mask_offset + (source + 1) * mask_stride]
        for x in range(width):
            alpha = 255
            if bit_count == 32:
                blue, green, red, alpha = row[4 * x:4 * x + 4]
            elif bit_count == 24:
                blue, green, red = row[3 * x:3 * x + 3]
            else:
                per_byte = 8 // bit_count
                shift = (per_byte - 1 - x % per_byte) * bit_count
                index = (row[x // per_byte] >> shift) & ((1 << bit_count) - 1)
                blue, green, red = palette[index]
            if (mask[x // 8] >> (7 - x % 8)) & 1:
                alpha = 0
            rgba[4 * (y * width + x):4 * (y * width + x) + 4] = bytes((red, green, blue, alpha))
    return width, height, bytes(rgba)


# ================
# png_chunk
# ================
def png_chunk(kind: bytes, data: bytes) -> bytes:
    return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)


# ================
# cursor_png
#
# The cursor as an 8-bit RGBA PNG with the DIB's pixels and transparency.
# ================
def cursor_png(dib: bytes) -> bytes:
    width, height, rgba = dib_rgba(dib)
    stride = width * 4
    raw = b"".join(PNG_FILTER_NONE + rgba[y * stride:(y + 1) * stride] for y in range(height))
    header = struct.pack(">IIBBBBB", width, height, 8, PNG_RGBA, 0, 0, 0)
    return PNG_SIGNATURE + png_chunk(b"IHDR", header) + png_chunk(b"IDAT", zlib.compress(raw, 9)) + png_chunk(b"IEND", b"")


# ================
# main
# ================
def main() -> int:
    ids = [int(arg, 0) for arg in sys.argv[1:]] or [
        0x95,
        0x96,
        0x97,
        0x98,
        0x99,
        0x9A,
        0xA0,
        0xA1,
        0xA3,
        0xA6,
    ]
    pe = pefile.PE(str(CLIENT_EXE))
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    failures = 0
    for group_id in ids:
        blob = extract_cursor(pe, group_id)
        if blob is None:
            print(f"MISSING group cursor 0x{group_id:x}")
            failures += 1
            continue
        target = OUT_DIR / f"sro_client_cursor_0x{group_id:x}.png"
        target.write_bytes(blob)
        print(f"wrote {target.name} ({len(blob)} bytes)")
    # List every available group-cursor ordinal for reference.
    available = sorted(resource_entries(pe, RT_GROUP_CURSOR).keys())
    print(f"available group-cursor ids: {[hex(i) for i in available]}")
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
