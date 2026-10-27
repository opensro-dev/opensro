"""
===========================================================================

skill110_scope.py - phase-three evidence: the later client's skill tier

Reads the owner's two Media.pk2 archives through the shared PK2 reader and
compares their skilldata shards by codename, not numeric id. It reports the
player ranks the later client adds whose mastery requirement is above the
native v1.150 character cap, splitting trainable ranks from the chain sub-rows
a chain points at. Existence and gating are observed; a decoded frame is not
execution, and no archive or live variant is modified. Full output belongs in
.state, never the source tree.

===========================================================================
"""
import argparse
from collections import Counter
import json
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import compare_cap110 as comparison  # noqa: E402
import import_vsro_alexandria as archive  # noqa: E402
import sro_pk2  # noqa: E402
from prepare_client_resources import write_atomically  # noqa: E402
from sro_paths import GAME_ROOT, REPO_ROOT  # noqa: E402

SERVICE_COLUMN = 0
ID_COLUMN = 1
GROUP_COLUMN = 2
CODE_COLUMN = 3
LEVEL_COLUMN = 7
CHAIN_NEXT_COLUMN = 9
MASTERY_COLUMN = 34
MASTERY_LEVEL_COLUMN = 36
SP_COLUMN = 46
RACE_COLUMN = 47
NATIVE_MASTERY_CAP = 90
CHINA_RACE = "0"
EUROPE_RACE = "1"
FORMAT = "opensro-skill110-scope-v1"

LIMITS = [
	"Archive identity is observed; the later client's release version is not authenticated.",
	"Column indices are zero-based and reuse the compare_cap110 skilldata contract.",
	"A mastery requirement above the native cap selects a rank; it does not prove the rank is",
	"trainable, has an execution owner, or preserves the later effect when pressed.",
	"Trainable and chain-sub classification follows each row's ChainNext target, the server's own mark.",
]


# ================
# cell
#
# A short row is missing data, never a reason to guess a column.
# ================
def cell(row, index):
	return row[index] if len(row) > index else ""


# ================
# parse_int
# ================
def parse_int(value, default=0):
	try:
		return int(str(value).strip())
	except (TypeError, ValueError):
		return default


# ================
# player_skill
#
# The server's own skill catalogue line: a Chinese or European player rank.
# ================
def player_skill(codename):
	return codename.startswith("SKILL_CH_") or codename.startswith("SKILL_EU_")


# ================
# mastery_line
#
# SKILL_CH_<LINE>_<KIND>... -> <LINE>; the mastery id itself is the join key,
# this is only a human label derived from the codenames the tier groups.
# ================
def mastery_line(codename):
	parts = codename.split("_")
	return parts[2] if len(parts) > 2 else ""


# ================
# chain_sub_ids
#
# EnterWorld marks a raw id a chain sub-row when some row's ChainNext names it.
# Those are execution records a chain points at, not trainable ranks.
# ================
def chain_sub_ids(rows):
	targets = {cell(row, CHAIN_NEXT_COLUMN).strip() for row in rows}
	return {cell(row, ID_COLUMN).strip() for row in rows if cell(row, ID_COLUMN).strip() in targets}


# ================
# player_rows
# ================
def player_rows(rows):
	return [
		row for row in rows
		if cell(row, SERVICE_COLUMN).strip() == "1" and player_skill(cell(row, CODE_COLUMN).strip())
	]


# ================
# scope_tier
#
# native_rows and later_rows are raw skilldata shard rows. The report observes
# which later player ranks are new by codename and gated above the native cap.
# ================
def scope_tier(native_rows, later_rows):
	native_players = player_rows(native_rows)
	later_players = player_rows(later_rows)
	native_codes = {cell(row, CODE_COLUMN).strip() for row in native_players}

	new_by_code = [row for row in later_players if cell(row, CODE_COLUMN).strip() not in native_codes]
	tier = [row for row in new_by_code if parse_int(cell(row, MASTERY_LEVEL_COLUMN)) > NATIVE_MASTERY_CAP]
	subs = chain_sub_ids(later_rows)
	learnable = [row for row in tier if cell(row, ID_COLUMN).strip() not in subs]
	chain = [row for row in tier if cell(row, ID_COLUMN).strip() in subs]

	labels = {}
	for row in learnable:
		labels.setdefault(parse_int(cell(row, MASTERY_COLUMN)), Counter())[mastery_line(cell(row, CODE_COLUMN).strip())] += 1
	masteries = [
		{
			"id": mid,
			"race": "china" if mid < 500 else "europe",
			"line": labels[mid].most_common(1)[0][0],
			"ranks": sum(labels[mid].values()),
			"maxLevel": max(parse_int(cell(row, MASTERY_LEVEL_COLUMN)) for row in learnable if parse_int(cell(row, MASTERY_COLUMN)) == mid),
			"maxRank": max(parse_int(cell(row, LEVEL_COLUMN)) for row in learnable if parse_int(cell(row, MASTERY_COLUMN)) == mid),
		}
		for mid in sorted(labels)
	]
	return {
		"format": FORMAT,
		"limits": LIMITS,
		"native": {
			"playerRanks": len(native_players),
			"maxMasteryLevel": max((parse_int(cell(row, MASTERY_LEVEL_COLUMN)) for row in native_players), default=0),
		},
		"later": {
			"playerRanks": len(later_players),
			"maxMasteryLevel": max((parse_int(cell(row, MASTERY_LEVEL_COLUMN)) for row in later_players), default=0),
		},
		"newPlayerRanks": len(new_by_code),
		"aboveCapRanks": len(tier),
		"learnableRanks": len(learnable),
		"chainSubRanks": len(chain),
		# The learn gate refuses a zero-SP rank, so a zero here is a finding, not a rank to ship.
		"zeroSpLearnable": sum(parse_int(cell(row, SP_COLUMN)) == 0 for row in learnable),
		"masteryLevelValues": dict(sorted(Counter(parse_int(cell(row, MASTERY_LEVEL_COLUMN)) for row in learnable).items())),
		"race": {
			"china": sum(cell(row, RACE_COLUMN).strip() == CHINA_RACE for row in learnable),
			"europe": sum(cell(row, RACE_COLUMN).strip() == EUROPE_RACE for row in learnable),
		},
		"masteries": masteries,
		"learnable": [
			{
				"id": parse_int(cell(row, ID_COLUMN)),
				"codename": cell(row, CODE_COLUMN).strip(),
				"group": parse_int(cell(row, GROUP_COLUMN)),
				"mastery": parse_int(cell(row, MASTERY_COLUMN)),
				"masteryLevel": parse_int(cell(row, MASTERY_LEVEL_COLUMN)),
				"rank": parse_int(cell(row, LEVEL_COLUMN)),
				"sp": parse_int(cell(row, SP_COLUMN)),
				"race": cell(row, RACE_COLUMN).strip(),
			}
			for row in sorted(learnable, key=lambda item: parse_int(cell(item, ID_COLUMN)))
		],
	}


# ================
# compare
#
# The two skilldata shard sets are validated by the shared reader, so a short
# or undecodable row fails here instead of becoming a scope finding.
# ================
def compare(native_root, later_root):
	loaded, identities = [], []
	for root in (native_root, later_root):
		path, handle, data = archive.open_archive(root, "Media")
		try:
			index = {sro_pk2.fold_ascii(entry.path): entry for entry in sro_pk2.read_directory(data)}
			rows, sources = comparison.read_tables(data, index, "skilldata")
			loaded.append(rows)
			identities.append({"archive": path.name, "skilldata": sources})
		finally:
			data.close()
			handle.close()
	report = scope_tier(loaded[0], loaded[1])
	report["inputs"] = {"native": identities[0], "later": identities[1]}
	return report


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--native-root", type=Path, default=GAME_ROOT)
	parser.add_argument("--vsro-root", type=Path, required=True)
	parser.add_argument("--output", type=Path, default=REPO_ROOT / ".state" / "skill110-scope.json")
	args = parser.parse_args()
	# The pin authenticates the later archive before anything is read (#1745).
	archive.verify_vsro_archive(args.vsro_root, "Media")
	report = compare(args.native_root, args.vsro_root)
	args.output.parent.mkdir(parents=True, exist_ok=True)
	write_atomically(args.output, (json.dumps(report, indent=2, ensure_ascii=True) + "\n").encode("utf-8"))
	print(f"player ranks: native {report['native']['playerRanks']} -> later {report['later']['playerRanks']}")
	print(f"new by codename: {report['newPlayerRanks']}; above cap {NATIVE_MASTERY_CAP}: {report['aboveCapRanks']}")
	print(f"learnable: {report['learnableRanks']}; chain-sub: {report['chainSubRanks']}; zero-SP learnable: {report['zeroSpLearnable']}")
	for mastery in report["masteries"]:
		print(f"   {mastery['id']:4d} {mastery['race']:6s} {mastery['line']:10s} {mastery['ranks']:3d} ranks to level {mastery['maxLevel']}")
	print(f"Report: {args.output}")


if __name__ == "__main__":
	main()
