"""
===========================================================================

compare_cap110.py - phase-zero evidence for the optional 110-cap expansion

Reads the owner's two Media.pk2 archives through the shared PK2 reader.
Follows their manifests, joins records by codename rather than numeric ID,
and audits skills with the server's production parameter compiler. Reports
observed differences; equal widths do not prove equal field semantics, and
decoded skills do not imply implemented combat effects. No archive or live
variant is modified. Full output belongs in .state, never the source tree.

===========================================================================
"""
import argparse
from collections import Counter
import json
from pathlib import Path
import re
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import import_vsro_alexandria as archive  # noqa: E402
import sro_pk2  # noqa: E402
from prepare_client_resources import write_atomically  # noqa: E402
from sro_paths import GAME_ROOT, REPO_ROOT  # noqa: E402

TABLE_KEYS = {"itemdata": 2, "characterdata": 2, "skilldata": 3}
SERVICE_COLUMN = 0
ID_COLUMN = 1
LEVEL_COLUMN = 0
EXPERIENCE_COLUMN = 1
EXAMPLE_LIMIT = 5
FORMAT = "opensro-cap110-comparison-v1"


# ================
# read_tables
#
# Manifest entries are archive names, never filesystem paths or commands.
# ================
def read_tables(data, index, family):
	names = [row[0].strip() for row in archive.table_rows(archive.table_text(data, index, family + ".txt"))]
	pattern = re.compile(re.escape(family) + r"_\d+\.txt", re.IGNORECASE)
	if not names or any(not pattern.fullmatch(name) for name in names):
		raise ValueError(f"invalid {family} shard manifest")
	if len({sro_pk2.fold_ascii(name) for name in names}) != len(names):
		raise ValueError(f"duplicate {family} shard manifest entry")
	rows, sources = [], []
	for name in names:
		raw = archive.table_text(data, index, sro_pk2.fold_ascii(name))
		parsed = archive.table_rows(raw)
		if any(len(row) <= TABLE_KEYS[family] or "\ufffd" in "".join(row) for row in parsed):
			raise ValueError(f"malformed or undecodable {name}")
		rows.extend(parsed)
		sources.append({"name": name, "sha256": sro_pk2.digest(raw), "rows": len(parsed)})
	return rows, sources


# ================
# unique_rows
#
# Ambiguous codenames stay visible and are excluded from field comparisons.
# ================
def unique_rows(rows, key_column):
	counts = Counter(row[key_column] for row in rows)
	duplicates = sorted(code for code, count in counts.items() if count > 1)
	return {row[key_column]: row for row in rows if counts[row[key_column]] == 1}, duplicates


# ================
# compare_rows
#
# Position-wise equality is a diagnostic, not an inferred column mapping.
# ================
def compare_rows(native, later, key_column):
	left, left_duplicates = unique_rows(native, key_column)
	right, right_duplicates = unique_rows(later, key_column)
	shared = sorted(left.keys() & right.keys())
	columns = []
	width = max((len(row) for row in native + later), default=0)
	for column in range(width):
		compared, equal, examples = 0, 0, []
		for code in shared:
			if column >= min(len(left[code]), len(right[code])):
				continue
			compared += 1
			if left[code][column] == right[code][column]:
				equal += 1
			elif len(examples) < EXAMPLE_LIMIT:
				examples.append({"code": code, "native": left[code][column], "later": right[code][column]})
		columns.append({"column": column, "compared": compared, "equal": equal, "differences": examples})
	return {
		"nativeRows": len(native), "laterRows": len(later),
		"nativeWidths": dict(sorted(Counter(len(row) for row in native).items())),
		"laterWidths": dict(sorted(Counter(len(row) for row in later).items())),
		"nativeEnabled": sum(row[SERVICE_COLUMN] == "1" for row in native),
		"laterEnabled": sum(row[SERVICE_COLUMN] == "1" for row in later),
		"nativeDuplicateCodes": left_duplicates, "laterDuplicateCodes": right_duplicates,
		"sharedUniqueCodes": len(shared),
		"renumberedSharedCodes": sum(left[code][ID_COLUMN] != right[code][ID_COLUMN] for code in shared),
		"addedCodes": sorted(right.keys() - left.keys() - set(left_duplicates)),
		"removedCodes": sorted(left.keys() - right.keys() - set(right_duplicates)),
		"columns": columns
	}


# ================
# compare_levels
# ================
def compare_levels(native, later):
	tables = []
	for rows in (native, later):
		levels = {}
		for row in rows:
			level, experience = int(row[LEVEL_COLUMN]), int(row[EXPERIENCE_COLUMN])
			if level in levels or level < 1 or experience < 1:
				raise ValueError("duplicate or invalid level curve row")
			levels[level] = experience
		tables.append(levels)
	left, right = tables
	return {
		"nativeRows": len(left), "laterRows": len(right),
		"nativeOnlyLevels": sorted(left.keys() - right.keys()),
		"laterOnlyLevels": sorted(right.keys() - left.keys()),
		"differences": [
			{"level": level, "nativeExp": left[level], "laterExp": right[level]}
			for level in sorted(left.keys() & right.keys()) if left[level] != right[level]
		]
	}


# ================
# audit_skills
#
# Failures stop at the first undecodable block. Later cells have no proven
# boundaries and must not be scanned as if they were additional opcodes.
# ================
def audit_skills(executable, rows):
	enabled = [row for row in rows if row[SERVICE_COLUMN] == "1"]
	request = "".join(json.dumps(row, ensure_ascii=True) + "\n" for row in enabled)
	result = subprocess.run([str(executable)], input=request, text=True, encoding="utf-8", capture_output=True, check=True)
	findings = [json.loads(line) for line in result.stdout.splitlines()]
	if len(findings) != len(enabled):
		raise ValueError("skill auditor response count differs from request")
	tags, failures = Counter(), []
	for row, finding in zip(enabled, findings):
		if "error" in finding:
			failures.append({"code": row[TABLE_KEYS["skilldata"]], "id": row[ID_COLUMN], "error": finding["error"]})
		else:
			tags.update(set(finding["tags"]))
	return {
		"enabledRows": len(enabled), "decodedRows": len(enabled) - len(failures),
		"tagsInDecodedRows": {f"0x{tag:08x}": count for tag, count in sorted(tags.items())},
		"failures": failures
	}


# ================
# compare_archives
# ================
def compare_archives(native_root, later_root, executable):
	inputs, loaded, levels, audits = [], [], [], []
	for root in (native_root, later_root):
		path, handle, data = archive.open_archive(root, "Media")
		try:
			index = {sro_pk2.fold_ascii(entry.path): entry for entry in sro_pk2.read_directory(data)}
			identity = {"archive": path.name, "bytes": len(data), "sha256": archive.sha256_of(data), "tables": {}}
			tables = {}
			for family in TABLE_KEYS:
				tables[family], identity["tables"][family] = read_tables(data, index, family)
			raw = archive.table_text(data, index, "leveldata.txt")
			identity["tables"]["leveldata"] = {"sha256": sro_pk2.digest(raw)}
			levels.append(archive.table_rows(raw))
			loaded.append(tables)
			inputs.append(identity)
			audits.append(audit_skills(executable, tables["skilldata"]))
		finally:
			data.close()
			handle.close()
	native, later = loaded
	left_tags, right_tags = (set(audit["tagsInDecodedRows"]) for audit in audits)
	native_failures = {row["code"]: row["error"] for row in audits[0]["failures"]}
	return {
		"format": FORMAT,
		"decoderSources": {
			name: sro_pk2.digest((REPO_ROOT / "apps" / "server" / "internal" / "game" / "enterworld" / name).read_bytes())
			for name in ("skillprogram.go", "spawnskillparams.go", "skillencoded.go", "itemrefs.go")
		},
		"limits": [
			"Archive identity is observed; the later client's release version is not authenticated.",
			"Column indices are zero-based. Equal positions or widths do not establish identical semantics.",
			"Skill decoding uses the current port compiler, not a new binary disassembly.",
			"A failed skill reports its first failure only; decoded framing does not establish execution support."
		],
		"inputs": {"native": inputs[0], "later": inputs[1]},
		"tables": {family: compare_rows(native[family], later[family], key) for family, key in TABLE_KEYS.items()},
		"levels": compare_levels(*levels),
		"skills": {
			"native": audits[0], "later": audits[1],
			"laterOnlyDecodedTags": sorted(right_tags - left_tags),
			"introducedOrChangedFailures": [
				row for row in audits[1]["failures"] if native_failures.get(row["code"]) != row["error"]
			]
		}
	}


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--native-root", type=Path, default=GAME_ROOT)
	parser.add_argument("--vsro-root", type=Path, required=True)
	parser.add_argument("--output", type=Path, default=REPO_ROOT / ".state" / "cap110-comparison.json")
	args = parser.parse_args()
	# The report used to leave the later archive's identity merely observed;
	# the pin authenticates it before anything is read (#1745).
	archive.verify_vsro_archive(args.vsro_root, "Media")
	build = REPO_ROOT / ".state" / "cap110-audit"
	build.mkdir(parents=True, exist_ok=True)
	executable = build / ("skill-program-audit.exe" if sys.platform == "win32" else "skill-program-audit")
	subprocess.run(
		["go", "build", "-o", str(executable), "./cmd/tools/skill-program-audit"],
		cwd=REPO_ROOT / "apps" / "server", check=True
	)
	report = compare_archives(args.native_root, args.vsro_root, executable)
	write_atomically(args.output, (json.dumps(report, indent=2, ensure_ascii=True) + "\n").encode("utf-8"))
	for family, table in report["tables"].items():
		print(f"{family}: widths {table['nativeWidths']} -> {table['laterWidths']}; "
			f"{table['sharedUniqueCodes']} shared, {len(table['addedCodes'])} added codenames")
	print(f"Changed EXP rows: {len(report['levels']['differences'])}")
	print(f"Introduced/changed skill framing failures: {len(report['skills']['introducedOrChangedFailures'])}")
	print(f"Report: {args.output}")


if __name__ == "__main__":
	main()
