"""
===========================================================================

import_rigid_scroll_rows.py - the Rigid L2 summon scrolls' item rows,
extracted from the ISRO-R shard backup (#3785)

The eight ITEM_RIGID_<X>_L2_SUMMON_SCROLL rows exist in no staged
client (the v1.150, VSRO and v1.696 Media tables were searched whole)
and in no ISRO-R refdata file export; they live only in the ISRO-R
shard database. This tool reads that backup directly - it is not a
SQL restore - and writes the rows in the 160-column itemdata export
shape the variant build stages (scripts/data/rigid-scroll-rows.json).

A row is two records: _RefObjCommon (58 columns: identity, names,
type, prices) and _RefObjItem (105 columns: requirements and the
twenty parameter value/description pairs), joined by the common
record's last fixed column (Link). A record is the SQL Server data
record the backup pages carry: a fixed-length head, a column count
and null bitmap, then the variable-length columns' end offsets and
their data. The fixed-column layout below was calibrated against
items the backup shares with the ISRO-R refdata file export (the
party summon scroll and a degree-11 sword agree byte for byte on
every field that did not change between the two snapshots).

Parameter 1's description is the monster the scroll summons;
parameter 4 pairs its value 600000 (milliseconds) with the
description COOLTIME:0x000000F1 - the family cooltime the server's
use path enforces. The extractor refuses a field it does not map
rather than dropping one silently.

Usage: py -3 scripts/build/import_rigid_scroll_rows.py <backup.bak>
       [--out scripts/data/rigid-scroll-rows.json]

===========================================================================
"""
import argparse
import hashlib
import json
import re
import struct
from pathlib import Path

SCROLLS = {
	"ITEM_RIGID_TIGERWOMAN_L2_SUMMON_SCROLL": "MOB_CH_TIGERWOMAN_L2",
	"ITEM_RIGID_KERBERUS_L2_SUMMON_SCROLL": "MOB_EU_KERBEROS_L2",
	"ITEM_RIGID_URUCHI_L2_SUMMON_SCROLL": "MOB_OA_URUCHI_L2",
	"ITEM_RIGID_IVY_L2_SUMMON_SCROLL": "MOB_AM_IVY_L2",
	"ITEM_RIGID_ISYUTARU_L2_SUMMON_SCROLL": "MOB_KK_ISYUTARU_L2",
	"ITEM_RIGID_BONELORD_L2_SUMMON_SCROLL": "MOB_TK_BONELORD_L2",
	"ITEM_RIGID_TAHOMET_L2_SUMMON_SCROLL": "MOB_RM_TAHOMET_L2",
	"ITEM_RIGID_BLACKSNAKE_L2_SUMMON_SCROLL": "MOB_TQ_BLACKSNAKE_L2",
}
COOLTIME_DESC = "COOLTIME:0x000000F1"
ITEM_COLUMNS = 160
FLOAT_COLUMNS = frozenset(range(63, 86)) | frozenset(range(95, 118))


# ================
# parse_record
#
# One SQL data record at byte r: fixed bytes and variable strings,
# or None when the bytes are not the record shape.
# ================
def parse_record(data, r):
	if r < 0 or data[r] not in (0x10, 0x30) or data[r + 1] != 0:
		return None
	fixed_end = struct.unpack_from("<H", data, r + 2)[0]
	if not 20 <= fixed_end <= 500:
		return None
	ncols = struct.unpack_from("<H", data, r + fixed_end)[0]
	if ncols not in (58, 105):
		return None
	bitmap = (ncols + 7) // 8
	vcount = struct.unpack_from("<H", data, r + fixed_end + 2 + bitmap)[0]
	if vcount not in (10, 20):
		return None
	arr_at = fixed_end + 2 + bitmap + 2
	arr = [struct.unpack_from("<H", data, r + arr_at + 2 * i)[0] for i in range(vcount)]
	strings = []
	prev = arr_at + 2 * vcount
	for end in arr:
		if end < prev or r + end > len(data):
			return None
		strings.append(data[r + prev:r + end])
		prev = end
	return {"fixed": data[r + 4:r + fixed_end], "ncols": ncols, "strings": strings}


# ================
# find_common
#
# The _RefObjCommon record whose first variable column is codename.
# ================
def find_common(data, codename):
	name = codename.encode()
	for x in (m.start() for m in re.finditer(re.escape(name), data)):
		backward = [struct.unpack_from("<H", data, x - 2 * i)[0] for i in range(1, 12)]
		if backward[10] != 10:
			continue
		offsets = sorted(backward[:10])
		d_rel = offsets[0] - len(name)
		if d_rel < 30 or d_rel > 400:
			continue
		got = parse_record(data, x - d_rel)
		if got and got["ncols"] == 58 and got["strings"][0] == name:
			return got
	return None


# ================
# find_item
#
# The _RefObjItem record whose identity is item_id.
# ================
def find_item(data, item_id):
	needle = struct.pack("<i", item_id)
	for m in re.finditer(re.escape(needle), data):
		got = parse_record(data, m.start() - 4)
		if got and got["ncols"] == 105 and struct.unpack_from("<i", got["fixed"], 0)[0] == item_id:
			return got
	return None


# ================
# export_row
#
# The 160-column export row for one scroll from its two records.
# Fields the calibration did not map must be filler (zero, -1, an
# empty float or 'xxx'); anything else refuses the extraction.
# ================
def export_row(codename, common, item):
	fixed_c, fixed_i = common["fixed"], item["fixed"]
	texts = [s.decode("cp949") for s in common["strings"]]
	descs = [s.decode("cp949") for s in item["strings"]]
	row = ["0"] * ITEM_COLUMNS
	for col in FLOAT_COLUMNS:
		row[col] = "0.0"

	def put(col, value):
		row[col] = str(value)

	put(0, struct.unpack_from("<i", fixed_c, 4)[0])
	put(1, struct.unpack_from("<i", fixed_c, 0)[0])
	for col, text in zip(range(2, 7), texts[:5]):
		put(col, text)
	for col, at in ((7, 8), (8, 9), (9, 10), (10, 11), (11, 12), (12, 13),
			(14, 18), (15, 19), (16, 20), (17, 21), (18, 22), (19, 23),
			(20, 24), (21, 25), (22, 26), (23, 27), (24, 28), (25, 29), (33, 58)):
		put(col, fixed_c[at])
	for col, at in ((13, 14), (26, 30), (27, 34), (28, 38), (29, 42),
			(30, 46), (31, 50), (32, 54), (48, 92), (49, 96), (50, 100)):
		put(col, struct.unpack_from("<i", fixed_c, at)[0])
	for col, at in ((34, 59), (36, 64), (38, 69), (40, 74)):
		put(col, struct.unpack_from("<i", fixed_c, at)[0])
	for col, at in ((35, 63), (37, 68), (39, 73)):
		put(col, fixed_c[at])
	for col, text in zip(range(52, 57), texts[5:]):
		put(col, text)

	put(57, struct.unpack_from("<i", fixed_i, 4)[0])
	put(58, struct.unpack_from("<i", fixed_i, 8)[0])
	put(59, fixed_i[16])
	put(61, fixed_i[17])
	put(62, fixed_i[18])
	put(92, fixed_i[120])
	put(93, fixed_i[121])
	put(94, fixed_i[122])
	for k, col in enumerate(range(95, 118)):
		put(col, format_float(struct.unpack_from("<f", fixed_i, 124 + 4 * k)[0]))
	for k in range(20):
		put(118 + 2 * k, struct.unpack_from("<i", fixed_i, 216 + 4 * k)[0])
		put(119 + 2 * k, descs[k])
	put(158, struct.unpack_from("<i", fixed_i, 296)[0])

	check_filler(codename, fixed_c, fixed_i)
	if texts[0] != codename or descs[0] != SCROLLS[codename] or descs[3] != COOLTIME_DESC:
		raise ValueError(f"{codename}: unexpected codename, monster link or cooltime")
	return row


# ================
# format_float
#
# An export float. Denormals (the Isyutaru scroll's column 117
# carries 2.8e-45 where its siblings carry zero) write as zero.
# ================
def format_float(value):
	if abs(value) < 1e-30:
		return "0.0"
	return repr(value)


# ================
# check_filler
#
# Every fixed byte outside the mapped fields must be filler, so a
# backup that moves a real value into an unmapped field fails loudly.
# ================
def check_filler(codename, fixed_c, fixed_i):
	mapped_c = set()
	for at, size in ((0, 4), (4, 4), (14, 4), (30, 4), (34, 4), (38, 4), (42, 4), (46, 4),
			(50, 4), (54, 4), (59, 4), (64, 4), (69, 4), (74, 4), (92, 4), (96, 4), (100, 4),
			(104, 4)):
		mapped_c.update(range(at, at + size))
	mapped_c.update(range(8, 14))
	mapped_c.update((18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 58, 63, 68, 73))
	mapped_i = set()
	for at, size in ((0, 4), (4, 4), (8, 4), (296, 4)):
		mapped_i.update(range(at, at + size))
	mapped_i.update((16, 17, 18, 120, 121, 122))
	mapped_i.update(range(124, 216))
	mapped_i.update(range(216, 296))
	for at, byte in enumerate(fixed_c):
		if at not in mapped_c and byte not in (0, 0xFF):
			raise ValueError(f"{codename}: unmapped common byte {at} = {byte}")
	for at, byte in enumerate(fixed_i):
		if at not in mapped_i and byte not in (0, 0xFF):
			raise ValueError(f"{codename}: unmapped item byte {at} = {byte}")


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description="Extract the Rigid summon scroll rows from an ISRO-R shard backup.")
	parser.add_argument("backup", type=Path)
	parser.add_argument("--out", type=Path, default=Path(__file__).resolve().parent.parent / "data" / "rigid-scroll-rows.json")
	args = parser.parse_args()

	data = args.backup.read_bytes()
	rows = []
	for codename in sorted(SCROLLS):
		common = find_common(data, codename)
		if common is None:
			raise SystemExit(f"no _RefObjCommon record for {codename}")
		link = struct.unpack_from("<i", common["fixed"], len(common["fixed"]) - 4)[0]
		item = find_item(data, link)
		if item is None:
			raise SystemExit(f"no _RefObjItem record {link} for {codename}")
		row = export_row(codename, common, item)
		rows.append({"codename": codename, "monster": SCROLLS[codename], "row": row})

	document = {"format": "opensro-rigid-scroll-rows-v1",
		"sha256": hashlib.sha256(data).hexdigest(),
		"source": args.backup.name,
		"rows": rows}
	args.out.parent.mkdir(parents=True, exist_ok=True)
	args.out.write_bytes((json.dumps(document, ensure_ascii=False, indent="\t") + "\n").encode("utf-8"))
	print(f"wrote {args.out}: {len(rows)} scroll rows")


if __name__ == "__main__":
	main()
