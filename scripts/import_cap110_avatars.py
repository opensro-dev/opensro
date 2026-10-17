"""
===========================================================================

import_cap110_avatars.py - bring mall avatar sets from a newer client into an
overlay beside the v1.150 extraction

The port's items, names and models come from the v1.150 extraction, which is
never edited. A newer (cap-110) client carries mall avatar sets that v1.150
never shipped. This tool reads whole sets, for example `piratecrew`, from that
client and writes them as an overlay:

	<game root>/extracted/overlays/cap110/<set>/
		Data_extracted/...   models, materials, textures (new files)
		Media_extracted/...  inventory icons (new files)
		Media_extracted/server_dep/silkroad/textdata/
			ItemData_30000.txt          the item rows (a new file)
			itemdata.txt                its index line
			textdataname.txt            English names and tooltips (rows to append)
			refpackageitem.txt          Item Mall packages (rows to append)
			refpricepolicyofitem.txt    their prices (rows to append)
			refscrapofpackageitem.txt   their scraps (rows to append)

scripts/merge_extraction_overlays.py then builds extracted-merged from the
retail tree plus the overlay, and SRO_EXTRACTION_OVERLAYS=cap110 points the
build at it. Without that variable the build is retail exactly.

The donor's item IDs are kept. They must be unused in the extraction (v1.150
reuses some low IDs for different items), so a collision stops the import.
Only mall avatar rows of the named set are taken; event copies are left out.
The packages take the v1.150 outfit prices (dress 130, hat 30, attachment 15
silk), not the donor's. They are only listed in the mall when the server runs
with SRO_MALL_EXTRA_GOODS=on,cap110. Nothing is written into either archive.
A run replaces the set's overlay folder; running it again changes nothing.

	py -3 scripts/import_cap110_avatars.py <donor client folder> piratecrew [more sets]
	py -3 scripts/import_cap110_avatars.py <donor client folder> piratecrew --dry-run

The game root is SRO_GAME_ROOT or the parent of the checkout (sro_paths.py).

===========================================================================
"""
import argparse
import json
import mmap
import re
import shutil
import sys
from pathlib import Path

import sro_pk2
from merge_extraction_overlays import EXTRACTED, OVERLAYS, TEXTDATA, encode_text, read_rows, read_text, records
from prepare_client_resources import safe_target, write_atomically

DONOR_TEXTDATA = "server_dep/silkroad/textdata"
OVERLAY_NAME = "cap110"
ITEM_FILE = "ItemData_30000.txt"
# Column layout shared by the donor and the extraction (160 cells).
ID, CODE, NAME_KEY, DESC_KEY = 1, 2, 5, 6
# Donor name rows: active flag, symbol, Korean, ..., English at 8.
DONOR_ENGLISH = 8
# Mall tables: file -> cell holding the package codename, and the row width
# once the donor row is adapted. The v1.150 mall rows all carry service 11.
PACKAGE_TABLES = {"refpackageitem.txt": 3, "refpricepolicyofitem.txt": 2, "refscrapofpackageitem.txt": 2}
PACKAGE_WIDTHS = {"refpackageitem.txt": 17, "refpricepolicyofitem.txt": 13, "refscrapofpackageitem.txt": 29}
LOCAL_SERVICE = "11"
# Silk prices of the v1.150 outfit tiers (the European wedding set's): the
# donor's later prices are not used.
SILK_CURRENCY = "2"
SILK_ATTACH, SILK_HAT, SILK_DRESS = "15", "30", "130"
PACKAGE_ID = 2       # refpackageitem: unique package ID
SCRAP_ID = -1        # refscrapofpackageitem: unique scrap ID, the last cell


#============================================================================
# set_pattern
#============================================================================
def set_pattern(name):
	"""Mall rows and files of one set: dress, hat, attach and numbered recolours."""
	name = re.escape(name.lower())
	return re.compile(rf"avatar_[mw]_{name}(?:_hat|_attach)?(?:_\d+)*$", re.IGNORECASE)


#============================================================================
# donor_rows
#============================================================================
def donor_rows(media, entries, pattern):
	"""Mall item rows of the set, and the donor's symbol -> name row."""
	items, names = [], {}
	for entry in entries:
		folder, _, base = entry.path.lower().rpartition("/")
		if folder != DONOR_TEXTDATA:
			continue
		if re.fullmatch(r"itemdata_\d+\.txt", base):
			for line in records(read_text(sro_pk2.payload(media, entry))):
				cells = line.split("\t")
				if len(cells) == 160 and cells[CODE].upper().startswith("ITEM_MALL_AVATAR_") \
					and pattern.search(cells[CODE][len("ITEM_MALL_"):]):
					items.append(cells)
		elif base == "textdata_object.txt":
			for line in records(read_text(sro_pk2.payload(media, entry))):
				cells = line.split("\t")
				if len(cells) > DONOR_ENGLISH and cells[1].startswith("SN_ITEM_MALL_AVATAR_"):
					names[cells[1]] = cells
	return items, names


#============================================================================
# silk_price
#============================================================================
def silk_price(code):
	"""The v1.150 tier of a package: attachment 15, hat 30, dress 130 silk."""
	stem = re.sub(r"(?:_\d+)+$", "", code)
	if stem.endswith("_ATTACH"):
		return SILK_ATTACH
	return SILK_HAT if stem.endswith("_HAT") else SILK_DRESS


#============================================================================
# package_rows
#============================================================================
def package_rows(media, entries, pattern):
	"""
	The set's Item Mall package, price and scrap rows, in the extraction's
	layout. The donor's price rows carry one extra cell after the currency;
	every row keeps the extraction's service (11) and flag (0) values.
	"""
	rows = {name: [] for name in PACKAGE_TABLES}
	for entry in entries:
		folder, _, base = entry.path.lower().rpartition("/")
		if folder != DONOR_TEXTDATA or base not in PACKAGE_TABLES:
			continue
		for line in records(read_text(sro_pk2.payload(media, entry))):
			cells = line.split("\t")
			code = cells[PACKAGE_TABLES[base]] if len(cells) > PACKAGE_TABLES[base] else ""
			if not code.upper().startswith("PACKAGE_ITEM_MALL_AVATAR_") \
				or not pattern.search(code[len("PACKAGE_ITEM_MALL_"):]):
				continue
			cells[1] = LOCAL_SERVICE
			if base == "refpackageitem.txt":
				cells[4] = "0"
			elif base == "refpricepolicyofitem.txt":
				del cells[4]
				if cells[3] == SILK_CURRENCY:
					cells[4] = silk_price(code)
			rows[base].append(cells)
	for name, width in PACKAGE_WIDTHS.items():
		if any(len(cells) != width for cells in rows[name]):
			raise SystemExit(f"{name}: a donor row does not fit the v1.150 layout ({width} cells).")
	return rows


#============================================================================
# check_free
#============================================================================
def check_free(items, packages):
	"""Item, package and scrap IDs must not already name something in v1.150."""
	text_root = EXTRACTED / TEXTDATA
	taken = {}
	for path in text_root.glob("[Ii]tem[Dd]ata_*.txt"):
		for row in read_rows(path):
			cells = row.split("\t")
			if len(cells) > CODE:
				taken[cells[ID]] = cells[CODE]
	for cells in items:
		if cells[ID] in taken:
			raise SystemExit(f"Item ID {cells[ID]} is already {taken[cells[ID]]} in v1.150; refusing {cells[CODE]}.")
	for name, id_cell in (("refpackageitem.txt", PACKAGE_ID), ("refscrapofpackageitem.txt", SCRAP_ID)):
		used = {row.split("\t")[id_cell] for row in read_rows(text_root / name) if row}
		for cells in packages[name]:
			if cells[id_cell] in used:
				raise SystemExit(f"{name}: ID {cells[id_cell]} is already used in v1.150; "
					f"refusing {cells[PACKAGE_TABLES[name]]}.")


#============================================================================
# write_set
#============================================================================
def write_set(directory, items, names, packages, files):
	"""Replace the set's overlay folder with its files and rows."""
	if directory.exists():
		shutil.rmtree(directory)
	text = directory / TEXTDATA
	for entry, root, payload in files:
		write_atomically(safe_target(directory / root, entry.path), bytes(payload))
	write_atomically(text / ITEM_FILE, encode_text(["\t".join(cells) for cells in sorted(items, key=lambda c: int(c[ID]))]))
	write_atomically(text / "itemdata.txt", encode_text([ITEM_FILE]))
	name_rows = {}
	for cells in items:
		for key in (cells[NAME_KEY], cells[DESC_KEY]):
			if key not in names:
				raise SystemExit(f"The donor has no text for {key}.")
			row = ["1", key, names[key][2], "", "", "0", "", "0", names[key][DONOR_ENGLISH], "", ""]
			name_rows[key] = "\t".join(row)
	write_atomically(text / "textdataname.txt", encode_text(list(name_rows.values())))
	for name, rows in packages.items():
		write_atomically(text / name, encode_text(["\t".join(cells) for cells in rows]))
	(directory / "overlay.json").write_text(json.dumps({
		"overlay": OVERLAY_NAME,
		"set": directory.name,
		"items": {cells[ID]: cells[CODE] for cells in items},
		"files": len(files),
	}, indent=2) + "\n", encoding="utf-8")


#============================================================================
# main
#============================================================================
def main():
	parser = argparse.ArgumentParser(description="Import mall avatar sets from a newer client into an overlay.")
	parser.add_argument("donor", type=Path, help="Folder holding the newer client's Data.pk2 and Media.pk2.")
	parser.add_argument("sets", nargs="+", help="Set names such as piratecrew.")
	parser.add_argument("--dry-run", action="store_true", help="List what would be written and write nothing.")
	args = parser.parse_args()
	if not (EXTRACTED / TEXTDATA).is_dir():
		raise SystemExit(f"No v1.150 extraction at {EXTRACTED}; run prepare_client_resources.py first.")

	with (args.donor / "Media.pk2").open("rb") as media_handle, \
		mmap.mmap(media_handle.fileno(), 0, access=mmap.ACCESS_READ) as media, \
		(args.donor / "Data.pk2").open("rb") as data_handle, \
		mmap.mmap(data_handle.fileno(), 0, access=mmap.ACCESS_READ) as data:
		media_entries = sro_pk2.read_directory(media)
		data_entries = sro_pk2.read_directory(data)
		for name in args.sets:
			pattern = set_pattern(name)
			items, names = donor_rows(media, media_entries, pattern)
			if not items:
				raise SystemExit(f"The donor client has no mall avatar rows for {name}.")
			packages = package_rows(media, media_entries, pattern)
			check_free(items, packages)

			# Files of the set: models and textures in Data, inventory icons in Media.
			files = []
			for entries, root, archive in ((data_entries, "Data_extracted", data), (media_entries, "Media_extracted", media)):
				for entry in entries:
					plain = entry.path.rsplit("/", 1)[-1].rsplit(".", 1)[0]
					plain = plain[len("mall_"):] if plain.lower().startswith("mall_avatar_") else plain
					low = entry.path.lower()
					in_scope = low.startswith(("prim/", "res/item/avatar/")) if root == "Data_extracted" \
						else low.startswith("icon/item/avatar/")
					if in_scope and pattern.search(plain):
						files.append((entry, root, sro_pk2.payload(archive, entry)))

			print(f"{name}: {len(items)} items, {len(files)} files, {len(packages['refpackageitem.txt'])} mall packages")
			for cells in items:
				print(f"  {cells[ID]:>6}  {cells[CODE]}")
			if not args.dry_run:
				write_set(OVERLAYS / OVERLAY_NAME / name, items, names, packages, files)

	if not args.dry_run:
		print(f"Overlay written under {OVERLAYS / OVERLAY_NAME}; the retail extraction is untouched.")
		print(f"Next: py -3 scripts/merge_extraction_overlays.py {OVERLAY_NAME}, then")
		print(f"      SRO_EXTRACTION_OVERLAYS={OVERLAY_NAME} pnpm assets build and pnpm task build server-game-data.")
		print("List the mall goods with SRO_MALL_EXTRA_GOODS=on,cap110 (default off).")


if __name__ == "__main__":
	sys.exit(main())
