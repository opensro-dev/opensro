"""
===========================================================================
revtool_evidence.py - evidence artifacts, doctor/dossier/status and RTTI classes

Part of revtool, the non-destructive symbol overlay for the SRO_Client
pseudocode dump. revtool.py is the entry point; this module owns one
command group or layer and is imported by it (and by revtool_rizin.py
through revtool's re-exports). The dump is an immutable Binary Ninja
hypothesis snapshot: original PE bytes and exact proofs outrank it,
and nothing here edits it.
===========================================================================
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import csv
import hashlib
import json
import re
import subprocess

from revtool_asserts import _ARR_ENTRY_RE, _ARR_SINGLE_RE, _ARR_START_RE
from revtool_asserts import _COL_RE, _VFTABLE_RE
from revtool_debt import compute_debt, debt_limit, strict_enabled
from revtool_dump import evidence_artifact_paths, get_binary, get_dump
from revtool_ingest import db_claims, default_sync_path, sync_diff
from revtool_ingest import validate_ledger_meta
from revtool_paths import DEFAULT_EVIDENCE_LOCK, EVIDENCE_LOCK_SCHEMA
from revtool_paths import HERE, PROOF_TOOLS, REBUILD_ROOT, RECONSTRUCT_DIR
from revtool_paths import REVTOOL_SCHEMA_VERSION, TARGET_SHA256
from revtool_paths import WORKSPACE_ROOT
from revtool_pe import PEImage, _best_rtti_index, open_pe
from revtool_pe import resolve_addr
from revtool_store import get_author, meta_get, sha256_file
from revtool_syntax import ASSERT_STATUS, TIERS

def _human_bytes(n):
	"""`115553624` -> `110.2 MiB (115,553,624 bytes)` for status readouts. The raw
    byte count is kept because it is the exact validation target for a regenerated
    dump (see revtool_README.md Provisioning)."""
	units = ["bytes", "KiB", "MiB", "GiB"]
	size = float(n)
	unit = 0
	while size >= 1024 and unit < len(units) - 1:
		size /= 1024
		unit += 1
	if unit == 0:
		return f"{n:,} bytes"
	return f"{size:.1f} {units[unit]} ({n:,} bytes)"


def artifact_lock_record(name, path):
	record = {
		"path": os.path.relpath(os.path.abspath(path), WORKSPACE_ROOT).replace("\\", "/"),
		"sha256": sha256_file(path),
		"size": os.path.getsize(path),
	}
	if name == "binary_ninja_hlil":
		record.update({
			"producer": "Binary Ninja HLIL linear-view export",
			"producer_version": "legacy-unrecorded",
			# This 110 MiB legacy linear-view dump predates the structured
			# exporter below.  Claiming that exporter could reproduce it would
			# turn a hash pin into false provenance.
			"recipe": None,
			"recipe_status": "legacy-unrecorded-no-exact-regeneration-recipe",
			"limitations": "semantic hypothesis snapshot; not byte/function ground truth",
		})
	elif name.startswith("binary_ninja"):
		record.update({
			"producer": "Binary Ninja structured snapshot",
			"recipe": "research/tools/re/cpp-replacement-proof/export_binary_ninja_text_coverage.py",
		})
	elif name.startswith("rizin"):
		record["producer"] = "Rizin analysis snapshot"
	elif name == "reconciled_function_index":
		record.update({
			"producer": "cross-analyzer entry reconciler",
			"recipe": "research/tools/re/cpp-replacement-proof/reconcile_function_index.py",
		})
	elif name == "progress_data":
		record.update({
			"producer": "proof-backed byte treemap generator",
			"recipe": "research/tools/re/cpp-replacement-proof/exact_progress_treemap.py",
		})
	elif name == "ui_family_plan":
		record.update({
			"producer": "RTTI UI family planner",
			"recipe": "research/tools/re/cpp-replacement-proof/ui_family_plan.py",
		})
	elif name == "exact_provider_manifest":
		record["producer"] = "C++ replacement proof gate"
	else:
		record["producer"] = "retail PE"
	return record


def validate_evidence_graph(con, args):
	"""Return stale or cross-target evidence relationships.

    Hashing every file independently does not prove the files belong together.
    The derived artifacts therefore carry their input hashes, and this gate
    verifies the complete graph before ``--refresh-lock`` is allowed to bless a
    new snapshot.
    """
	paths = evidence_artifact_paths(con, args)
	issues = []

	def document(name):
		path = paths[name]
		try:
			with open(path, encoding="utf-8") as f:
				return json.load(f)
		except (OSError, json.JSONDecodeError) as exc:
			issues.append(f"{name}: {exc}")
			return {}

	def expect(label, actual, expected):
		if str(actual or "").lower() != str(expected or "").lower():
			issues.append(f"{label}: {actual!r} != {expected!r}")

	for name, path in paths.items():
		if not path or not os.path.isfile(path):
			issues.append(f"{name}: missing {path}")
	if issues:
		return issues

	target_hash = sha256_file(paths["target"])
	expect("target", target_hash, TARGET_SHA256)

	coverage = document("binary_ninja_coverage")
	expect("binary_ninja_coverage.schema", coverage.get("schema"),
		   "sro-binary-ninja-text-coverage-v3")
	expect("binary_ninja_coverage.target", coverage.get("binary_sha256"), target_hash)
	expect("binary_ninja_coverage.spans",
		   coverage.get("function_spans_sha256"),
		   sha256_file(paths["binary_ninja_functions"]))
	expect("binary_ninja_coverage.calls",
		   coverage.get("direct_calls_sha256"),
		   sha256_file(paths["binary_ninja_calls"]))

	rizin_index = document("rizin_function_index")
	expect("rizin_function_index.target", rizin_index.get("target_sha256"), target_hash)
	expect("rizin_function_index.project", rizin_index.get("rizin_project_sha256"),
		   sha256_file(paths["rizin_project"]))

	reconciled = document("reconciled_function_index")
	expect("reconciled_function_index.target", reconciled.get("target_sha256"), target_hash)
	reconciled_sources = reconciled.get("sources") or {}
	for key, source_name in (
		("rizin_index_sha256", "rizin_function_index"),
		("binary_ninja_functions_sha256", "binary_ninja_functions"),
		("binary_ninja_coverage_sha256", "binary_ninja_coverage"),
		("provider_manifest_sha256", "exact_provider_manifest"),
		("rizin_project_sha256", "rizin_project"),
	):
		expect(f"reconciled_function_index.{key}", reconciled_sources.get(key),
			   sha256_file(paths[source_name]))

	progress = document("progress_data")
	expect("progress_data.target", progress.get("targetSha256"), target_hash)
	progress_sources = progress.get("sources") or {}
	for key, source_name in (
		("functionIndexSha256", "reconciled_function_index"),
		("providerManifestSha256", "exact_provider_manifest"),
		("binaryNinjaCoverageSha256", "binary_ninja_coverage"),
		("rizinProjectSha256", "rizin_project"),
	):
		expect(f"progress_data.{key}", progress_sources.get(key),
			   sha256_file(paths[source_name]))
	ledger_path = os.path.abspath(args.ledger or default_sync_path(args.db))
	if os.path.isfile(ledger_path):
		expect("progress_data.revtoolLedgerSha256",
			   progress_sources.get("revtoolLedgerSha256"), sha256_file(ledger_path))

	plan = document("ui_family_plan")
	expect("ui_family_plan.schema", plan.get("schema"), "sro-ui-family-plan-v7")
	expect("ui_family_plan.target", plan.get("targetSha256"), target_hash)
	plan_sources = plan.get("sources") or {}
	for key, source_name in (
		("functionIndexSha256", "reconciled_function_index"),
		("providerManifestSha256", "exact_provider_manifest"),
		("rizinProjectSha256", "rizin_project"),
		("binaryNinjaCallsSha256", "binary_ninja_calls"),
	):
		expect(f"ui_family_plan.{key}", plan_sources.get(key),
			   sha256_file(paths[source_name]))
	if os.path.isfile(ledger_path):
		expect("ui_family_plan.revtoolLedgerSha256",
			   plan_sources.get("revtoolLedgerSha256"), sha256_file(ledger_path))
	return issues


def build_evidence_lock(con, args):
	issues = validate_evidence_graph(con, args)
	if issues:
		raise ValueError("evidence graph is stale or cross-target:\n  " + "\n  ".join(issues))
	artifacts = {}
	for name, path in evidence_artifact_paths(con, args).items():
		if not path or not os.path.isfile(path):
			raise FileNotFoundError(f"required evidence artifact is missing: {name}: {path}")
		artifacts[name] = artifact_lock_record(name, path)
	if artifacts["target"]["sha256"] != TARGET_SHA256:
		raise ValueError("configured target is not the pinned SRO_Client.exe v1.150")
	return {
		"schema": EVIDENCE_LOCK_SCHEMA,
		"target_sha256": TARGET_SHA256,
		"artifacts": artifacts,
		"policy": {
			"bytes": "original PE and exact providers",
			"structure": "PE/RTTI/direct calls, then corroborated analyzers",
			"semantic_names": "provenance-tiered Revtool claims",
			"single_analyzer": "hypothesis only",
		},
	}


def _doctor_report(checks, args):
	"""The doctor verdict both backends print (and --json emits)."""
	result = {"schema": "sro-revtool-doctor-v1", "ok": all(c["ok"] for c in checks),
			  "checks": checks}
	if args.json:
		print(json.dumps(result, indent=2))
	else:
		for row in checks:
			print(f"{'PASS' if row['ok'] else 'FAIL'}  {row['name']}: {row['detail']}")
		print(f"doctor: {'PASS' if result['ok'] else 'FAIL'}")
	return 0 if result["ok"] else 2


def cmd_doctor_rizin(args, con):
	"""The fail-closed audit for a rizin-backed overlay (`--backend rizin`).

	The dump doctor audits the BN evidence lock, the structured BN exports
	and the dump binding - artifacts a contributor machine never has. A
	rizin DB's provenance is instead its recorded artifact rows (the
	target binary, the rizin analysis that built the index) plus the
	project file those facts came from. The checks below audit exactly
	that chain, then the same ledger and conflict checks the dump doctor
	runs. Anything missing FAILs: a doctor that guesses is worse than none.
	"""
	checks = []

	def check(name, ok, detail):
		checks.append({"name": name, "ok": bool(ok), "detail": detail})

	check("schema", meta_get(con, "schema_version") == str(REVTOOL_SCHEMA_VERSION),
		  f"db={meta_get(con, 'schema_version')} expected={REVTOOL_SCHEMA_VERSION}")
	artifact = con.execute(
		"SELECT path,record_count,version FROM artifact_provenance "
		"WHERE name='rizin-analysis'").fetchone()
	version = artifact[2] if artifact and artifact[2] else "unrecorded"
	check("backend", True,
		  f"rizin {version} (facts from the retail binary via rzpipe; no BN dump)")
	if getattr(args, "refresh_lock", False):
		check("evidence-lock", False,
			  "--refresh-lock builds the BN evidence lock; a rizin overlay "
			  "has none - its provenance is the artifact rows above")
	binary = get_binary(con, args)
	if os.path.isfile(binary):
		actual_target = sha256_file(binary)
		check("db-target-binding", meta_get(con, "target_sha256") == actual_target,
			  f"db={meta_get(con, 'target_sha256')} actual={actual_target}")
	else:
		check("target", False, f"missing: {binary}")
	rzdb = getattr(args, "rzdb", None)
	if not rzdb and artifact and artifact[0].endswith(".rzdb"):
		rzdb = artifact[0]
	if not rzdb:
		rzdb = os.path.splitext(os.path.abspath(args.db))[0] + ".rzdb"
	if os.path.isfile(rzdb):
		check("rizin-project", True, f"present: {rzdb}")
	else:
		check("rizin-project", False,
			  f"missing: {rzdb}; rerun `index` (it saves the project) or pass --rzdb")
	actual_functions = con.execute("SELECT COUNT(*) FROM functions").fetchone()[0]
	if artifact:
		check("db-function-index", artifact[1] == actual_functions,
			  f"db={actual_functions} indexed={artifact[1]}")
	else:
		check("db-function-index", False,
			  "no rizin-analysis artifact recorded; run `index`")
	ledger_path = os.path.abspath(getattr(args, "ledger", None) or default_sync_path(args.db))
	if os.path.isfile(ledger_path):
		try:
			diff = sync_diff(con, ledger_path)
			validate_ledger_meta(con, diff["meta"], ledger_path)
			check("ledger-claims", not diff["local_only"] and not diff["remote_only"]
				  and not diff["differing"],
				  f"local-only={len(diff['local_only'])} "
				  f"remote-only={len(diff['remote_only'])} differing={len(diff['differing'])}")
			check("ledger-events", not diff["local_event_only"] and not diff["remote_event_only"],
				  f"local-only={len(diff['local_event_only'])} "
				  f"remote-only={len(diff['remote_event_only'])}")
		except (OSError, ValueError) as exc:
			check("ledger", False, str(exc))
	elif db_claims(con):
		check("ledger", False,
			  f"missing: {ledger_path}; claims not exported - run `sync export`")
	else:
		check("ledger", True,
			  "no ledger yet and no claims to audit; `sync export` creates it")
	conflicts = con.execute(
		"SELECT COUNT(*) FROM sync_conflicts WHERE resolved=0").fetchone()[0]
	check("sync-conflicts", conflicts == 0, f"unresolved={conflicts}")
	return _doctor_report(checks, args)


def cmd_doctor(args, con):
	if meta_get(con, "backend") == "rizin":
		return cmd_doctor_rizin(args, con)
	lock_path = os.path.abspath(args.lock or DEFAULT_EVIDENCE_LOCK)
	if args.refresh_lock:
		lock = build_evidence_lock(con, args)
		with open(lock_path, "w", encoding="utf-8") as f:
			json.dump(lock, f, indent=2, ensure_ascii=False)
			f.write("\n")
		print(f"refreshed evidence lock: {lock_path}")

	checks = []
	def check(name, ok, detail):
		checks.append({"name": name, "ok": bool(ok), "detail": detail})

	check("schema", meta_get(con, "schema_version") == str(REVTOOL_SCHEMA_VERSION),
		  f"db={meta_get(con, 'schema_version')} expected={REVTOOL_SCHEMA_VERSION}")
	if not os.path.isfile(lock_path):
		check("evidence-lock", False, f"missing: {lock_path}; run doctor --refresh-lock")
		lock = None
	else:
		try:
			lock = json.load(open(lock_path, encoding="utf-8"))
			check("evidence-lock-schema", lock.get("schema") == EVIDENCE_LOCK_SCHEMA,
				  str(lock.get("schema")))
		except (OSError, json.JSONDecodeError) as exc:
			check("evidence-lock", False, str(exc))
			lock = None
	if lock:
		for name, record in lock.get("artifacts", {}).items():
			path = os.path.join(WORKSPACE_ROOT, record.get("path", ""))
			if not os.path.isfile(path):
				check(f"artifact:{name}", False, f"missing: {path}")
				continue
			actual = sha256_file(path)
			check(f"artifact:{name}", actual == record.get("sha256"),
				  f"sha256={actual} locked={record.get('sha256')}")
	graph_issues = validate_evidence_graph(con, args)
	check("evidence-graph", not graph_issues,
		  "all derived inputs match" if not graph_issues else "; ".join(graph_issues))
	binary = get_binary(con, args)
	if os.path.isfile(binary):
		actual_target = sha256_file(binary)
		check("target-hash", actual_target == TARGET_SHA256,
			  f"sha256={actual_target}")
		check("db-target-binding", meta_get(con, "target_sha256") == actual_target,
			  f"db={meta_get(con, 'target_sha256')} actual={actual_target}")
	else:
		check("target", False, f"missing: {binary}")

	# A lock over two unrelated files is not enough: validate that the
	# structured Binary Ninja coverage and span CSV are a self-consistent pair
	# exported from this exact PE.  This catches a stale CSV paired with a fresh
	# JSON file, a snapshot from another Binary Ninja database, and newline or
	# serialization drift that invalidates the embedded hash.
	evidence_paths = evidence_artifact_paths(con, args)
	coverage_path = evidence_paths["binary_ninja_coverage"]
	spans_path = evidence_paths["binary_ninja_functions"]
	calls_path = evidence_paths["binary_ninja_calls"]
	if (os.path.isfile(coverage_path) and os.path.isfile(spans_path)
			and os.path.isfile(calls_path) and os.path.isfile(binary)):
		try:
			with open(coverage_path, encoding="utf-8") as f:
				coverage = json.load(f)
			check("binary-ninja-schema",
				  coverage.get("schema") == "sro-binary-ninja-text-coverage-v3"
				  and coverage.get("exporter_version") == 3,
				  f"schema={coverage.get('schema')} exporter={coverage.get('exporter_version')}")
			check("binary-ninja-target-binding",
				  coverage.get("binary_sha256") == actual_target
				  and coverage.get("binary_size") == os.path.getsize(binary),
				  f"sha256={coverage.get('binary_sha256')} size={coverage.get('binary_size')}")
			spans_hash = sha256_file(spans_path)
			check("binary-ninja-span-binding",
				  coverage.get("function_spans_sha256") == spans_hash,
				  f"csv={spans_hash} embedded={coverage.get('function_spans_sha256')}")
			calls_hash = sha256_file(calls_path)
			check("binary-ninja-call-binding",
				  coverage.get("direct_calls_sha256") == calls_hash,
				  f"csv={calls_hash} embedded={coverage.get('direct_calls_sha256')}")
			with open(spans_path, newline="", encoding="utf-8") as f:
				span_count = sum(1 for _ in csv.DictReader(f))
			check("binary-ninja-function-count",
				  coverage.get("function_count") == span_count,
				  f"csv={span_count} metadata={coverage.get('function_count')}")
			with open(calls_path, newline="", encoding="utf-8") as f:
				call_rows = list(csv.DictReader(f))
			direct_edges = sum(1 for row in call_rows if row.get("target_address"))
			unresolved_sites = sum(1 for row in call_rows if not row.get("target_address"))
			check("binary-ninja-call-count",
				  coverage.get("direct_call_edge_count") == direct_edges
				  and coverage.get("unresolved_call_site_count") == unresolved_sites,
				  f"direct={direct_edges}/{coverage.get('direct_call_edge_count')} "
				  f"unresolved={unresolved_sites}/{coverage.get('unresolved_call_site_count')}")

			pe_image = PEImage(binary)
			text_section = next(
				(row for row in pe_image.sections if row[0] == ".text"), None)
			if text_section is None:
				raise ValueError("target PE has no .text section")
			_name, text_rva, text_virtual_size, text_raw_offset, text_raw_size = text_section
			text_bytes = pe_image.data[text_raw_offset:text_raw_offset + text_raw_size]
			expected_text_hash = hashlib.sha256(text_bytes).hexdigest()
			text_metadata_ok = (
				coverage.get("text_va") == pe_image.image_base + text_rva
				and coverage.get("text_virtual_size") == text_virtual_size
				and coverage.get("text_raw_offset") == text_raw_offset
				and coverage.get("text_size") == text_raw_size
				and coverage.get("text_sha256") == expected_text_hash
			)
			check("binary-ninja-text-binding", text_metadata_ok,
				  f"sha256={coverage.get('text_sha256')} expected={expected_text_hash}")
		except (OSError, ValueError, KeyError, json.JSONDecodeError) as exc:
			check("binary-ninja-structured-evidence", False, str(exc))
	else:
		check("binary-ninja-structured-evidence", False,
			  f"missing coverage={coverage_path} spans={spans_path} calls={calls_path} "
			  f"or target={binary}")
	dump = get_dump(con, args)
	if dump and os.path.isfile(dump):
		actual_dump = sha256_file(dump)
		check("db-dump-binding", meta_get(con, "dump_sha256") == actual_dump,
			  f"db={meta_get(con, 'dump_sha256')} actual={actual_dump}")
		expected_functions = meta_get(con, "dump_function_count")
		actual_functions = con.execute("SELECT COUNT(*) FROM functions").fetchone()[0]
		check("db-function-index", expected_functions == str(actual_functions),
			  f"db={actual_functions} indexed={expected_functions}")
	else:
		check("dump", False, f"missing: {dump}")

	ledger_path = os.path.abspath(args.ledger or default_sync_path(args.db))
	if os.path.isfile(ledger_path):
		try:
			diff = sync_diff(con, ledger_path)
			validate_ledger_meta(con, diff["meta"], ledger_path)
			check("ledger-claims", not diff["local_only"] and not diff["remote_only"]
				  and not diff["differing"],
				  f"local-only={len(diff['local_only'])} "
				  f"remote-only={len(diff['remote_only'])} differing={len(diff['differing'])}")
			check("ledger-events", not diff["local_event_only"] and not diff["remote_event_only"],
				  f"local-only={len(diff['local_event_only'])} "
				  f"remote-only={len(diff['remote_event_only'])}")
		except (OSError, ValueError) as exc:
			check("ledger", False, str(exc))
	else:
		check("ledger", False, f"missing: {ledger_path}")
	conflicts = con.execute(
		"SELECT COUNT(*) FROM sync_conflicts WHERE resolved=0").fetchone()[0]
	check("sync-conflicts", conflicts == 0, f"unresolved={conflicts}")

	return _doctor_report(checks, args)


def cmd_dossier(args, con):
	script = os.path.join(HERE, "evidence_dossier.py")
	command = [sys.executable, script, args.target, "--db", os.path.abspath(args.db),
			   "--dump", os.path.abspath(get_dump(con, args)),
			   "--binary", os.path.abspath(get_binary(con, args))]
	if args.out:
		command.extend(["--out", os.path.abspath(args.out)])
	if args.json:
		command.append("--json")
	if args.allow_stale:
		command.append("--allow-stale")
	return subprocess.run(command, cwd=REBUILD_ROOT).returncode


def cmd_status(args, con):
	nf = con.execute("SELECT COUNT(*) FROM functions").fetchone()[0]
	ng = con.execute("SELECT COUNT(*) FROM globals").fetchone()[0]
	nl = con.execute("SELECT COUNT(*) FROM locals").fetchone()[0]
	nt = con.execute("SELECT COUNT(*) FROM types").fetchone()[0]
	nfld = con.execute("SELECT COUNT(*) FROM fields").fetchone()[0]
	nb = con.execute("SELECT COUNT(*) FROM bindings").fetchone()[0]
	nn = con.execute("SELECT COUNT(*) FROM notes").fetchone()[0]
	dump = get_dump(con, args)
	# DB state: revtool.db is per-machine (gitignored, binary); the function index
	# inside it is rebuilt with `index`, the overlay work syncs via the JSONL. Report
	# whether it exists and its size so a fresh clone can see it must be (re)built.
	db_present = os.path.exists(args.db)
	db_size = os.path.getsize(args.db) if db_present else 0
	print(f"db:    {args.db}  ({'present, ' + _human_bytes(db_size) if db_present else 'MISSING - run `revtool.py index`'})")
	# Dump state: size is a validation target for a regenerated export (see
	# revtool_README.md "Provisioning"); a present-but-truncated dump is a broken
	# export, not "no callers", so surface the byte size next to present/absent.
	if os.path.exists(dump):
		dump_size = os.path.getsize(dump)
		dump_state = "present, " + _human_bytes(dump_size) if dump_size else "present but EMPTY (0 bytes) - broken export"
	else:
		dump_state = "MISSING - see revtool_README.md Provisioning"
	print(f"dump:  {dump}  ({dump_state})")
	print(f"functions indexed: {nf}")
	print(f"global renames:    {ng}")
	print(f"local renames:     {nl}")
	print(f"types / fields:    {nt} / {nfld}")
	print(f"bindings:          {nb}")
	print(f"notes:             {nn}")
	# confidence breakdown across renameable claims
	print("confidence (global+local+field):")
	counts = {t: 0 for t in TIERS}
	for table in ("globals", "locals", "fields"):
		for tier, c in con.execute(f"SELECT tier,COUNT(*) FROM {table} GROUP BY tier"):
			counts[tier if tier in counts else "guess"] = counts.get(tier, 0) + c
	for t in TIERS:
		print(f"  {t:<10} {counts.get(t, 0)}")
	ndisp = sum(con.execute(f"SELECT COUNT(*) FROM {t} WHERE disputed=1").fetchone()[0]
				for t in ("globals", "locals", "fields"))
	print(f"  DISPUTED   {ndisp}")
	# assertions + dynamic reconciliation
	na = con.execute("SELECT COUNT(*) FROM assertions").fetchone()[0]
	nobs = con.execute("SELECT COUNT(*) FROM observations").fetchone()[0]
	print(f"assertions:        {na}  (observations: {nobs})")
	if na:
		print("  status:")
		for st in ASSERT_STATUS:
			c = con.execute("SELECT COUNT(*) FROM assertions WHERE status=?", (st,)).fetchone()[0]
			print(f"    {st:<10} {c}")
		nweak = con.execute(
			"SELECT COUNT(*) FROM assertions WHERE basis IN ('absence','inferred') "
			"AND status!='confirmed'").fetchone()[0]
		print(f"  unconfirmed weak-basis (run `review --needs-dynamic`): {nweak}")
	# investigation ledger / enforcement
	ninv = con.execute(
		"SELECT COUNT(*) FROM access_log WHERE kind IN ('viewed','xref')").fetchone()[0]
	author = get_author(args)
	my_debt = compute_debt(con, author=author)
	all_debt = compute_debt(con)
	limit = debt_limit(con)
	print(f"investigated (view/xref): {ninv}")
	print(f"  YOUR debt ({author}): {len(my_debt)}/{limit}   (this is what gates you)")
	print(f"  ALL authors' debt: {len(all_debt)}   (revtool review --debt)")
	print(f"strict enforcement: {'on' if strict_enabled(args, con) else 'off'}  "
		  f"(exploration budget {limit} PER author; config debt_limit N)")
	return 0


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #
# =========================================================================== #
# Codebase-mapping formats (static analyzers + dynamic-trace ingest + C export)
#
# Seven capabilities that turn the immutable dump + the PE + injected runtime
# traces into structured overlay facts, then re-render typed C. Each is a thin
# producer of the SAME overlay tables the manual commands write, so provenance,
# tiers, and `sync` apply uniformly. See revtool_README.md "Codebase mapping".
#   1. asserts        static: scrape ASSERT(file,"expr") -> member/method names
#   2. classes        static: RTTI/vftable -> class hierarchy + vtable slots
#   3. ingest-calls   dynamic: trace call edges + observed `this` vtable -> bind
#   4. ingest-allocs  dynamic: alloc size + ctor vtable -> sizeof(class)
#   5. ingest-layout  dynamic: guard-page field accesses -> struct fields
#   6. infer-types    dynamic: value samples -> field/global type + assertion
#   7. reconstruct    revtool-native typed C (no Ghidra): struct decls + body
#                     with `*(T*)((char*)v+0xNN)` rewritten to `v->field`
# =========================================================================== #

# An ASSERT macro compiles to `helper(cond, line, "file.cpp", "stringified-cond")`.
# We do not need the helper name: the adjacency of a source-file string literal and
# a following string literal is the reliable tell, and \s* spans the newline MSVC
# inserts when the condition string wraps. Group1=source file, group2=the expression.
def cmd_classes(args, con):
	"""FORMAT 2 - structurally bounded PE RTTI/vftable class hierarchy."""
	pe = open_pe(con, args)
	if pe is None:
		return 1
	try:
		idx = _best_rtti_index(args, con, pe)
	except (OSError, ValueError, json.JSONDecodeError) as exc:
		print(f"error: structural RTTI inventory unavailable: {exc}", file=sys.stderr)
		return 1
	classes = idx["classes"]

	if args.name:
		cls = args.name
		if cls not in classes:
			# tolerate an applied/dump alias by case-insensitive match
			hit = [c for c in classes if c.lower() == cls.lower()]
			if not hit:
				print(f"error: class not found in RTTI: {cls}", file=sys.stderr)
				return 1
			cls = hit[0]
		anc = idx["anc"].get(cls, set())
		des = idx["desc"].get(cls, set())
		direct_sub = sorted(
			c for c in des if cls in idx.get("direct_bases", {}).get(c, ())
		)
		print(f"class {cls}")
		direct = sorted(idx.get("direct_bases", {}).get(cls, ()))
		print(f"  direct base(s): {', '.join(direct) or '(none - root)'}")
		if anc:
			print(f"  all ancestors: {', '.join(sorted(anc))}")
		print(f"  direct subclasses: {', '.join(direct_sub) or '(none)'}")
		if des:
			print(f"  all descendants ({len(des)}): {', '.join(sorted(des))}")
		vaddr = idx["vtable_addr"].get(cls)
		if vaddr is None:
			print("  vtable: (no primary vftable found)")
			return 0
		print(f"  vtable @ 0x{vaddr:08x}")
		record = idx.get("vtable_records", {}).get(vaddr)
		count = len(record.targets) if record is not None else 0
		print(f"  source: {idx['source']}")
		print(f"  bounded slots: {count}")
		printed = 0
		for i in range(min(args.count, count)):
			target = pe.read_u32(vaddr + i * 4)
			if target is None or not pe.in_image(target):
				break
			name, tag = resolve_addr(con, target)
			print(f"    +0x{i*4:<4x} 0x{target:08x}  {name}  {tag}")
			printed += 1
		if not printed:
			print("    (no readable slots)")
		return 0

	if args.base:
		base = args.base
		subs = idx["desc"].get(base, set())
		if not subs and base not in classes:
			print(f"error: base class not found in RTTI: {base}", file=sys.stderr)
			return 1
		print(f"subclasses of {base}: {len(subs)}")
		for c in sorted(subs):
			direct = (
				"  (direct)"
				if base in idx.get("direct_bases", {}).get(c, ())
				else ""
			)
			va = idx["vtable_addr"].get(c)
			vs = f"  vtable 0x{va:08x}" if va else ""
			print(f"  {c}{vs}{direct}")
		return 0

	roots = sorted(c for c in classes if not idx.get("direct_bases", {}).get(c))
	print(f"classes: {len(classes)} RTTI classes, {len(idx['vtable_addr'])} with primary vtables, "
		  f"{len(idx['direct_bases'])} with one or more known direct bases.")
	print(f"source: {idx['source']}")
	if args.list:
		for c in sorted(classes):
			va = idx["vtable_addr"].get(c)
			b = idx["direct_base"].get(c)
			extra = []
			if b:
				extra.append("base=" + b)
			if va:
				extra.append(f"vtable=0x{va:08x}")
			print(f"  {c}" + ("  [" + "; ".join(extra) + "]" if extra else ""))
	else:
		print(f"\nroot classes (no base): {len(roots)}")
		for c in roots[:args.limit]:
			nd = len(idx["desc"].get(c, set()))
			print(f"  {c}" + (f"  (+{nd} descendants)" if nd else ""))
		print("\nnext: `classes <Name>` | `classes --base CObj` | `classes --list`")
	return 0


# --------------------------------------------------------------------------- #
# Dynamic-trace ingest (formats 3-6). All read JSONL emitted by the native-trace
# DLL (one JSON object per line, `kind` field selects the record).
# --------------------------------------------------------------------------- #
