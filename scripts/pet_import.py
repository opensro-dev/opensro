"""
===========================================================================

pet_import.py - stage the later clients' pets (pickup, attack, transport and vehicle kinds)

Port-only, not native, where the source is not a later native client. The
v1.150 client already carries every pet the VSRO client has (the cap110
character import brought their COS rows with the monsters), so a pass over
an old source reports every kind as existing and adds nothing. What this
importer adds is the road for the kinds only a later source holds: the
v1.696 client's pets (a later native client, so no flag of its own) and
the v2.355 export's custom pets, read from the one pinned, optional
opensro archive the avatar pass reads (SRO_AVATARS_ZIP, opened through
avatar_archive so its sha256 pin is checked before a byte is read;
unset: no archive, the pass skips).

Selection is by codename family (the codename minus its trailing level
number): COS_P_ kinds with leveled rows are attack (growth) pets, COS_P_
kinds with one level are pickup pets, COS_T_ are transports, COS_C_ are
vehicles and trade animals. COS_GUARD_ and COS_GUILD_ rows are mercenary
and guild systems, not pets, and never candidates. Fellow kinds
(COS_F_) ride the attack-pet lane (#3328, inference recorded there):
the persistent summoner admits them by their rows' own band and they
fight with their native per-level rows, like every summonable kind
without a petdesign entry. A kind of any other prefix is deferred
and reported, never half imported: its rows wait for a lane.

A chosen kind brings its characterdata rows, the summon scrolls whose
itemdata column 119 names it, the name text for both, and the skilldata
rows its DefaultSkillIDs name (so an attack or fellow pet can fight the
moment it is summoned). The pet's model (.bsr, characterdata column 52)
must exist in the later archives or the native tree, or the kind is
skipped and reported; the mesh, material and animation closure below
the .bsr is the character model builder's, as it is for every imported
monster. What an import does not bring is the expansion's trained-skill
tree: companion/petdesign.json is the owner's single source for those
rules, so the report names, per added attack kind, the design entry and
pet skill rows a follow-up still owes. No retail table is committed to
Git; rows land in the variant staging tree beside the avatar shards.

Rows keep their later-client identity when it is free in the native
tables and the character data; the few that clash take a fresh one above
every known identity, remembered in the marker so a rebuild never moves
a pet. Each source owns one character shard, one item shard and one
marker, so a source's off path removes only its own rows.

Two passes close what no source fills (owner 2026-11-28: every pet is in
the game). The VSRO pass also lists the native kinds whose scroll no
package sells (native_scrolls), on their lane's tab. A fourth source,
INFERRED_SOURCE, runs pet_inference.py's rows for the COS models every
client ships without a row (gorilla, frilled lizard, the fourth dragon
horse tier) through the same selection, after every real source.

===========================================================================
"""
import argparse
import collections
import json
import mmap
import os
from pathlib import Path
import re

import alexandria_npcs as characters
import avatar_archive
import cap110_monsters
import cap110_item_import as shared
import cap110_items as items
import import_vsro_alexandria as tables
import published_ids
import later_world_import
import mall_avatar_import as mall_authoring
import mall_tables as mall
import pet_inference
import skilldata_enc as skill_tables
from pin_file import read_pin_field
from prepare_client_resources import safe_target, write_atomically
import sro_pk2
import later_tables
from sro_paths import EXTRACTED_ROOT, REPO_ROOT

FLAG = "SRO_PETS"
FLAG_OFF = ("0", "off", "false", "no")
# The one pinned archive the avatar pass reads (#3254): the v2.355
# export's pets ride the same zip and the same sha256 pin, so the build
# has one optional supply chain, not two.
AVATARS_ENV = "SRO_AVATARS_ZIP"
AVATARS_PIN_FILE = Path(__file__).with_name("avatars_archive_pins.json")
V1696_ENV = "SRO_V1696_ROOT"
V1696_PIN_FILE = Path(__file__).with_name("v1696_archive_pins.json")
PIN_FORMAT = "opensro-vsro-archive-pins-v1"
FOLDERS = shared.FOLDERS
TEXT_ROOT = shared.TEXT_ROOT
# Where a row's icon column points (Media icon/, under the staging tree).
ICON_ROOT = Path("Media_extracted") / "icon"

# Character row columns (the layout scripts/cap110_monsters.py reads):
CHAR_ID = 1
CHAR_CODE = 2
CHAR_NAME_KEY = 5
CHAR_MODEL = 52
CHAR_LEVEL = 57
# Item row column 119 is the associated character codename (the server's
# itemrefs.go reads the same column): a summon scroll names its pet there.
ITEM_ASSOCIATED_CHAR = 119
SERVICE_ON = "1"

# One shard triple and one marker per source, in import order: a kind is
# imported once, from the earliest source that holds it. Skill shards sit
# on the same band as the character/item shards (nasrun already owns
# skilldata_130000, so the VSRO pet pass writes skilldata_131000).
VSRO_SOURCE = {"name": "vsro", "char_shard": "characterdata_130000.txt", "item_shard": "itemdata_130000.txt",
	"skill_shard": "skilldata_131000.txt", "report": ".opensro-pets.json", "pins": None}
V1696_SOURCE = {"name": "v1696", "char_shard": "characterdata_140000.txt", "item_shard": "itemdata_140000.txt",
	"skill_shard": "skilldata_140000.txt", "report": ".opensro-pets-v1696.json", "pins": V1696_PIN_FILE}
ARCHIVE_SOURCE = {"name": "archive", "char_shard": "characterdata_150000.txt", "item_shard": "itemdata_150000.txt",
	"skill_shard": "skilldata_150000.txt", "report": ".opensro-pets-archive.json", "pins": AVATARS_PIN_FILE}
# The kinds no source has a row for (pet_inference.py), inferred from the
# VSRO client's template rows and run as the last source, so a kind any
# real source carries always wins.
INFERRED_SOURCE = {"name": "inferred", "char_shard": "characterdata_340000.txt", "item_shard": "itemdata_340000.txt",
	"skill_shard": "skilldata_340000.txt", "report": ".opensro-pets-inferred.json", "pins": None, "infer": True}
SOURCES = (VSRO_SOURCE, V1696_SOURCE, ARCHIVE_SOURCE, INFERRED_SOURCE)

EXCLUDED_PREFIXES = ("COS_GUARD_", "COS_GUILD_")
# A developer's test row (v1.696 COS_T_BEHEMOTH_TEST: no scroll, a copy
# of the Behemoth) is no kind at all (mall_avatar_import's _TEST rule).
EXCLUDED_SUFFIXES = ("_TEST",)
# The Item Mall tab each lane's summon scrolls list on: v1.150's own pet
# tabs (refshoptab). Fellows fight like attack pets and share their tab.
# Owner 2026-11-27: every new pet sells, native or not.
MALL_TAB = {
	"pickup": "MALL_PET_B_SILK", "attack": "MALL_PET_A_GROWTH", "fellow": "MALL_PET_A_GROWTH",
	"transport": "MALL_PET_D_CART", "vehicle": "MALL_PET_C_VEHICLE",
}
# Event copies are rewards, not goods: their scrolls never list.
EVENT_SCROLL_PREFIX = "ITEM_EVENT_"
LEVEL_SUFFIX = re.compile(r"_\d+$")
# Kinds the server's COS lanes already run (the summoning, pickup, growth,
# transport, riding and fellow lanes); anything else defers until its
# lane exists. Fellows need no petdesign entry: they run their native
# rows, and the report's design-entry need stays attack-only.
LANE_KINDS = ("pickup", "attack", "transport", "vehicle", "fellow")
# RefObjChar default-skill columns in the v1.150 layout (convert_character
# lands the later client's ten actions here; the server reads the same).
CHAR_DEFAULT_SKILL1 = 89
CHAR_DEFAULT_SKILL_N = 98


# ================
# enabled
#
# The import as a whole defaults on like the mall avatars: an unset or
# empty value is on, only an explicit off value turns it off. An
# unreadable value is refused rather than guessed. The archive source
# has its own gate (AVATARS_ENV names the pinned archive; unset imports
# nothing). Gate names, confirmed against #3262 for #3328: there is no
# SRO_PETS_ARCHIVE; the archive source rides the shared pinned zip
# (SRO_AVATARS_ZIP) under this same SRO_PETS switch.
# ================
def enabled(env=os.environ):
	value = env.get(FLAG, "").strip().lower()
	if value in ("", "1", "on", "true", "yes"):
		return True
	if value in FLAG_OFF:
		return False
	raise ValueError(f"{FLAG} must be on or off, not {value!r}")


# ================
# family_of / kind_of
#
# A kind is its codename family: COS_P_WOLF_001 and COS_P_WOLF_140 are
# one wolf. The kind follows the prefix and the family's shape.
# ================
def family_of(codename):
	return LEVEL_SUFFIX.sub("", codename)


def kind_of(family, rows):
	if family.startswith(EXCLUDED_PREFIXES) or family.endswith(EXCLUDED_SUFFIXES):
		return "excluded"
	if family.startswith("COS_P_"):
		levels = {row[CHAR_LEVEL] for row in rows if len(row) > CHAR_LEVEL}
		return "attack" if len(levels) > 1 else "pickup"
	if family.startswith("COS_T_"):
		return "transport"
	if family.startswith("COS_C_"):
		return "vehicle"
	if family.startswith("COS_F_"):
		return "fellow"
	# Anything else waits for its lane (the banner's rule): a prefix
	# this map does not name is deferred, never fellow by default.
	return "unknown"


# ================
# previous_targets
#
# A marker's identity map, so a rebuilt import keeps the identities it
# already handed out (the avatar marker's rule).
# ================
def previous_targets(prior):
	result = {}
	for code, entry in prior.get("idMap", {}).items():
		result[code] = int(entry["target"]) if isinstance(entry, dict) else int(entry)
	return result


# ================
# select_pets
#
# later_chars / later_items are full row lists from one later source;
# earlier_codes are the row codenames an earlier source imported.
# missing(family, rows) names absent resources. Scrolls are the later
# item rows whose associated-character column names the family (or one
# of its rows); a kind without one cannot be obtained and is skipped.
# Returns (char_rows, item_rows, report) with identities applied and the
# character rows in the v1.150 layout (alexandria_npcs.convert_character,
# the one owner of the 104 -> 120 column map). The item rows need no map:
# itemdata is 160 columns in every client.
# ================
def select_pets(native_chars, native_items, earlier_codes, later_chars, later_items, previous=None, missing=lambda family, rows: [], reserved_ids=(),
		pinned=None):
	native_codes = {row[CHAR_CODE] for row in native_chars if len(row) > CHAR_CODE}
	taken_ids = {int(row[CHAR_ID]) for row in native_chars if len(row) > CHAR_ID and row[CHAR_ID].isdigit()}
	taken_ids |= {int(row[items.IDENTITY]) for row in native_items if len(row) > items.IDENTITY and row[items.IDENTITY].isdigit()}
	# Identities other importers already placed in the staging tree are
	# taken too: character and item rows share one built identity space,
	# so a source id that is free in the native tables may still be used.
	taken_ids |= {int(value) for value in reserved_ids}
	previous = previous or {}
	families = collections.defaultdict(list)
	for row in later_chars:
		if len(row) > CHAR_CODE and row[CHAR_CODE].startswith("COS_"):
			families[family_of(row[CHAR_CODE])].append(row)
	scrolls = collections.defaultdict(list)
	for row in later_items:
		if len(row) > ITEM_ASSOCIATED_CHAR and row[items.SERVICE] == SERVICE_ON and row[ITEM_ASSOCIATED_CHAR].strip():
			scrolls[row[ITEM_ASSOCIATED_CHAR].strip()].append(row)
	chosen, existing, deferred, skipped = [], [], [], []
	for family in sorted(families):
		rows = sorted(families[family], key=lambda row: int(row[CHAR_ID]) if row[CHAR_ID].isdigit() else 0)
		kind = kind_of(family, rows)
		if kind == "excluded":
			continue
		fresh_rows = [row for row in rows if row[CHAR_CODE] not in native_codes and row[CHAR_CODE] not in earlier_codes]
		if not fresh_rows:
			existing.append(family)
			continue
		if kind not in LANE_KINDS:
			deferred.append({"family": family, "kind": kind, "reason": "no server lane yet"})
			continue
		codes = {row[CHAR_CODE] for row in rows}
		linked = [row for code, linked_rows in scrolls.items() if code == family or code in codes for row in linked_rows]
		absent = list(missing(family, fresh_rows))
		if not linked:
			absent.append("summon scroll")
		if absent:
			skipped.append({"family": family, "kind": kind, "missing": absent})
			continue
		chosen.append((family, kind, fresh_rows, linked))
	# Identities: a released one first (pinned, published_ids.overlay:
	# every other released object id stays taken), else a retained one,
	# else the later one when free, else a fresh one above every known
	# identity. Character and item rows share one identity space in the
	# built data.
	if pinned:
		staged = [row[CHAR_CODE] for _, _, rows, _ in chosen for row in rows]
		staged += [row[items.CODENAME] for _, _, _, linked in chosen for row in linked]
		held, previous = published_ids.overlay(pinned, staged, taken_ids, previous)
		taken_ids |= held
	targets = {}

	def claim(code, source_id):
		retained = previous.get(code)
		if retained is not None and retained not in taken_ids:
			targets[code] = retained
		elif source_id not in taken_ids:
			targets[code] = source_id
		else:
			fresh = max(taken_ids) + 1 if taken_ids else 1
			while fresh in taken_ids:
				fresh += 1
			targets[code] = fresh
		taken_ids.add(targets[code])

	char_rows, id_map, report_kinds = [], {}, []
	for family, kind, rows, linked in chosen:
		levels = sorted({int(row[CHAR_LEVEL]) for row in rows if len(row) > CHAR_LEVEL and row[CHAR_LEVEL].isdigit()})
		for row in rows:
			claim(row[CHAR_CODE], int(row[CHAR_ID]))
			# The later clients' characterdata is 104 columns (VSRO and v1.696
			# alike) and v1.150's is 120; the server and client read the v1.150
			# layout, so a row joins in it. Copied as-is, a pet row had no column
			# 109 and the NPC manifest wrote its material kind as null, which the
			# client refuses ("Invalid monster material kind").
			converted = characters.convert_character(row)
			id_map[row[CHAR_CODE]] = {"source": int(row[CHAR_ID]), "target": targets[row[CHAR_CODE]]}
			converted[CHAR_ID] = str(targets[row[CHAR_CODE]])
			char_rows.append(converted)
		needs = ["petdesign entry"]
		if kind == "attack":
			needs.append("pet skill rows")
		if kind == "fellow":
			needs = []  # native rows only; the expansion design is attack-pet only
		report_kinds.append({
			"family": family, "kind": kind, "rows": len(rows),
			"levels": [levels[0], levels[-1]] if len(levels) > 1 else levels,
			"scrolls": sorted({row[items.CODENAME] for row in linked}), "needs": needs})
	item_rows = []
	for family, kind, rows, linked in chosen:
		for row in linked:
			claim(row[items.CODENAME], int(row[items.IDENTITY]))
			converted = list(row)
			id_map[row[items.CODENAME]] = {"source": int(row[items.IDENTITY]), "target": targets[row[items.CODENAME]]}
			converted[items.IDENTITY] = str(targets[row[items.CODENAME]])
			item_rows.append(converted)
	report = {"added": report_kinds, "existing": existing, "deferred": deferred, "skipped": skipped, "idMap": id_map}
	return char_rows, item_rows, report


# ================
# later table readers
#
# Every source's tables come off a Media (bytes, index) pair: a pk2
# source mmaps its client archive, the pinned archive's Media comes
# from avatar_archive. Both return (character rows, item rows, object
# text table) with shards resolved by their owner,
# later_tables.Archive.
# ================
def later_tables_from_media(media, index, object_text_required=True):
	read = later_tables.Archive.media(media, index)

	# A client Media always carries the object text, so a pk2 read stops
	# without it; the pinned archive may carry pet rows without it, and a
	# kind whose name never arrived is reported, never fatal. v1.696's
	# textdata_object.txt is a shard index, not the table: read through
	# later_object_text, which follows it (and reads a flat VSRO table as
	# itself). Read raw, every v1.696 pet's name went missing and the
	# asset build refused the release (21 item titles, 2026-11-15).
	if object_text_required or read.has("textdata_object.txt"):
		object_text = b"\xff\xfe" + later_world_import.later_object_text(read).encode("utf-16-le")
	else:
		object_text = b""
	return (read.rows("characterdata.txt"),
		read.rows("itemdata.txt"),
		object_text)


def later_tables_from_pk2(root):
	with open(Path(root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		return later_tables_from_media(media, tables.archive_index(media))


def native_tables(native, exclude_shards=()):
	# exclude_shards drops this source's own overlay when native and
	# output share a tree, so a rebuild still sees its kinds as fresh
	# and can restage their skills (later_world_import's tree_characters
	# rule).
	excluded = {name.lower() for name in exclude_shards}
	text_root = Path(native) / TEXT_ROOT
	chars = [row for path in sorted(text_root.glob("characterdata_*.txt"))
		if path.name.lower() not in excluded
		for row in tables.table_rows(path.read_bytes())]
	item_rows = [row for path in sorted(text_root.glob("itemdata_*.txt"))
		if path.name.lower() not in excluded
		for row in tables.table_rows(path.read_bytes())]
	return chars, item_rows


# ================
# stage_pet_skills
#
# The DefaultSkillIDs on the staged character rows (native columns
# 89..98): pull those skilldata rows from the later client, merge them
# onto skill_shard the way later_world_import.stage_kinds does, and
# remap the character columns through the identity map. An empty need
# drops the shard so a rebuild that stages no fighters leaves no
# orphan skill file. Returns (char_rows, skill code->id map).
# ================
def stage_pet_skills(read, text_root, skill_shard, char_rows, previous_skills=None):
	text_root = Path(text_root)
	needed = {
		int(value) for row in char_rows for value in row[CHAR_DEFAULT_SKILL1:CHAR_DEFAULT_SKILL_N + 1]
		if len(row) > CHAR_DEFAULT_SKILL_N and value.isdigit() and int(value)
	}
	if not needed:
		_drop_skill_shard(text_root, skill_shard)
		return char_rows, {}
	native_skills = [
		row for path in text_root.glob("skilldata_*.txt") if path.name.lower() != skill_shard.lower()
		for row in tables.table_rows(path.read_bytes()) if len(row) == 118
	]
	client_skills = [row for row in skill_tables.client_skill_rows(read) if len(row) > 3 and row[1].isdigit()]
	added, skill_map = cap110_monsters.merge_skills(native_skills, client_skills, needed, previous_skills or {})
	for row in char_rows:
		row[CHAR_DEFAULT_SKILL1:CHAR_DEFAULT_SKILL_N + 1] = [
			str(skill_map[int(value)]) if value.isdigit() and int(value) else "0"
			for value in row[CHAR_DEFAULT_SKILL1:CHAR_DEFAULT_SKILL_N + 1]
		]
	write_atomically(text_root / skill_shard, cap110_monsters.encode_rows(added))
	manifest = tables.table_rows((text_root / "skilldata.txt").read_bytes()) if (text_root / "skilldata.txt").exists() else []
	manifest = [row for row in manifest if row and row[0].lower() != skill_shard.lower()] + [[skill_shard]]
	write_atomically(text_root / "skilldata.txt", cap110_monsters.encode_rows(manifest))
	return char_rows, {row[3]: int(row[1]) for row in added}


def _drop_skill_shard(text_root, skill_shard):
	text_root = Path(text_root)
	manifest = text_root / "skilldata.txt"
	if manifest.exists():
		kept = [row for row in tables.table_rows(manifest.read_bytes()) if not row or row[0].lower() != skill_shard.lower()]
		write_atomically(manifest, cap110_monsters.encode_rows(kept))
	(text_root / skill_shard).unlink(missing_ok=True)


def read_marker(output, source):
	path = Path(output) / source["report"]
	if not path.exists():
		return {}
	try:
		return json.loads(path.read_text(encoding="utf-8"))
	except (OSError, ValueError):
		return {}


def _encode(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


# ================
# remove_import
#
# A source's off path: its shards leave the manifests and the tree, and
# its marker goes, leaving every other source's rows untouched.
# ================
def _drop_manifest_line(output, manifest_name, shard):
	manifest = Path(output) / TEXT_ROOT / manifest_name
	if manifest.exists():
		kept = [row for row in tables.table_rows(manifest.read_bytes()) if not row or row[0].lower() != shard]
		write_atomically(manifest, _encode(kept))


def remove_import(output, source):
	text_root = Path(output) / TEXT_ROOT
	mall.strip_mall(Path(output), set(read_marker(output, source).get("mall", {}).get("packages", [])))
	for manifest_name, shard in (("characterdata.txt", source["char_shard"]), ("itemdata.txt", source["item_shard"])):
		_drop_manifest_line(output, manifest_name, shard)
		(text_root / shard).unlink(missing_ok=True)
	_drop_skill_shard(text_root, source["skill_shard"])
	_remove_icons(output, read_marker(output, source))
	(Path(output) / source["report"]).unlink(missing_ok=True)


# ================
# _remove_icons
#
# The icons a previous pass copied (its marker's "icons"), gone with it.
# Only files this importer wrote are named there; native icons never are.
# ================
def _remove_icons(output, marker):
	for name in marker.get("icons", []):
		safe_target(Path(output) / ICON_ROOT, name).unlink(missing_ok=True)


# ================
# _copy_icons
#
# The icons of the added kinds and their scrolls (column 54, relative to
# Media icon/): a pet's portrait and its scroll's inventory picture. The
# later archive is the only source of a kind v1.150 lacks, and the row
# import alone left the item with no picture at all (the snowman scroll's
# "no image"). An icon the native tree has is never copied; one neither
# holds is reported. A rebuild first removes what the last pass copied.
# ================
def _copy_icons(root, native, output, rows, report):
	with open(Path(root) / "Media.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as media:
		index = tables.archive_index(media)

		def later_icon(name):
			entry = index.get("icon/" + name)
			return None if entry is None else sro_pk2.payload(media, entry)

		copy_icon_files(native, output, rows, report, later_icon)


# ================
# copy_icon_files
#
# _copy_icons' rule over any later source: later_icon(name) answers the
# bytes of Media icon/<name>, or None. report's "_prior" is the previous
# marker, whose copies go first; report gains "icons" (what was copied)
# and, when some icon exists nowhere, "missingIcons".
# ================
def copy_icon_files(native, output, rows, report, later_icon):
	_remove_icons(output, report.pop("_prior", {}))
	icon_root = Path(native) / ICON_ROOT
	native_icons = {
		path.relative_to(icon_root).as_posix().lower() for path in icon_root.rglob("*") if path.is_file()
	} if icon_root.exists() else set()
	wanted = sorted({
		shared.normalized_path(row[items.ICON]) for row in rows
		if len(row) > items.ICON and row[items.ICON] not in ("", items.UNUSED)
	})
	copied, absent = [], []
	for name in wanted:
		if name in native_icons:
			continue
		raw = later_icon(name)
		if raw is None:
			absent.append(name)
			continue
		write_atomically(safe_target(Path(output) / ICON_ROOT, name), raw)
		copied.append(name)
	report["icons"] = copied
	if absent:
		report["missingIcons"] = absent


def _manifest_with(output, manifest_name, shard):
	manifest = safe_target(Path(output) / TEXT_ROOT, manifest_name)
	rows = tables.table_rows(manifest.read_bytes()) if manifest.exists() else []
	if not any(row and row[0].lower() == shard for row in rows):
		rows.append([shard])
	write_atomically(manifest, _encode(rows))


# ================
# _merge_name_text
#
# Name text for the imported kinds: rows the later object table carries
# under the kinds' name keys, appended to the staging name table where
# the native one lacks them. Missing keys are reported, not fatal (a
# kind whose name never arrived is still listed for the follow-up).
# ================
def _merge_name_text(output, char_rows, item_rows, later_text, report):
	name_path = safe_target(Path(output) / TEXT_ROOT, "textdataname.txt")
	base = name_path.read_bytes() if name_path.exists() else b""
	known = {row[1] for row in characters.translation_rows(base) if len(row) > 1}
	wanted = ({row[CHAR_NAME_KEY] for row in char_rows if len(row) > CHAR_NAME_KEY}
		| {row[items.NAME_KEY] for row in item_rows if len(row) > items.NAME_KEY}) - known
	# later_text_row brings a v1.696 17-column row (text id after the
	# service cell) to the VSRO layout this merge reads, as stage_kinds
	# does; a VSRO row passes through unchanged.
	texts = [[row[0], row[1], row[2], "", "", row[5], row[4], row[7], row[8], row[9], ""]
		for row in (later_world_import.later_text_row(row) for row in characters.translation_rows(later_text))
		if len(row) >= 10 and row[1] in wanted]
	if texts:
		write_atomically(name_path, characters.append_rows(base, texts))
	report["missingNameText"] = sorted(wanted - {row[1] for row in texts})


# ================
# tab_price
#
# The silk price most of a tab's goods carry in the built tree: an
# imported pet takes the price its own tab already asks (inferred: no
# mall lists these pets natively, so the tab's common price is the one
# a player of that tab expects). None when the tab sells nothing.
# ================
def tab_price(current, tab):
	packages = {row[3] for row in current["refshopgoods.txt"] if len(row) > 3 and row[2] == tab}
	prices = collections.Counter(
		row[mall.MALL_PRICE_AMOUNT - 1] for row in current["refpricepolicyofitem.txt"]
		if len(row) > mall.MALL_PRICE_AMOUNT - 1 and row[2] in packages and row[3] == mall.SILK_CURRENCY
	)
	return prices.most_common(1)[0][0] if prices else None


# ================
# _list_in_mall
#
# The imported summon scrolls in the Item Mall: one authored package per
# scroll on its lane's pet tab, at the tab's price, in slots past the
# tab's last. A rebuild first strips what this source listed before
# (its marker's mall packages). A scroll some package already sells, an
# event copy, a full tab or a tab without a price is reported unlisted.
# ================
def _list_in_mall(output, native, item_rows, report, prior, source, native_goods=()):
	output = Path(output)
	mall.strip_mall(output, set(prior.get("mall", {}).get("packages", [])))
	files = {name: safe_target(output / TEXT_ROOT, name) for name, _ in mall.MALL_TABLES}

	def raw(name):
		if files[name].exists():
			return files[name].read_bytes()
		path = Path(native) / TEXT_ROOT / name
		return path.read_bytes() if path.exists() else None

	if any(raw(name) is None for name, _ in mall.MALL_TABLES):
		report["mall"] = {"packages": [], "listed": 0, "unlisted": [], "note": "no mall tables in the tree"}
		return
	current = {name: tables.table_rows(raw(name)) for name, _ in mall.MALL_TABLES}
	# A later source's listings from the previous build are stale: it strips
	# and re-lists them after this pass. They leave the tables this pass
	# counts, so neither the slots nor the authored ids walk upward a
	# rebuild at a time (mall_avatar_import.py does the same for avatars).
	stale = set()
	for later in SOURCES[SOURCES.index(source) + 1:]:
		stale |= set(read_marker(output, later).get("mall", {}).get("packages", []))
	current = mall.without_packages(current, stale)
	kind_of_scroll = {code: added["kind"] for added in report.get("added", []) for code in added.get("scrolls", [])}
	kind_of_scroll.update({row[items.CODENAME]: kind for row, kind, _ in native_goods})
	sold = {row[3] for row in current["refscrapofpackageitem.txt"] if len(row) > 3}
	# A native kind some scroll of it already sells (the flute's silk copy,
	# the rabbit's gold one) needs no second listing; only kinds nobody can
	# get list.
	sold_families = {family for row, _, family in native_goods if row[items.CODENAME] in sold}
	item_rows = list(item_rows)
	unlisted = []
	for row, _, family in native_goods:
		if family in sold_families and row[items.CODENAME] not in sold:
			unlisted.append({"code": row[items.CODENAME], "reason": "kind already sold"})
		else:
			item_rows.append(row)
	slots = collections.defaultdict(lambda: -1)
	for row in current["refshopgoods.txt"]:
		if len(row) > 4 and row[4].lstrip("-").isdigit():
			slots[row[2]] = max(slots[row[2]], int(row[4]))
	rows = {name: [] for name, _ in mall.MALL_TABLES}
	ids = mall_authoring.AuthoredIds(
		{name: [] for name, _ in mall.MALL_TABLES}, current,
		{int(row[2]) for row in current["refpackageitem.txt"] if len(row) > 2 and row[2].isdigit()})
	packages = []
	for row in sorted(item_rows, key=lambda row: row[items.CODENAME]):
		code = row[items.CODENAME]
		tab = MALL_TAB.get(kind_of_scroll.get(code, ""))
		price = tab_price(current, tab) if tab else None
		if code.startswith(EVENT_SCROLL_PREFIX):
			unlisted.append({"code": code, "reason": "event copy"})
		elif code in sold:
			unlisted.append({"code": code, "reason": "already sold"})
		elif price is None:
			unlisted.append({"code": code, "reason": f"no priced tab for {kind_of_scroll.get(code, 'its kind')}"})
		elif slots[tab] + 1 >= mall.MALL_SLOTS:
			unlisted.append({"code": code, "reason": f"{tab} is full"})
		else:
			slots[tab] += 1
			package = mall_authoring.author_package(rows, row, ids, price)
			rows["refshopgoods.txt"].append(["1", mall.NATIVE_SOURCE, tab, package, str(slots[tab])] + ["-1", items.UNUSED] * 4)
			packages.append(package)
	for name, _ in mall.MALL_TABLES:
		if rows[name]:
			write_atomically(files[name], characters.append_rows(raw(name), rows[name]))
	report["mall"] = {"packages": packages, "listed": len(packages), "unlisted": unlisted}


# ================
# native_scrolls
#
# The v1.150 summon scrolls of every native kind on a COS lane, as
# (scroll row, kind, family) triples for _list_in_mall. Nine native kinds (Rudolph,
# Unicorn, Wild Boar, Glider, Jinn, Raccoon Dog, Behemoth, Donkey, Lizard)
# have a scroll no shop or mall package sells, so no player could get one
# (owner 2026-11-28: every pet is in the game); _list_in_mall skips the
# ones some package already sells, the other scrolls of a kind one of them
# sells, and the event copies.
# ================
def native_scrolls(native_chars, native_items):
	families = collections.defaultdict(list)
	for row in native_chars:
		if len(row) > CHAR_LEVEL and row[CHAR_CODE].startswith("COS_"):
			families[family_of(row[CHAR_CODE])].append(row)
	kind_of_code = {}
	for family, rows in families.items():
		kind = kind_of(family, rows)
		if kind in LANE_KINDS:
			kind_of_code[family] = (kind, family)
			kind_of_code.update({row[CHAR_CODE]: (kind, family) for row in rows})
	return [
		(row,) + kind_of_code[row[ITEM_ASSOCIATED_CHAR].strip()] for row in native_items
		if len(row) > ITEM_ASSOCIATED_CHAR and row[items.SERVICE] == SERVICE_ON
		and row[ITEM_ASSOCIATED_CHAR].strip() in kind_of_code
	]


# ================
# inferred_tables
#
# The inferred source's tables: pet_inference's rows for the COS models
# no source has a row for, built on this client's template rows, with
# their object text encoded as a later object table. An inferred icon
# neither the native tree nor this client's Media holds takes the
# template's.
# ================
def inferred_tables(later_chars, later_items, later_text, native, media_index):
	icon_root = Path(native) / ICON_ROOT

	def has_icon(name):
		return (icon_root / name).is_file() or ("icon/" + name) in media_index

	texts = {row[1]: row for row in characters.translation_rows(later_text) if len(row) > 1}
	chars, scrolls, text_rows = pet_inference.infer_kinds(later_chars, later_items, texts, has_icon)
	return chars, scrolls, _encode(text_rows)


def _write_pass(output, source, char_rows, item_rows, report):
	text_root = Path(output) / TEXT_ROOT
	if char_rows:
		write_atomically(safe_target(text_root, source["char_shard"]), _encode(sorted(char_rows, key=lambda row: int(row[CHAR_ID]))))
		_manifest_with(output, "characterdata.txt", source["char_shard"])
	else:
		# No shard file, so no manifest line either: a listed but
		# missing shard fails the table assembly that reads it.
		_drop_manifest_line(output, "characterdata.txt", source["char_shard"])
		(text_root / source["char_shard"]).unlink(missing_ok=True)
		_drop_skill_shard(text_root, source["skill_shard"])
	if item_rows:
		write_atomically(safe_target(text_root, source["item_shard"]), _encode(sorted(item_rows, key=lambda row: int(row[items.IDENTITY]))))
		_manifest_with(output, "itemdata.txt", source["item_shard"])
	else:
		_drop_manifest_line(output, "itemdata.txt", source["item_shard"])
		(text_root / source["item_shard"]).unlink(missing_ok=True)
	report["enabled"] = True
	report["source"] = source["name"]
	write_atomically(Path(output) / source["report"], (json.dumps(report, indent=2) + "\n").encode("utf-8"))
	return report


def _disabled(source, note):
	return {"enabled": False, "added": [], "existing": [], "deferred": [], "skipped": [], "source": source["name"], "note": note}


# ================
# add_pets
#
# One source's pass over a client root (pk2 archives). root None, the
# master flag off, or the archives absent removes the source's previous
# import and reports disabled. A pinned source's root is verified
# against the pins file; without the pins file in the tree the pass
# refuses (the supply chain is pinned, never guessed).
# ================
def add_pets(root, native, output, source=VSRO_SOURCE, earlier_codes=(), env=os.environ):
	output = Path(output)
	if root is None or not enabled(env) or not all((Path(root) / f"{name}.pk2").is_file() for name in FOLDERS):
		remove_import(output, source)
		return _disabled(source, "no later client found")
	if source["pins"] is not None:
		if not Path(source["pins"]).is_file():
			remove_import(output, source)
			return _disabled(source, "archive pins not in tree")
		tables.require_vsro_pin(root, tuple(FOLDERS), read_pin_field(source["pins"], PIN_FORMAT, "archives"))
	own_shards = (source["char_shard"], source["item_shard"], source["skill_shard"])
	native_chars, native_items = native_tables(native, exclude_shards=own_shards)
	later_chars, later_items, later_text = later_tables_from_pk2(root)
	res_root = Path(native) / "Data_extracted" / "res"
	native_models = {path.name.lower() for path in res_root.rglob("*.bsr")} if res_root.exists() else set()
	with open(Path(root) / "Media.pk2", "rb") as media_handle, mmap.mmap(media_handle.fileno(), 0, access=mmap.ACCESS_READ) as media, \
			open(Path(root) / "Data.pk2", "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as data:
		media_index = tables.archive_index(media)
		data_index = tables.archive_index(data)
		if source.get("infer"):
			later_chars, later_items, later_text = inferred_tables(later_chars, later_items, later_text, native, media_index)

		def missing(family, rows):
			for row in rows:
				model = row[CHAR_MODEL].replace("\\", "/").lower() if len(row) > CHAR_MODEL else ""
				if not model or (model.rsplit("/", 1)[-1] not in native_models and f"res/{model}" not in data_index):
					return ["model"]
			return []

		read = later_tables.Archive.media(media, media_index)

		prior = read_marker(output, source)
		char_rows, item_rows, report = select_pets(
			native_chars, native_items, set(earlier_codes), later_chars, later_items,
			previous=previous_targets(prior), missing=missing,
			reserved_ids=published_ids.occupied_objects(Path(output) / TEXT_ROOT, (source["char_shard"], source["item_shard"])), pinned=published_ids.published(published_ids.OBJECTS))
		if char_rows:
			char_rows, skills = stage_pet_skills(
				read, Path(output) / TEXT_ROOT, source["skill_shard"], char_rows, prior.get("skills", {}))
			report["skills"] = skills
	if char_rows:
		_merge_name_text(output, char_rows, item_rows, later_text, report)
	report["_prior"] = prior
	_copy_icons(root, native, output, char_rows + item_rows, report)
	native_goods = native_scrolls(native_chars, native_items) if source is VSRO_SOURCE else ()
	_list_in_mall(output, native, item_rows, report, prior, source, native_goods)
	return _write_pass(output, source, char_rows, item_rows, report)


# ================
# add_archive_pets
#
# The pinned-archive source: the v2.355 export's custom pets ride the
# one opensro archive the avatar pass reads (AVATARS_ENV), opened
# through avatar_archive so its sha256 is verified against
# avatars_archive_pins.json before a byte is read; a mismatch stops the
# build. Unset AVATARS_ENV, a missing archive, an archive carrying no
# pet tables or the master flag off removes the pass's previous import.
# The archive is read, never the export folder.
# ================
def add_archive_pets(native, output, earlier_codes=(), env=os.environ):
	output = Path(output)
	zip_path = env.get(AVATARS_ENV, "").strip()
	if not zip_path or not enabled(env) or not Path(zip_path).is_file():
		remove_import(output, ARCHIVE_SOURCE)
		return _disabled(ARCHIVE_SOURCE, "no pets archive")
	archives, _identities = avatar_archive.open_archive(zip_path, AVATARS_PIN_FILE)
	media, media_index = archives["Media"]
	data, data_index = archives["Data"]
	read = later_tables.Archive.media(media, media_index)
	if not read.has("characterdata.txt") or not read.has("itemdata.txt"):
		remove_import(output, ARCHIVE_SOURCE)
		return _disabled(ARCHIVE_SOURCE, "archive carries no pet tables")
	own_shards = (ARCHIVE_SOURCE["char_shard"], ARCHIVE_SOURCE["item_shard"], ARCHIVE_SOURCE["skill_shard"])
	native_chars, native_items = native_tables(native, exclude_shards=own_shards)
	later_chars, later_items, later_text = later_tables_from_media(media, media_index, object_text_required=False)

	def missing(family, rows):
		for row in rows:
			model = row[CHAR_MODEL].replace("\\", "/").lower() if len(row) > CHAR_MODEL else ""
			native_hit = bool(model) and (Path(native) / "Data_extracted" / "res" / model).exists()
			if not model or (not native_hit and f"res/{model}" not in data_index):
				return ["model"]
		return []

	prior = read_marker(output, ARCHIVE_SOURCE)
	char_rows, item_rows, report = select_pets(
		native_chars, native_items, set(earlier_codes), later_chars, later_items,
		previous=previous_targets(prior), missing=missing,
		reserved_ids=published_ids.occupied_objects(Path(output) / TEXT_ROOT, (ARCHIVE_SOURCE["char_shard"], ARCHIVE_SOURCE["item_shard"])), pinned=published_ids.published(published_ids.OBJECTS))
	if char_rows:
		# An archive that ships no skilldata cannot feed fighters; merge
		# refuses a missing dependency. Pickup/vehicle kinds name no
		# DefaultSkillIDs and stage an empty skill map.
		if any(read.has(name) for name in ("skilldata.txt", "skilldataenc.txt")):
			char_rows, skills = stage_pet_skills(
				read, Path(output) / TEXT_ROOT, ARCHIVE_SOURCE["skill_shard"], char_rows, prior.get("skills", {}))
			report["skills"] = skills
		_merge_name_text(output, char_rows, item_rows, later_text, report)
		# The archive's resources join the staging tree at their pk2
		# paths, so the model builder reads one tree for every source.
		for key in sorted(data_index):
			target = safe_target(output / "Data_extracted", data_index[key].path)
			if not target.exists():
				write_atomically(target, sro_pk2.payload(data, data_index[key]))
	_list_in_mall(output, native, item_rows, report, prior, ARCHIVE_SOURCE)
	return _write_pass(output, ARCHIVE_SOURCE, char_rows, item_rows, report)


# ================
# v1696_root / add_all_sources
#
# The four passes in order over the variant staging tree, each seeing
# the row codenames the earlier ones imported.
# ================
def v1696_root(env=os.environ):
	value = env.get(V1696_ENV, "").strip()
	return Path(value) if value else None


def add_all_sources(vsro_root, native, output, env=os.environ):
	vsro = add_pets(vsro_root, native, output, VSRO_SOURCE, env=env)
	codes = set(read_marker(output, VSRO_SOURCE).get("idMap", {}))
	v1696 = add_pets(v1696_root(env), native, output, V1696_SOURCE, earlier_codes=codes, env=env)
	codes |= set(read_marker(output, V1696_SOURCE).get("idMap", {}))
	archive = add_archive_pets(native, output, earlier_codes=codes, env=env)
	codes |= set(read_marker(output, ARCHIVE_SOURCE).get("idMap", {}))
	inferred = add_pets(vsro_root, native, output, INFERRED_SOURCE, earlier_codes=codes, env=env)
	return {"pets": vsro, "petsV1696": v1696, "petsArchive": archive, "petsInferred": inferred}


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--vsro-root", type=Path, required=True)
	parser.add_argument("--v1696-root", type=Path, default=v1696_root(), help=f"the v1.696 client (else {V1696_ENV})")
	parser.add_argument("--native-extracted", type=Path, default=EXTRACTED_ROOT)
	parser.add_argument("--output-extracted", type=Path, default=REPO_ROOT / ".state" / "pets" / "extracted")
	args = parser.parse_args()
	summaries = add_all_sources(args.vsro_root, args.native_extracted, args.output_extracted)
	for name, summary in summaries.items():
		added = summary.get("added", [])
		print(f"[{name}] enabled: {summary.get('enabled')}; kinds added: {len(added)}; "
			f"existing: {len(summary.get('existing', []))}; deferred: {len(summary.get('deferred', []))}; skipped: {len(summary.get('skipped', []))}")
		for kind in added:
			print(f"  added {kind['family']} ({kind['kind']}, levels {kind['levels']}, scrolls {kind['scrolls']}, needs {kind['needs']})")


if __name__ == "__main__":
	main()
