"""
===========================================================================

later_mall_items.py - the v1.696 Item Mall items the later goods sell

Later-native, no switch (#4240 part 3): staged whenever the v1.696
client root is named (SRO_V1696_ROOT), and reaped when it is not, the
rule later_mall_goods itself follows. The later mall's goods name
these items in their scraps; with the rows in the tree the goods list
at their v1.696 silk prices, and without them they stay unlisted
(later_mall_goods.py's "item not in the tree").

Eighteen items in five families, each admitted only because a lane
the server already runs, or one landing beside this import, serves
its type:

- 3/3/13/1, eight rows (the damage and max-HP/MP scrolls and
  potions): Desc1 names a SKILL_MALL_*_01 buff, so the item-owned
  skill lane (action/skillitemuse.go) serves them; the eight skills
  are staged with them, plus the expert package's APRU skill - which
  another build step may already have staged, in which case it is
  left standing and not staged again.
- 3/3/13/6, the resurrection scroll: the resurrection lane
  (action/resurrectionscroll.go) reads its Param1 as the flat HP/MP
  it revives with and finds no Param3, so it refunds no EXP - the
  cheap scroll beside the native 60P/100P rows, as its tooltip's
  silence on recovery says.
- 3/3/13/14, the expert package: the composite lane
  (action/compositeitem.go) already parses its [USU1][BFI1][UQL1]
  bundle; the booth item it names already stands in the tree.
- 3/3/13/21, six EXP / skill-EXP scrolls: the param-job lane
  (action/expscroll.go) raises the distributor's EXP keepers from
  Param1 seconds, Param2 kind and Param3 percent.
- 3/3/13/17, the inventory and warehouse additions: the expansion
  lane (action/expansion.go) grows the persisted capacities their
  tooltips promise (+32 once per character, +30 twice per account).

Left out on purpose, so no good sells an item nothing can serve:
the dungeon free-entry tickets (3/3/13/22 and 23 - the port has no
Forgotten World content at all, and the Pharaoh tomb ticket's free
entry is its own evidence job), the OptLevel upgrade scrolls
(3/3/3/14 - a timed equipment-plus buff with no lane yet) and the
premium quest ticket (3/3/3/15 - the ticket case is unported).
The miscellaneous alchemy tab's four families are left out on the
same ground: the degree-3 Destruction Rondo (3/3/11/9), the
12th-grade Magic Proof Stones (3/3/10/8), the 11th-grade Special
Awaken Stones (3/3/11/15) and the trade seals and statement
(3/3/9/0, 3/3/11/12) - later_mall_goods places their goods in the
Athanasia tab, so each lists the day its family is imported here
with a lane to serve it.
Their goods stay unlisted until those land.

Identities come from published_ids, above every object (items) and
skill row of the tree, marker-kept so a rebuild keeps them; a row
either family already has refuses. The shards are this import's own
(itemdata_330000.txt, skilldata_330000.txt): one importer owns one
shard, and the off path unlinks them. One native column is the
exception: the v1.150 20% damage scroll takes the later overlap word
(OVERLAP_CODES, #4690) in the output copy of its shard, and the marker
keeps the native word for the off path to put back. Names and
descriptions are the later client's own English (cap110 merge_texts),
and icons ride the cap110 asset closure. A build without the v1.696
client reaps
exactly what the marker records.

===========================================================================
"""
import json
import os
from contextlib import ExitStack
from pathlib import Path

import cap110_item_import as shared
import cap110_items as items
import cap110_monsters as cap110
import import_vsro_alexandria as tables
import jupiter_import as jupiter
import later_world_import
import mall_avatar_import as avatars
import nasrun_import as nasrun
import published_ids
import sro_pk2
from prepare_client_resources import safe_target, write_atomically
from sro_paths import trees_overlap

ROOT_ENV = "SRO_V1696_ROOT"
MARKER = ".opensro-later-mall-items.json"
TEXT_ROOT = shared.TEXT_ROOT
NAME_TABLE = "textdataname.txt"
# The shard bands are this import's own: no other importer writes
# these files (one importer owns one shard).
ITEM_SHARD = "itemdata_330000.txt"
SKILL_SHARD = "skilldata_330000.txt"
ITEM_MANIFEST = "itemdata.txt"
SKILL_MANIFEST = "skilldata.txt"
# The native skill rows whose overlap word (column 18, Action_Overlap)
# the later tables changed (#4690). The later mall damage scrolls and the
# Devil's Spirit's lent skills all install casting state 0x5E, so each
# refuses the others; the v1.150 20% scroll's word (0x405) predates that
# state and let it stack with them. v1.696 and the 1.188 server both
# carry 0x4055E for it. The import takes the later word while it runs
# and puts the native one back on its off path.
OVERLAP_CODES = ("SKILL_MALL_DAMAGE_INC_20P_SCROLL_01",)
SKILL_OVERLAP = 18

# The later text tables that carry item and skill names
# (mall_avatar_import reads the same three).
LATER_TEXT_TABLES = ("textdata_equip&skill.txt", "textdata_object.txt", "textdata_new.txt")

SKILL_SERVICE, SKILL_ID, SKILL_CODENAME = 0, 1, 3
SKILL_COLUMNS = 118
SKILL_ICON, SKILL_NAME_KEY, SKILL_DESCRIPTION_KEY = 61, 62, 64
SKILL_UNUSED = ("", "0", "xxx")

SKILL_ITEM_TYPE = ["3", "3", "13", "1"]
RESURRECTION_TYPE = ["3", "3", "13", "6"]
EXPERT_PACKAGE_TYPE = ["3", "3", "13", "14"]
EXP_SCROLL_TYPE = ["3", "3", "13", "21"]
EXPANSION_TYPE = ["3", "3", "13", "17"]

# The admitted rows and the type each must carry, in import order:
# first the skill-carrying consumables, then the resurrection scroll,
# the expert package, the EXP scrolls and the two additions.
ITEMS = (
	("ITEM_MALL_DAMAGE_INC_10P_SCROLL", SKILL_ITEM_TYPE),
	("ITEM_MALL_DAMAGE_ABS_10P_SCROLL", SKILL_ITEM_TYPE),
	("ITEM_MALL_DAMAGE_INC_ABS_10P_SCROLL", SKILL_ITEM_TYPE),
	("ITEM_MALL_DAMAGE_INC_ABS_20P_SCROLL", SKILL_ITEM_TYPE),
	("ITEM_MALL_HP_INC_1000_POTION", SKILL_ITEM_TYPE),
	("ITEM_MALL_HP_INC_2000_POTION", SKILL_ITEM_TYPE),
	("ITEM_MALL_MP_INC_1000_POTION", SKILL_ITEM_TYPE),
	("ITEM_MALL_MP_INC_2000_POTION", SKILL_ITEM_TYPE),
	("ITEM_MALL_RESURRECTION_SCROLL", RESURRECTION_TYPE),
	("ITEM_MALL_EXPERT_PACKAGE_SERVICE_GLOBAL", EXPERT_PACKAGE_TYPE),
	("ITEM_MALL_100_EXP_SCROLL_1TIME", EXP_SCROLL_TYPE),
	("ITEM_MALL_100_EXP_SCROLL_2TIME", EXP_SCROLL_TYPE),
	("ITEM_MALL_100_SKILL_EXP_SCROLL_1TIME", EXP_SCROLL_TYPE),
	("ITEM_MALL_200_SKILL_EXP_SCROLL_1TIME", EXP_SCROLL_TYPE),
	("ITEM_MALL_100_SKILL_EXP_SCROLL_2TIME", EXP_SCROLL_TYPE),
	("ITEM_MALL_200_SKILL_EXP_SCROLL_2TIME", EXP_SCROLL_TYPE),
	("ITEM_MALL_INVENTORY_ADDITION", EXPANSION_TYPE),
	("ITEM_MALL_WAREHOUSE_ADDITION", EXPANSION_TYPE),
)

# The skills the rows name: each 3/3/13/1 row's Desc1 buff, and the
# expert package's USU1 entry (SKILL_MALL_PRE_APRU_GLOBAL_4W_01).
SKILL_CODES = (
	"SKILL_MALL_DAMAGE_INC_10P_SCROLL_01",
	"SKILL_MALL_DAMAGE_ABS_10P_SCROLL_01",
	"SKILL_MALL_DAMAGE_INC_ABS_10P_SCROLL_01",
	"SKILL_MALL_DAMAGE_INC_ABS_20P_SCROLL_01",
	"SKILL_MALL_HP_INC_1000_POTION_01",
	"SKILL_MALL_HP_INC_2000_POTION_01",
	"SKILL_MALL_MP_INC_1000_POTION_01",
	"SKILL_MALL_MP_INC_2000_POTION_01",
	"SKILL_MALL_PRE_APRU_GLOBAL_4W_01",
)


# ================
# select_item_rows
#
# The admitted item rows of the later tables, in ITEMS order. A
# missing or miscast row refuses: a family half staged would list
# goods the tree cannot pair.
# ================
def select_item_rows(later_rows):
	by_code = {}
	for row in later_rows:
		if len(row) > items.CODENAME and row[items.SERVICE] == "1":
			by_code.setdefault(row[items.CODENAME], row)
	selected = []
	for position, (code, kind) in enumerate(ITEMS):
		row = by_code.get(code)
		if row is None:
			raise ValueError(f"the later client has no mall item row {code}")
		if len(row) != items.ITEM_COLUMNS or row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] != kind:
			raise ValueError(f"mall item row {code} is not of type {'/'.join(kind)}")
		for column in (items.DROP_MODEL, items.ICON):
			if row[column] in ("", "0", items.UNUSED):
				raise ValueError(f"mall item row {code} has no resource at column {column}")
		# A skill-carrying row must name the skill this import stages
		# for it (SKILL_CODES is in ITEMS order for these eight): a
		# row naming another skill would sell a buff nothing pins.
		if kind is SKILL_ITEM_TYPE and row[items.PARAM_DESCRIPTION] != SKILL_CODES[position]:
			raise ValueError(f"mall item row {code} names skill {row[items.PARAM_DESCRIPTION]!r}, want {SKILL_CODES[position]}")
		selected.append(row)
	return selected


# ================
# select_skill_rows
#
# The admitted skill rows of the later tables, in SKILL_CODES order.
# A missing or short row refuses, on the same ground as the items.
# ================
def select_skill_rows(later_rows):
	by_code = {}
	for row in later_rows:
		if len(row) > SKILL_CODENAME and row[SKILL_SERVICE] == "1":
			by_code.setdefault(row[SKILL_CODENAME], row)
	selected = []
	for code in SKILL_CODES:
		row = by_code.get(code)
		if row is None:
			raise ValueError(f"the later client has no mall skill {code}")
		if len(row) != SKILL_COLUMNS:
			raise ValueError(f"mall skill {code} has {len(row)} columns, want {SKILL_COLUMNS}")
		if row[SKILL_ICON] in SKILL_UNUSED or row[SKILL_NAME_KEY] in SKILL_UNUSED:
			raise ValueError(f"mall skill {code} has no icon or name at its columns")
		selected.append(row)
	return selected


# ================
# remove_assets
#
# The later files a marker records, under output (reset_scroll_import
# keeps the same rule: a native-sourced copy may be another layer's).
# ================
def remove_assets(output, records):
	for record in records:
		if record.get("source") == "later":
			safe_target(output / shared.FOLDERS[record["archive"]], record["path"]).unlink(missing_ok=True)


# ================
# restore_overlaps
#
# Put back the native overlap words a marker records. A shard this
# import copied into the output only to patch goes again once it reads
# as the native rows; a shard another layer wrote keeps that layer's
# rows with the native word restored.
# ================
def restore_overlaps(native, output, records):
	for record in records:
		path = safe_target(output / TEXT_ROOT, record["shard"])
		if not path.exists():
			continue
		rows = tables.table_rows(path.read_bytes())
		for row in rows:
			if len(row) > SKILL_OVERLAP and row[SKILL_CODENAME] == record["code"]:
				row[SKILL_OVERLAP] = record["native"]
		source = native / TEXT_ROOT / record["shard"]
		if record["created"] and source.exists() and rows == tables.table_rows(source.read_bytes()):
			path.unlink()
		else:
			write_atomically(path, cap110.encode_rows(rows))


# ================
# patch_overlaps
#
# Give each OVERLAP_CODES row the later table's overlap word, in the
# output copy of the shard that holds it (copied from the native tree
# when no layer wrote one). Returns the marker records restore_overlaps
# reads; a row the tree lacks refuses, as a missing later row does.
# ================
def patch_overlaps(native, output, later_rows):
	later = {row[SKILL_CODENAME]: row[SKILL_OVERLAP] for row in later_rows
		if len(row) > SKILL_OVERLAP and row[SKILL_SERVICE] == "1" and row[SKILL_CODENAME] in OVERLAP_CODES}
	records = []
	for code in OVERLAP_CODES:
		if code not in later:
			raise ValueError(f"the later client has no skill {code}")
		found = None
		for root in (output / TEXT_ROOT, native / TEXT_ROOT):
			for path in sorted(root.glob("skilldata_*.txt")):
				if sro_pk2.fold_ascii(path.name) == SKILL_SHARD:
					continue
				if any(len(row) > SKILL_OVERLAP and row[SKILL_CODENAME] == code for row in tables.table_rows(path.read_bytes())):
					found = path
					break
			if found:
				break
		if found is None:
			raise ValueError(f"no skill table holds {code}")
		target = safe_target(output / TEXT_ROOT, found.name)
		created = not target.exists()
		rows = tables.table_rows(found.read_bytes())
		for row in rows:
			if len(row) > SKILL_OVERLAP and row[SKILL_CODENAME] == code:
				records.append({"shard": found.name, "code": code, "native": row[SKILL_OVERLAP], "word": later[code], "created": created})
				row[SKILL_OVERLAP] = later[code]
		write_atomically(target, cap110.encode_rows(rows))
	return records


# ================
# remove_import
#
# The off path: the texts, the two shards and their manifest lines,
# the later asset files and the marker.
# ================
def remove_import(native, output, prior, marker):
	text_root = output / TEXT_ROOT
	nasrun.strip_texts(safe_target(text_root, NAME_TABLE), set(prior.get("texts", [])))
	restore_overlaps(native, output, prior.get("overlaps", []))
	for shard_name, manifest_name in ((ITEM_SHARD, ITEM_MANIFEST), (SKILL_SHARD, SKILL_MANIFEST)):
		shard = safe_target(text_root, shard_name)
		if shard.exists():
			shard.unlink()
		manifest = safe_target(text_root, manifest_name)
		if manifest.exists():
			names = [row[0] for row in tables.table_rows(manifest.read_bytes()) if row]
			kept = [name for name in names if sro_pk2.fold_ascii(name) != shard_name]
			if len(kept) != len(names):
				write_atomically(manifest, cap110.encode_rows([[name] for name in kept]))
	remove_assets(output, prior.get("assets", []))
	marker.unlink(missing_ok=True)


# ================
# add_later_mall_items
#
# The layer the Alexandria builder runs with the other later layers,
# before the later mall goods list: stage the eighteen items and
# their nine skills in their shards under marker-kept identities,
# with their later English texts and icons, or (no v1.696 client
# named) reap a previous import.
# ================
def add_later_mall_items(native, output, v1696_root=None, env=os.environ):
	native, output = Path(native).resolve(), Path(output).resolve()
	if trees_overlap(native, output):
		raise ValueError("later mall items output must be separate from the native extraction")
	marker = output / MARKER
	prior = cap110.read_json(marker) if marker.exists() else {}
	text_root = output / TEXT_ROOT
	root_named = str(v1696_root or "").strip() or env.get(ROOT_ENV, "").strip()
	if not root_named:
		remove_import(native, output, prior, marker)
		return {"enabled": False, "reason": f"{ROOT_ENV} is not set"}
	root = Path(root_named)
	jupiter.require_v1696_pin(root, tuple(shared.FOLDERS))
	item_codes = [code for code, _ in ITEMS]
	# A rebuild starts from the tables without this import's own rows.
	nasrun.strip_texts(safe_target(text_root, NAME_TABLE), set(prior.get("texts", [])))
	restore_overlaps(native, output, prior.get("overlaps", []))
	for path in sorted(text_root.glob("itemdata*.txt")):
		if sro_pk2.fold_ascii(path.name) in (ITEM_SHARD, ITEM_MANIFEST):
			continue
		for row in tables.table_rows(path.read_bytes()):
			if len(row) > items.CODENAME and row[items.SERVICE] == "1" and row[items.CODENAME] in item_codes:
				raise ValueError(f"mall item {row[items.CODENAME]} already stands in the item tables")
	with ExitStack() as stack:
		archives, identities = avatars.open_archives(stack, root, avatars.V1696_SOURCE)
		media, index = archives["Media"]
		item_rows = select_item_rows(avatars.later_table_rows(media, index, "itemdata.txt"))
		later_skills = avatars.later_table_rows(media, index, "skilldata.txt")
		skill_rows = select_skill_rows(later_skills)

		occupied = published_ids.occupied_objects(text_root, (ITEM_SHARD,))
		allocated = published_ids.allocate(published_ids.OBJECTS, occupied, item_codes, prior.get("items", {}))
		staged_items = []
		for row in item_rows:
			converted = list(row)
			converted[items.IDENTITY] = str(allocated[row[items.CODENAME]])
			converted[items.PLACEHOLDER_NAME] = items.UNUSED
			staged_items.append(converted)

		# Skill ids stand above every skill of the native tables and
		# of each shard an earlier pass wrote into the output.
		occupied_skills = set()
		standing_skills = set()
		for path in sorted({*text_root.glob("skilldata_*.txt"), *(native / TEXT_ROOT).glob("skilldata_*.txt")}):
			if sro_pk2.fold_ascii(path.name) == SKILL_SHARD:
				continue
			for row in tables.table_rows(path.read_bytes()):
				if len(row) > SKILL_CODENAME and row[SKILL_ID].strip().isdigit():
					occupied_skills.add(int(row[SKILL_ID]))
				if row[SKILL_CODENAME] in SKILL_CODES:
					# Already standing before this import: another
					# build step owns that skill (the expert
					# package references it by name); only its
					# absence from the later rows is a defect.
					standing_skills.add(row[SKILL_CODENAME])
		staged_codes = [code for code in SKILL_CODES if code not in standing_skills]
		allocated_skills = published_ids.allocate(published_ids.SKILLS, occupied_skills, staged_codes, prior.get("skills", {}))
		staged_skills = []
		for row in skill_rows:
			if row[SKILL_CODENAME] in standing_skills:
				continue
			converted = list(row)
			converted[SKILL_ID] = str(allocated_skills[row[SKILL_CODENAME]])
			staged_skills.append(converted)

		symbols = {row[column] for row in staged_skills for column in (SKILL_NAME_KEY, SKILL_DESCRIPTION_KEY)} - set(SKILL_UNUSED)
		later_texts = [row for name in LATER_TEXT_TABLES for row in avatars.later_text_rows(media, index, name)]
		name_path = safe_target(text_root, NAME_TABLE)
		base_names = name_path.read_bytes() if name_path.exists() else (native / TEXT_ROOT / NAME_TABLE).read_bytes()
		merged_names, absent_descriptions = items.merge_texts(base_names, later_texts, staged_items, extra=sorted(symbols))
		known = {row[1] for row in tables.table_rows(base_names) if len(row) > 1}
		after = {row[1] for row in tables.table_rows(merged_names) if len(row) > 1}
		missing_names = {row[SKILL_NAME_KEY] for row in staged_skills} - after
		if missing_names:
			raise ValueError(f"missing mall skill names: {sorted(missing_names)}")

		item_assets = shared.copy_asset_closure(staged_items, native, output, archives, worn=False)
		icons = {("Media", "icon/" + shared.normalized_path(row[SKILL_ICON])) for row in staged_skills}
		skill_assets = shared.copy_resource_closure(icons, native, output, archives)
	staged_items.sort(key=lambda row: int(row[items.IDENTITY]))
	staged_skills.sort(key=lambda row: int(row[SKILL_ID]))
	write_atomically(safe_target(text_root, ITEM_SHARD), cap110.encode_rows(staged_items))
	write_atomically(safe_target(text_root, SKILL_SHARD), cap110.encode_rows(staged_skills))
	write_atomically(name_path, merged_names)
	overlaps = patch_overlaps(native, output, later_skills)
	for shard_name, manifest_name in ((ITEM_SHARD, ITEM_MANIFEST), (SKILL_SHARD, SKILL_MANIFEST)):
		manifest = safe_target(text_root, manifest_name)
		current = manifest.read_bytes() if manifest.exists() else (native / TEXT_ROOT / manifest_name).read_bytes()
		names = [row[0] for row in tables.table_rows(current) if row]
		names = [name for name in names if sro_pk2.fold_ascii(name) != shard_name] + [shard_name]
		write_atomically(manifest, cap110.encode_rows([[name] for name in names]))
	summary = {
		"enabled": True, "format": "opensro-later-mall-items-v1", "source": "v1696",
		"laterArchiveHashes": identities,
		"items": {row[items.CODENAME]: int(row[items.IDENTITY]) for row in staged_items},
		"skills": {row[SKILL_CODENAME]: int(row[SKILL_ID]) for row in staged_skills},
		"texts": sorted(after - known), "absentDescriptions": absent_descriptions,
		"assets": item_assets + skill_assets,
		"overlaps": overlaps,
		"limits": [
			"The dungeon free-entry tickets (3/3/13/22, 3/3/13/23) are not imported: no Forgotten World content stands in the port, and the Pharaoh tomb ticket's free entry is unported.",
			"The OptLevel upgrade scrolls (3/3/3/14) and the premium quest ticket (3/3/3/15) are not imported: neither case has a lane yet.",
			"No drop or event reward names these rows; they arrive by mall purchase or GM grant alone.",
		],
	}
	write_atomically(marker, (json.dumps(summary, indent=2) + "\n").encode())
	return summary
