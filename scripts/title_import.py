"""
===========================================================================

title_import.py - the later client's title table, cut for the server (#3502)

Later-native, not port-only: the title system is the later client's own
(build 1696), so it ships as the default with no switch (the owner's
later-native rule). This reads the owner's copy of that client's
Media.pk2 (scripts/sro_pk2.py) and writes titles.txt into a server
textdata root:

	id <TAB> codename <TAB> category <TAB> name-line format

The table is server_dep/silkroad/textdata/RefTitle.txt (id, codename,
category 0-4, SN text code). The format is that code's text in the
TextQuest_Speech&Name chunks: a printf-style string the character's
name prints through, e.g. SN_TITLE_NORMAL_GOLD_USE_01 is
"<Big Spender> %s" in the English column (the tenth field, the layout
the Arabia importer reads). No title is id 0 and prints as plain "%s";
a title whose text no chunk resolves is left out and counted, and
prints plain until its text is authored. In this build only 21 of the
288 titles carry an English format; the rest are untranslated regional
rows (the SN_TITLE_REQ_JAPAN_* family and kin), never localized for
this client, so they stay out rather than print another language's
line.

The variant build runs this cut as a stage (add_titles, called from
build_alexandria_variant.py after the later layers): titles.txt lands
in the variant's server textdata (Media_extracted/server_dep/silkroad/
textdata), and the server bundle's projection copies it with every
other .txt of that root, so a release server always holds the table.
Later-native means no switch: a build that names the later client
(SRO_V1696_ROOT, or the inputs/sro_1696 folder beside the game root)
and cannot produce the file fails, as vsroImportPlan's named-folder
rule does; a tree that names none prints one skip line and builds
without it, and InitTitles' missing-file case covers that local-dev
build only. The command line below stays for a maintainer's manual
cut and freshness check:

	py -3 scripts/title_import.py --media <Media.pk2> --out <textdata>/titles.txt
	py -3 scripts/title_import.py --media <Media.pk2> --out <textdata>/titles.txt --check

===========================================================================
"""
import argparse
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import sro_pk2  # noqa: E402
from pin_file import read_pin_field  # noqa: E402
from prepare_client_resources import file_digest, write_atomically  # noqa: E402
from sro_paths import MAIN_CHECKOUT_ROOT  # noqa: E402

REF_TITLE_PATH = "server_dep/silkroad/textdata/RefTitle.txt"
SPEECH_PREFIX = "server_dep/silkroad/textdata/textquest_speech&name"
FORMAT = "opensro-titles-v1"
SOURCE_ENV = "SRO_V1696_ROOT"
PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
PIN_FORMAT = "opensro-vsro-archive-pins-v1"
# The variant's server textdata (build_alexandria_variant's TEXTDATA,
# import_vsro_alexandria's stage-1 root under Media_extracted).
TEXT_ROOT = Path("Media_extracted") / "server_dep" / "silkroad" / "textdata"
# Speech&Name columns: 1, id, code, Korean, then "0" fillers with the
# English text tenth (the Arabia importer's layout).
SPEECH_CODE_FIELD = 2
SPEECH_ENGLISH_FIELD = 9


# ================
# parse_ref_titles
# ================
def parse_ref_titles(text):
	"""RefTitle rows as (id, codename, category, SN code); strict."""
	rows = []
	for line in text.splitlines():
		if not line.strip():
			continue
		fields = line.split("\t")
		if len(fields) < 4:
			raise ValueError(f"RefTitle row has {len(fields)} fields: {line!r}")
		try:
			title_id = int(fields[0])
			category = int(fields[2])
		except ValueError:
			raise ValueError(f"RefTitle row has a non-numeric id/category: {line!r}") from None
		if title_id <= 0 or not 0 <= category <= 4:
			raise ValueError(f"RefTitle row out of range: {line!r}")
		rows.append((title_id, fields[1], category, fields[3]))
	return rows


# ================
# parse_speech_formats
# ================
def parse_speech_formats(texts):
	"""SN_TITLE_* code -> English name-line format, from Speech&Name chunks.

	A format counts only when the English column carries one with %s in
	it; an untranslated or format-less row resolves nothing (the title
	prints plain). The first chunk to resolve a code wins.
	"""
	formats = {}
	for text in texts:
		for line in text.splitlines():
			fields = line.split("\t")
			if len(fields) <= SPEECH_ENGLISH_FIELD:
				continue
			code = fields[SPEECH_CODE_FIELD]
			if not code.startswith("SN_TITLE_") or code in formats:
				continue
			english = fields[SPEECH_ENGLISH_FIELD]
			if english and english != "0" and "%s" in english:
				formats[code] = english
	return formats


# ================
# build_rows
# ================
def build_rows(ref_text, speech_texts):
	"""(rows, unresolved): titles.txt rows by id, and the SN codes with
	no resolved format (left out of the file, counted by the caller)."""
	formats = parse_speech_formats(speech_texts)
	rows, unresolved = [], []
	for title_id, codename, category, code in parse_ref_titles(ref_text):
		fmt = formats.get(code)
		if fmt is None:
			unresolved.append(code)
			continue
		rows.append((title_id, codename, category, fmt))
	rows.sort(key=lambda row: row[0])
	return rows, unresolved


# ================
# render
# ================
def render(rows):
	lines = [f"# {FORMAT} - generated by scripts/title_import.py from the later client's Media.pk2; do not edit."]
	for title_id, codename, category, fmt in rows:
		lines.append(f"{title_id}\t{codename}\t{category}\t{fmt}")
	return ("\n".join(lines) + "\n").encode("utf-8")


# ================
# generate
# ================
def generate(media_path):
	data = sro_pk2.map_archive(str(media_path))
	entries = {entry.path: entry for entry in sro_pk2.read_directory(data)}
	ref = entries.get(REF_TITLE_PATH)
	if ref is None:
		raise ValueError(f"{REF_TITLE_PATH} not in {media_path}")
	speech = [
		sro_pk2.payload(data, entry).decode("utf-16")
		for path, entry in sorted(entries.items())
		if path.lower().startswith(SPEECH_PREFIX)
	]
	rows, unresolved = build_rows(sro_pk2.payload(data, ref).decode("utf-16"), speech)
	return render(rows), len(rows), unresolved


#============================================================================
# The variant build's stage
#============================================================================

# ================
# named_client_root
#
# The v1.696 client folder a build names: SRO_V1696_ROOT, else the
# inputs/sro_1696 folder beside the game root (skytemple_import's
# source_root rule). None when neither names one; default_root lets
# the tests name a folder of their own.
# ================
def named_client_root(env, default_root=None):
	configured = env.get(SOURCE_ENV, "").strip()
	if configured:
		return Path(configured)
	default = default_root if default_root is not None else MAIN_CHECKOUT_ROOT.parent / "inputs" / "sro_1696"
	return default if default.is_dir() else None


# ================
# title_plan
#
# (run, root) for one build environment. No named client: do not run
# (the caller prints the one skip line). A named client with no
# Media.pk2 fails the build instead of shipping a table-less release
# whose titles all print plain - vsroImportPlan's named-folder rule.
# ================
def title_plan(env=os.environ, default_root=None):
	root = named_client_root(env, default_root)
	if root is None:
		return False, None
	if not (root / "Media.pk2").is_file():
		raise SystemExit(f"Titles: {root} names the later client but holds no Media.pk2; "
			f"restore that folder or unset {SOURCE_ENV} (a release without the title table "
			"prints every title plain)")
	return True, root


# ================
# verify_media
#
# The named client's Media.pk2 against the pin file (the centre
# banner import's rule, #3260): a changed archive fails before any
# of it is read.
# ================
def verify_media(root):
	archives = read_pin_field(PIN_FILE, PIN_FORMAT, "archives")
	if "Media" not in archives:
		raise SystemExit(f"{PIN_FILE} pins no Media.pk2")
	path = Path(root) / "Media.pk2"
	if not path.is_file():
		raise SystemExit(f"{path} not found; {SOURCE_ENV} must name the pinned v1.696 client folder")
	actual = file_digest(path)
	if actual != archives["Media"]:
		raise SystemExit(f"{path} does not match the pinned v1.696 Media.pk2 (sha256 {actual})")
	return path


# ================
# add_titles
#
# The stage build_alexandria_variant.py runs after the later layers:
# titles.txt into the variant's server textdata, where the server
# bundle's projection picks it up with the root's other tables. The
# skip path also reaps a stale titles.txt an earlier named build of a
# reused variant root left behind - the file is this stage's alone.
# Later-native, so there is no off switch and no marker to reap by.
# ================
def add_titles(native, out, env=os.environ, default_root=None):
	target = Path(out) / TEXT_ROOT / "titles.txt"
	run, root = title_plan(env, default_root)
	if not run:
		print("Titles: skipped (no later client named: SRO_V1696_ROOT is not set and inputs/sro_1696 "
			"is absent; titles print plain)", flush=True)
		target.unlink(missing_ok=True)
		return {"enabled": False, "titles": 0}
	media = verify_media(root)
	body, count, unresolved = generate(media)
	target.parent.mkdir(parents=True, exist_ok=True)
	write_atomically(target, body)
	print(f"Titles: wrote {count} titles to {target} ({len(unresolved)} unresolved SN codes left out)",
		flush=True)
	return {"enabled": True, "titles": count, "unresolved": len(unresolved)}


def main():
	parser = argparse.ArgumentParser(description="Cut titles.txt from the later client's Media.pk2 (#3502).")
	parser.add_argument("--media", required=True, help="the later client's Media.pk2")
	parser.add_argument("--out", required=True, help="titles.txt to write")
	parser.add_argument("--check", action="store_true", help="fail when the written file differs")
	args = parser.parse_args()
	body, count, unresolved = generate(Path(args.media))
	out = Path(args.out)
	if args.check:
		if not out.exists() or out.read_bytes() != body:
			print(f"title_import: {out} is stale; regenerate without --check")
			return 1
		print(f"title_import: {out} current ({count} titles)")
		return 0
	out.write_bytes(body)
	print(f"title_import: wrote {count} titles to {out}; {len(unresolved)} unresolved SN codes left out")
	return 0


if __name__ == "__main__":
	raise SystemExit(main())
