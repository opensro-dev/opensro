"""
===========================================================================

avatar_archive.py - the pinned avatar archive (opensro_avatars.zip) format

Port-only, not native (owner ask #3254). One optional archive carries a
newer client's avatar rows and resources into the build: entries live
under data/, media/ and particles/ prefixes mirroring the pk2 paths, so
the importer reads them exactly as it reads a pk2. pack_avatars.py
writes the archive outside the repository (never committed);
mall_avatar_import.py reads it when SRO_AVATARS_ZIP names it. The whole
archive is verified against the sha256 pinned in
avatars_archive_pins.json before any entry is read.

===========================================================================
"""
import json
from pathlib import Path
import zipfile

import cap110_item_import as shared
import sro_pk2

PIN_FORMAT = "opensro-avatars-archive-pin-v1"
FOLDERS = shared.FOLDERS
KINDS = {"data": "Data", "media": "Media", "particles": "Particles"}
ZIP_TIME = (1980, 1, 1, 0, 0, 0)


# ================
# write_archive
#
# Sorted entries at a fixed timestamp, so packing the same export twice
# yields the same sha256.
# ================
def write_archive(path, entries):
	with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
		for name in sorted(entries):
			info = zipfile.ZipInfo(name, date_time=ZIP_TIME)
			info.compress_type = zipfile.ZIP_DEFLATED
			info.create_system = 0
			archive.writestr(info, entries[name])


# ================
# read_pin
#
# The pinned sha256 of the avatar archive, from its pin file.
# ================
def read_pin(pin_file):
	try:
		pin = json.loads(Path(pin_file).read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{pin_file} is unreadable: {error}")
	if pin.get("format") != PIN_FORMAT or not isinstance(pin.get("sha256"), str):
		raise SystemExit(f"{pin_file} is not a {PIN_FORMAT} pin file")
	return pin["sha256"]


# ================
# open_archive
#
# The archive as (bytes, index) archives plus its verified sha256. The
# archive holds only avatar resources, so it is read whole.
# ================
def open_archive(path, pin_file):
	expected = read_pin(pin_file)
	raw = Path(path).read_bytes()
	actual = sro_pk2.digest(raw)
	if actual != expected:
		raise SystemExit(
			f"{path} does not match the pinned avatar archive: sha256 is {actual}, pinned is {expected}. "
			"The archive is packed once by pack_avatars.py and never committed (#3254)."
		)
	blobs = {name: bytearray() for name in FOLDERS}
	indexes = {name: {} for name in FOLDERS}
	with zipfile.ZipFile(Path(path)) as archive:
		for info in archive.infolist():
			if info.is_dir():
				continue
			kind, _, rest = info.filename.partition("/")
			if kind not in KINDS or not rest:
				raise SystemExit(f"{path}: entry {info.filename!r} is outside the data/, media/ and particles/ prefixes")
			name = KINDS[kind]
			data = archive.read(info)
			key = shared.normalized_path(rest)
			if key in indexes[name]:
				raise SystemExit(f"{path}: duplicate {name} entry {rest!r}")
			indexes[name][key] = sro_pk2.Entry(rest, len(blobs[name]), len(data))
			blobs[name].extend(data)
	archives = {name: (bytes(blobs[name]), indexes[name]) for name in FOLDERS}
	return archives, {"archive": actual}
