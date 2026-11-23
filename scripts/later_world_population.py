"""
===========================================================================

later_world_population.py - a spec's population export, read and projected.

The three moves every later-world population makes before the shared
engine (later_world_import) stages anything: read the pinned export
and refuse a wrong one (read_population), project its nest rows into
the Version-2 schema the server reads, validating extraction facts the
schema does not carry (project_population), and choose which claimed
kinds a pass stages when a spec places kinds that already stand
(staged_refs). One owner for the export's shape, so the engine and the
four skins read it the same way.

===========================================================================
"""
import json
from pathlib import Path

import cap110_monsters as cap110
from isror_nest_table import require_native_nest_table


# ================
# read_population
#
# The population export: its recorded backup hash must be the pinned
# one, its nests must come from ISRO-R's native table, and the spec's
# worlds must all be present.
# ================
def read_population(spec, source_root):
	path = Path(source_root) / spec.population_name
	try:
		export = json.loads(path.read_text(encoding="utf-8"))
	except (OSError, ValueError) as error:
		raise SystemExit(f"{path} is unreadable: {error}")
	if str(export.get("sourceSha256", "")).lower() != spec.source_sha256:
		raise ValueError(
			f"population export {path} records backup {export.get('sourceSha256') or 'none'}, "
			f"not the pinned shard backup ({spec.source_sha256})"
		)
	require_native_nest_table(export.get("nestTable"), f"population export {path}")
	worlds = export.get("worlds") or {}
	for world in spec.worlds_required:
		if str(world) not in worlds:
			raise ValueError(f"population export {path} has no world {world}")
	return export


# ================
# project_population
#
# The nest rows of the given worlds, in the cap-110 Version-2 row
# schema the server reads. Extraction facts the schema does not carry
# (the continent, the hive join) are validated, never kept: a nest
# flag or a keep-count hive would need a projection this family does
# not have, so it refuses by name instead of silently shrinking. A
# hive total cap is the one exception: the row schema has no
# cross-nest cap, so a capped kind projects onto its first nest with
# the cap as its count. The spawn kind rides the row as SpawnType,
# as the temple loader preserves it.
# ================
def project_population(spec, rows, worlds):
	projected = []
	hive_kept = set()
	for row in sorted(rows, key=lambda item: item["NestID"]):
		if row["WorldID"] not in worlds:
			continue
		hive = row.get("Hive") or {}
		if row.get("NestFlag") or hive.get("KeepMonsterCountType"):
			raise ValueError(f"{spec.label} nest requires a projection this family does not have: " + row["Codename"])
		cap = hive.get("OverwriteMaxTotalCount") or 0
		if cap:
			if row["Codename"] in hive_kept:
				continue
			hive_kept.add(row["Codename"])
		out = {
			"Codename": row["Codename"], "NestID": row["NestID"], "WorldID": row["WorldID"],
			"RegionID": row["RegionID"], "X": row["X"], "Y": row["Y"], "Z": row["Z"],
			"Radius": row["Radius"], "GenerateRadius": row["GenerateRadius"],
			"MaxCount": min(row["MaxCount"], cap) if cap else row["MaxCount"], "RespawnDelayMinSec": row["RespawnDelayMinSec"],
			"RespawnDelayMaxSec": row["RespawnDelayMaxSec"], "Respawn": row["Respawn"],
			"InitialDir": row["InitialDir"], "ChampionGenPercentage": row["ChampionGenPercentage"],
			"Controls": row["Controls"], "Champion": row["Champion"],
		}
		if row.get("SpawnType"):
			out["SpawnType"] = row["SpawnType"]
		projected.append(out)
	projected.sort(key=lambda row: row["NestID"])
	return {"Version": 2, "Rows": projected}


# ================
# staged_refs
#
# The claimed kinds a pass stages. A spec that places standing kinds
# (place_standing) leaves a kind another layer already staged on its
# row: one codename, one row, so this pass's nests name the standing
# row and it writes no second one (hunting2's rule). Every other spec
# stages them all, and stage_kinds refuses a standing kind by name.
# ================
def staged_refs(spec, text_root, refs):
	if not spec.place_standing:
		return refs
	standing = {row[2] for row in cap110.tree_characters(text_root, spec.character_shard) if len(row) > 2}
	return {code: row for code, row in refs.items() if code not in standing}


