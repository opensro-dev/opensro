"""
===========================================================================

extract_client_cursors.py - the native cursors from SRO_Client.exe

The native cursor manager (sub_a15af0 / CursorManager_SetCursorByParam) calls
LoadCursorA(hInstance, MAKEINTRESOURCE(param)) - the interaction cursor param
IS the RT_GROUP_CURSOR resource ordinal. The port serves these as
/assets/cursors/sro_client_cursor_0x<id>.cur (0x95 arrow already extracted;
0x99 pickup / 0x9a pickup-pressed are the sub_67a410 params the ground-drop
hover uses).

Usage: py scripts/extract_client_cursors.py [ids...]
Defaults to every cursor referenced by the browser client. SRO_Client.exe
is read from the game root (sro_paths.GAME_ROOT).

Roots come from sro_paths.py.

===========================================================================
"""

import struct
import sys
from pathlib import Path

import pefile

from sro_paths import GAME_ROOT, REPO_ROOT

RT_CURSOR = 1
RT_GROUP_CURSOR = 12

CLIENT_EXE = GAME_ROOT / "SRO_Client.exe"
OUT_DIR = REPO_ROOT / ".generated" / "client-public" / "assets" / "cursors"


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

    entries = []
    for index in range(count):
        width, height, planes, bit_count, bytes_in_res, ordinal = struct.unpack_from(
            "<HHHHIH", group, 6 + index * 14
        )
        cursor_struct = cursors.get(ordinal)
        if cursor_struct is None:
            continue
        blob = read_resource(pe, cursor_struct)
        # RT_CURSOR payload: WORD xHotspot, WORD yHotspot, then the DIB.
        x_hot, y_hot = struct.unpack_from("<HH", blob, 0)
        dib = blob[4:]
        entries.append((width, height, bit_count, x_hot, y_hot, dib))

    if not entries:
        return None

    # .cur file: ICONDIR (type 2) + ICONDIRENTRY per image + DIB payloads.
    out = bytearray(struct.pack("<HHH", 0, 2, len(entries)))
    offset = 6 + 16 * len(entries)
    payloads = []
    for width, height, bit_count, x_hot, y_hot, dib in entries:
        # GRPCURSORDIR heights are DOUBLED (mask included); the ICONDIRENTRY
        # wants the visual height. Width/height bytes: 0 encodes 256.
        visual_height = height // 2
        out += struct.pack(
            "<BBBBHHII",
            width & 0xFF,
            visual_height & 0xFF,
            0,
            0,
            x_hot,
            y_hot,
            len(dib),
            offset,
        )
        payloads.append(dib)
        offset += len(dib)
    for dib in payloads:
        out += dib
    return bytes(out)


# ================
# main
# ================
def main() -> int:
    ids = [int(arg, 0) for arg in sys.argv[1:]] or [
        0x95,
        0x97,
        0x98,
        0x99,
        0x9A,
        0xA0,
        0xA1,
        0xA3,
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
        target = OUT_DIR / f"sro_client_cursor_0x{group_id:x}.cur"
        target.write_bytes(blob)
        print(f"wrote {target.name} ({len(blob)} bytes)")
    # List every available group-cursor ordinal for reference.
    available = sorted(resource_entries(pe, RT_GROUP_CURSOR).keys())
    print(f"available group-cursor ids: {[hex(i) for i in available]}")
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
