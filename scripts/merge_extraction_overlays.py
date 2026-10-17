"""
===========================================================================

merge_extraction_overlays.py - build the merged extraction an overlay needs

The v1.150 extraction (<game root>/extracted) is the port's retail source of
truth and is never edited. Content from another client (scripts/
import_cap110_avatars.py) lives beside it as an overlay:

	extracted/overlays/<name>/<set>/<same relative layout as extracted/>

This tool builds <game root>/extracted-merged from the retail tree plus the
overlays you name, and records what it did in overlay-manifest.json. The
resolvers (scripts/build/world/paths.mjs, scripts/sro_paths.py) read the
merged tree, in place of extracted/, only when SRO_EXTRACTION_OVERLAYS names
overlays; unset, the build is retail exactly.

The merge rules are explicit:

  * Every retail file is hard-linked (a copy where the volume cannot link),
    so the merged tree costs no space and the retail bytes are the same files.
  * A row table (ROW_TABLES) takes the overlay's rows appended after retail's;
    the merged file is a new file, never a write through a retail link. A row
    whose key retail or another overlay already has stops the merge.
  * Any other overlay file is added; it must not already exist unless its
    bytes are identical.

	py -3 scripts/merge_extraction_overlays.py cap110

The game root is SRO_GAME_ROOT or the parent of the checkout (sro_paths.py).

===========================================================================
"""
import argparse
import hashlib
import json
import os
import shutil
import sys
from pathlib import Path

# This tool builds the merged tree, so it must start before one exists: the
# path resolvers (sro_paths.py) would refuse a missing merged tree.
os.environ.pop("SRO_EXTRACTION_OVERLAYS", None)

from prepare_client_resources import safe_target  # noqa: E402
from sro_paths import GAME_ROOT  # noqa: E402

EXTRACTED = GAME_ROOT / "extracted"
MERGED = GAME_ROOT / "extracted-merged"
OVERLAYS = EXTRACTED / "overlays"
MANIFEST = "overlay-manifest.json"
TEXTDATA = Path("Media_extracted/server_dep/silkroad/textdata")

# Row tables an overlay appends to: file -> the cells that identify a row. The
# index lists whole lines. Price rows are one per currency.
ROW_TABLES = {
	"textdataname.txt": lambda cells: (cells[1],),
	"itemdata.txt": lambda cells: (cells[0],),
	"refpackageitem.txt": lambda cells: (cells[3],),
	"refpricepolicyofitem.txt": lambda cells: (cells[2], cells[3]),
	"refscrapofpackageitem.txt": lambda cells: (cells[2],),
}


#============================================================================
# read_text / records / encode_text
#============================================================================
def read_text(data):
	if data[:2] == b"\xff\xfe":
		return data.decode("utf-16")
	return data.decode("cp949")


def records(text):
	"""Rows end in CRLF only; a bare LF belongs to a cell (splitlines would cut it)."""
	rows = text.split("\r\n")
	return rows[:-1] if rows and rows[-1] == "" else rows


def encode_text(lines):
	return b"\xff\xfe" + "".join(line + "\r\n" for line in lines).encode("utf-16-le")


def read_rows(path):
	"""The rows of one UTF-16 table; the tables an overlay appends to all are."""
	data = path.read_bytes()
	if data[:2] != b"\xff\xfe":
		raise SystemExit(f"{path} is not a UTF-16 table; cannot merge rows into it.")
	return records(read_text(data))


def row_key(name, row):
	return ROW_TABLES[name](row.split("\t"))


#============================================================================
# overlay_files
#============================================================================
def overlay_files(overlay_dirs):
	"""Every file of the overlays as (relative path, overlay dir), in a stable order."""
	files = []
	for directory in overlay_dirs:
		for path in sorted(p for p in directory.rglob("*") if p.is_file() and p.name != "overlay.json"):
			files.append((path.relative_to(directory), directory))
	return files


#============================================================================
# link_retail
#============================================================================
def link_retail(target):
	"""Hard-link (or copy) every retail file into the merged tree."""
	copied = 0
	for source in EXTRACTED.rglob("*"):
		relative = source.relative_to(EXTRACTED)
		if relative.parts[0] == "overlays":
			continue
		destination = target / relative
		if source.is_dir():
			destination.mkdir(parents=True, exist_ok=True)
			continue
		destination.parent.mkdir(parents=True, exist_ok=True)
		try:
			os.link(source, destination)
		except OSError:
			shutil.copy2(source, destination)
			copied += 1
	return copied


#============================================================================
# merge
#============================================================================
def merge(names):
	"""Build the merged tree and return its manifest."""
	overlay_dirs = []
	for name in names:
		sets = sorted(p for p in (OVERLAYS / name).glob("*") if p.is_dir())
		if not sets:
			raise SystemExit(f"No overlay sets under {OVERLAYS / name}; run the importer first.")
		overlay_dirs.extend(sets)

	partial = MERGED.with_name(MERGED.name + ".partial")
	for stale in (partial, MERGED):
		if stale.exists():
			shutil.rmtree(stale)
	partial.mkdir(parents=True)
	copied = link_retail(partial)

	appended, added = {}, []
	known = {}
	for relative, directory in overlay_files(overlay_dirs):
		name = relative.name
		target = safe_target(partial, relative.as_posix())
		if name in ROW_TABLES and relative.parent == TEXTDATA:
			if not target.exists():
				raise SystemExit(f"{relative}: retail has no such table to append to.")
			rows = known.get(relative)
			if rows is None:
				rows = read_rows(target)
				known[relative] = rows
			keys = {row_key(name, row) for row in rows if row}
			new = []
			for row in read_rows(directory / relative):
				key = row_key(name, row)
				if key in keys:
					raise SystemExit(f"{relative}: {directory.name} repeats row {key}, already in retail or another overlay.")
				keys.add(key)
				new.append(row)
			rows.extend(new)
			appended.setdefault(relative.as_posix(), []).append({"set": directory.name, "rows": len(new)})
			continue
		payload = (directory / relative).read_bytes()
		if target.exists():
			if target.read_bytes() != payload:
				raise SystemExit(f"{relative}: {directory.name} would replace a different file.")
			continue
		target.parent.mkdir(parents=True, exist_ok=True)
		target.write_bytes(payload)
		added.append({"path": relative.as_posix(), "sha256": hashlib.sha256(payload).hexdigest(), "set": directory.name})

	# A table is a new file: unlink the retail link first, never write through it.
	for relative, rows in known.items():
		target = partial / relative
		target.unlink()
		target.write_bytes(encode_text(rows))

	manifest = {
		"overlays": names,
		"sets": [directory.name for directory in overlay_dirs],
		"retailPreparation": hashlib.sha256((EXTRACTED / ".opensro-preparation.json").read_bytes()).hexdigest()
			if (EXTRACTED / ".opensro-preparation.json").exists() else None,
		"addedFiles": added,
		"appendedTables": appended,
		"copiedRetailFiles": copied,
	}
	(partial / MANIFEST).write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
	os.replace(partial, MERGED)
	return manifest


#============================================================================
# main
#============================================================================
def main():
	parser = argparse.ArgumentParser(description="Build the merged extraction an overlay needs.")
	parser.add_argument("names", nargs="+", help="Overlay names, e.g. cap110.")
	args = parser.parse_args()
	manifest = merge(args.names)
	rows = sum(entry["rows"] for entries in manifest["appendedTables"].values() for entry in entries)
	print(f"Merged {', '.join(manifest['sets'])}: {len(manifest['addedFiles'])} files added, "
		f"{rows} table rows appended, retail untouched.")
	print(f"Build with: SRO_EXTRACTION_OVERLAYS={','.join(args.names)} pnpm assets build")


if __name__ == "__main__":
	sys.exit(main())
