"""
===========================================================================

content_archive.py - the pinned content archive (opensro_content.zip) format

Port-only, not native (owner ask #3263, route #3254). One optional
archive carries a later client's new areas from the pinned export into
the build: each packed area's sector terrain, navmesh and map tiles
under areas/<area>/ (entry names keep the export tree's relative
paths), and the shared tables the area importers join under tables/.
A manifest.json at the root names the format and each area's regions.

pack_content.py writes the archive outside the repository (never
committed); the area importers read it through open_archive when
SRO_CONTENT_ZIP names it, and the areas exist only while
SRO_EXPORT_WORLD is on. The whole archive is verified against the
sha256 pinned in content_archive_pins.json before any entry is read,
so a zip that is absent or unpinned imports nothing. The avatar
archive (avatar_archive.py) is the sibling format; the discipline -
sorted entries at a fixed timestamp, a pinned sha256 - is the same.

===========================================================================
"""
import hashlib
import json
from pathlib import Path
import zipfile

PIN_FORMAT = "opensro-content-archive-pin-v1"
ARCHIVE_FORMAT = "opensro-content-archive-v1"
PIN_FILE = "content_archive_pins.json"
MANIFEST_ENTRY = "manifest.json"
TABLES_PREFIX = "tables/"
AREAS_PREFIX = "areas/"
ZIP_TIME = (1980, 1, 1, 0, 0, 0)
DIGEST_CHUNK = 1024 * 1024


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
# The pinned sha256 of the content archive, from its pin file.
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
# file_digest
#
# The sha256 of a file, read in chunks: the archive can hold a whole
# continent's terrain, so it is never loaded whole to be verified.
# ================
def file_digest(path):
	digest = hashlib.sha256()
	with open(path, "rb") as stream:
		for chunk in iter(lambda: stream.read(DIGEST_CHUNK), b""):
			digest.update(chunk)
	return digest.hexdigest()


# ================
# ContentArchive
#
# A verified archive: its manifest, and entries read one at a time.
# ================
class ContentArchive:
	def __init__(self, path, archive, manifest, infos):
		self.path = path
		self.manifest = manifest
		self._archive = archive
		self._infos = infos

	# ================
	# areas
	# ================
	def areas(self):
		"""The packed areas' names, sorted."""
		return sorted(self.manifest["areas"])

	# ================
	# regions
	# ================
	def regions(self, area):
		"""One packed area's sector keys, as (x, y) pairs."""
		return [tuple(region) for region in self.manifest["areas"][area]["regions"]]

	# ================
	# names
	# ================
	def names(self, area=None):
		"""Entry names, or one area's entry names (manifest excluded)."""
		if area is None:
			return sorted(name for name in self._infos if name != MANIFEST_ENTRY)
		prefix = AREAS_PREFIX + area + "/"
		return sorted(name for name in self._infos if name.startswith(prefix))

	# ================
	# read
	# ================
	def read(self, name):
		"""One entry's bytes; an unlisted name is an error, never empty."""
		info = self._infos.get(name)
		if info is None:
			raise SystemExit(f"{self.path}: the archive has no entry {name!r}")
		return self._archive.read(info)

	# ================
	# close
	# ================
	def close(self):
		self._archive.close()


# ================
# open_archive
#
# Verify the archive against the pin, then index it: every entry must
# be the manifest, a shared table, or an entry of an area the manifest
# declares. Nothing is read before the sha256 matches.
# ================
def open_archive(path, pin_file):
	expected = read_pin(pin_file)
	actual = file_digest(path)
	if actual != expected:
		raise SystemExit(
			f"{path} does not match the pinned content archive: sha256 is {actual}, pinned is {expected}. "
			"The archive is packed once by pack_content.py and never committed (#3263)."
		)
	archive = zipfile.ZipFile(Path(path))
	infos = {}
	for info in archive.infolist():
		if info.is_dir():
			continue
		if info.filename in infos:
			archive.close()
			raise SystemExit(f"{path}: duplicate entry {info.filename!r}")
		infos[info.filename] = info
	try:
		manifest = json.loads(archive.read(MANIFEST_ENTRY).decode("utf-8"))
	except (KeyError, ValueError) as error:
		archive.close()
		raise SystemExit(f"{path}: the manifest is unreadable: {error}")
	if manifest.get("format") != ARCHIVE_FORMAT or not isinstance(manifest.get("areas"), dict):
		archive.close()
		raise SystemExit(f"{path}: the manifest is not a {ARCHIVE_FORMAT} manifest")
	declared = set(manifest["areas"])
	seen = set()
	for name in infos:
		if name == MANIFEST_ENTRY or name.startswith(TABLES_PREFIX):
			continue
		area, _, rest = name[len(AREAS_PREFIX):].partition("/") if name.startswith(AREAS_PREFIX) else ("", "", "")
		if not area or not rest:
			archive.close()
			raise SystemExit(f"{path}: entry {name!r} is outside the manifest, tables/ and areas/ layout")
		seen.add(area)
	if seen != declared:
		archive.close()
		raise SystemExit(f"{path}: entries cover areas {sorted(seen)} but the manifest declares {sorted(declared)}")
	return ContentArchive(path, archive, manifest, infos)
