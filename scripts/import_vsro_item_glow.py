"""
===========================================================================

import_vsro_item_glow.py - pull the later client's item glow tables

PORT-ONLY, NOT NATIVE. The v1.150 client tints a reinforced weapon or
shield through resinfo\\itemtypenumber.txt (tiers +3/5/7/9) and never draws
the system_enchant*.efp sparks its Particles.pk2 ships. The later VSRO
client adds +6/8/10 tiers to the same table and names those sparks per
item in resinfo\\itemoptionefp.txt (effect, bone, scale, from +8). The
client's Experimental "Enhanced item glow" row draws them; off keeps the
v1.150 look.

From the operator's own VSRO client (SRO_VSRO_ROOT or --vsro-root, holding
Media.pk2) this copies the three tables, unchanged, into
extracted/VsroItemGlow_extracted/resinfo/ beside the v1.150 extraction,
with a manifest recording the archive hash. Nothing lands in the
repository; the asset build reads them when they are present.

	py -3 scripts/import_vsro_item_glow.py --vsro-root <folder>

===========================================================================
"""
import argparse
import hashlib
import json
import mmap
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import import_vsro_alexandria as stage1  # noqa: E402
import sro_pk2  # noqa: E402
from prepare_client_resources import safe_target, write_atomically  # noqa: E402
from sro_paths import EXTRACTED_ROOT  # noqa: E402

ROOT_ENV = "SRO_VSRO_ROOT"
MANIFEST_FORMAT = "opensro-vsro-item-glow-v1"
MANIFEST_NAME = ".opensro-vsro-item-glow.json"
OUTPUT_FOLDER = "VsroItemGlow_extracted"
TABLES = ("resinfo/itemtypenumber.txt", "resinfo/itemoption.txt", "resinfo/itemoptionefp.txt")
HASH_CHUNK_BYTES = 1 << 20


"""
================
archive_digest

The archive's sha256, read in chunks so a 700 MB Media.pk2 is never held.
================
"""
def archive_digest(path):
	digest = hashlib.sha256()
	with open(path, "rb") as source:
		for chunk in iter(lambda: source.read(HASH_CHUNK_BYTES), b""):
			digest.update(chunk)
	return digest.hexdigest()


"""
================
import_item_glow

Copy the three tables out of media into output; refuse an archive missing
any of them rather than publish half a catalog.
================
"""
def import_item_glow(media, output):
	found = {}
	with open(media, "rb") as source:
		data = mmap.mmap(source.fileno(), 0, access=mmap.ACCESS_READ)
		try:
			for entry in sro_pk2.read_directory(data):
				key = sro_pk2.fold_ascii(entry.path)
				if key in TABLES:
					found[key] = bytes(sro_pk2.payload(data, entry))
		finally:
			data.close()
	missing = [name for name in TABLES if name not in found]
	if missing:
		raise SystemExit(f"{media} lacks {', '.join(missing)}; is it a VSRO Media.pk2?")
	files = {}
	for name in TABLES:
		write_atomically(safe_target(output, name), found[name])
		files[name] = hashlib.sha256(found[name]).hexdigest()
	manifest = {
		"format": MANIFEST_FORMAT,
		"source": {"archive": "Media.pk2", "sha256": archive_digest(media)},
		"files": files
	}
	write_atomically(output / MANIFEST_NAME, (json.dumps(manifest, indent=2) + "\n").encode())
	return files


"""
================
main
================
"""
def main():
	parser = argparse.ArgumentParser(description=__doc__.split("\n")[3])
	parser.add_argument("--vsro-root", default=os.environ.get(ROOT_ENV), help=f"VSRO client folder (else {ROOT_ENV})")
	args = parser.parse_args()
	if not args.vsro_root:
		raise SystemExit(f"Pass --vsro-root or set {ROOT_ENV} to the VSRO client folder.")
	media = Path(args.vsro_root) / "Media.pk2"
	if not media.is_file():
		raise SystemExit(f"{media} not found; --vsro-root must be a VSRO client folder")
	# The tables below were taken from one VSRO client; the archive itself
	# must be that client before any table is read (#1745).
	stage1.verify_vsro_archive(Path(args.vsro_root), "Media")
	output = EXTRACTED_ROOT / OUTPUT_FOLDER
	files = import_item_glow(media, output)
	print(f"[vsro-item-glow] copied {len(files)} table(s) to {output}")


if __name__ == "__main__":
	main()
