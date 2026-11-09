"""
===========================================================================
revtool_ingest.py - ingest/sync/export and the type-inference reconstructions

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
import json
import re
import struct

from revtool_asserts import _TRACE_FOGRS_RE, _TRACE_GLOBAL_RE
from revtool_claims import reconcile_assertions
from revtool_dump import get_dump, read_func_lines, resolve_func
from revtool_overlay import apply_renames, cmd_view, load_bindings
from revtool_overlay import load_fields, load_globals, load_locals
from revtool_paths import LEDGER_SCHEMA, REVTOOL_SCHEMA_VERSION
from revtool_paths import TARGET_SHA256
from revtool_pe import _best_rtti_index, resolve_addr
from revtool_store import _as_int, _read_jsonl, get_author, meta_get
from revtool_store import now_iso, record_history, sha256_file, stable_json

def cmd_ingest(args, con):
	"""Bulk-observe from a native-trace log, reconciling against assertions.

    Parses two line shapes:
      WORLD_RENDER_PASS ... data_f0673f(near)=1692.86[...] data_f06743(far)=2500[...]
      FOG_RS FOGSTART state=0x24 value=0x.. int=.. float=1692.86 caller=..
    Distinct (subject,value) pairs become observations; each reconciles assertions.
    """
	if not os.path.exists(args.log):
		print(f"error: log not found: {args.log}", file=sys.stderr)
		return 1
	author = get_author(args)
	seen = {}  # subject -> set of distinct values (keep first line for source)
	src_line = {}
	with open(args.log, encoding="utf-8", errors="replace") as f:
		for ln_no, line in enumerate(f, 1):
			for m in _TRACE_GLOBAL_RE.finditer(line):
				subj, val = m.group(1), m.group(2)
				seen.setdefault(subj, set()).add(val)
				src_line.setdefault((subj, val), f"{os.path.basename(args.log)}:{ln_no}")
			fm = _TRACE_FOGRS_RE.search(line)
			if fm:
				state, fval = fm.group(1), fm.group(3)
				if fval.lower() in ("-nan", "nan", "inf", "-inf"):
					continue  # packed-color states are not floats; skip the bogus reinterpret
				subj = f"D3DRS_{state}"
				seen.setdefault(subj, set()).add(fval)
				src_line.setdefault((subj, fval), f"{os.path.basename(args.log)}:{ln_no}")

	n_obs = n_conf = n_ref = 0
	for subj, vals in sorted(seen.items()):
		for val in sorted(vals, key=lambda v: float(v) if _isnum(v) else 0.0):
			source = src_line.get((subj, val), os.path.basename(args.log))
			con.execute(
				"INSERT INTO observations(subject,value,source,author,ts) VALUES(?,?,?,?,?)",
				(subj, val, source, author, now_iso()))
			n_obs += 1
			for r in reconcile_assertions(con, subj, val, source, author):
				if r["verdict"] == "confirmed":
					n_conf += 1
				elif r["verdict"] == "refuted":
					n_ref += 1
					print(f"  REFUTED assert #{r['id']} by {subj}={val}: {r['claim']}")
	con.commit()
	distinct = sum(len(v) for v in seen.values())
	print(f"ingest {args.log}: {len(seen)} subjects, {distinct} distinct values, "
		  f"{n_obs} observations.")
	print(f"  assertions: {n_conf} confirmed, {n_ref} refuted.")
	if n_ref:
		print("  -> refuted assertions are now DISPUTED; run `revtool review --disputed`.")
	return 0


def _isnum(s):
	try:
		float(s)
		return True
	except (TypeError, ValueError):
		return False


# --------------------------------------------------------------------------- #
# sync  (git-mergeable JSONL export/import for multiple engineers)
# --------------------------------------------------------------------------- #
SYNC_TABLES = {
	"globals": ["orig_name", "new_name", "kind", "note", "tier", "author",
				"evidence", "updated_at", "disputed", "dispute_reason",
				"basis", "method", "blindspots"],
	"locals": ["func_addr", "orig_name", "new_name", "vtype", "note", "tier",
			   "author", "evidence", "updated_at", "disputed", "dispute_reason",
			   "basis", "method", "blindspots"],
	"types": ["name", "note", "tier", "author", "evidence", "updated_at",
			  "disputed", "dispute_reason", "basis", "method", "blindspots"],
	"fields": ["type_name", "offset", "new_name", "ftype", "note", "tier",
			   "author", "evidence", "updated_at", "disputed", "dispute_reason",
			   "basis", "method", "blindspots"],
	"bindings": ["func_addr", "var_name", "type_name", "tier", "author", "updated_at"],
	"notes": ["target", "body", "author", "tier", "updated_at"],
	"assertions": ["subject", "claim", "predicate", "basis", "method", "blindspots",
				   "tier", "status", "expected", "observed", "source", "author",
				   "evidence", "updated_at", "disputed", "dispute_reason"],
	"observations": ["subject", "value", "source", "author", "ts"],
}
SYNC_KEYS = {
	"globals": ["orig_name"],
	"locals": ["func_addr", "orig_name"],
	"types": ["name"],
	"fields": ["type_name", "offset"],
	"bindings": ["func_addr", "var_name"],
	"notes": ["target", "body"],
	"assertions": ["subject", "claim"],
	"observations": ["subject", "value", "source", "author", "ts"],
}


def default_sync_path(db_path):
	"""Keep each overlay's collaboration ledger beside and named after it."""
	root, _ = os.path.splitext(os.path.abspath(db_path))
	return root + "_symbols.jsonl"


def claim_key(table, rec):
	return tuple(rec.get(k) for k in SYNC_KEYS[table])


def read_ledger(path):
	meta = None
	claims = {}
	events = {}
	if not os.path.exists(path):
		return meta, claims, events
	with open(path, encoding="utf-8") as f:
		for line_no, ln in enumerate(f, 1):
			if not ln.strip():
				continue
			try:
				rec = json.loads(ln)
			except json.JSONDecodeError as exc:
				raise ValueError(f"{path}:{line_no}: invalid JSON: {exc}") from exc
			table = rec.get("_t")
			if table == "_meta":
				if meta is not None:
					raise ValueError(f"{path}:{line_no}: duplicate ledger metadata")
				meta = rec
			elif table == "_event":
				event_id = rec.get("event_id")
				if not event_id:
					raise ValueError(f"{path}:{line_no}: event has no event_id")
				events[event_id] = rec
			elif table in SYNC_TABLES:
				# v1 did not serialize provenance columns for `types`.
				# Rehydrate the database defaults so upgrading a legacy ledger
				# does not manufacture 17 false conflicts (NULL versus the
				# migrated defaults `guess` / 0).
				if "tier" not in rec:
					rec["tier"] = "guess"
				if "disputed" in SYNC_TABLES[table] and "disputed" not in rec:
					rec["disputed"] = 0
				for column in SYNC_TABLES[table]:
					rec.setdefault(column, None)
				key = (table, claim_key(table, rec))
				if key in claims:
					# Legacy ledgers occasionally contain the same serialized
					# row twice.  That carries no competing information and is
					# safe to collapse.  A second value for the same semantic
					# key is still an error: choosing one would be an implicit
					# last-writer-wins merge.
					if stable_json(claims[key]) == stable_json(rec):
						continue
					raise ValueError(f"{path}:{line_no}: conflicting duplicate claim {key}")
				claims[key] = rec
	return meta, claims, events


def db_claims(con):
	result = {}
	for table, cols in SYNC_TABLES.items():
		for row in con.execute(f"SELECT {','.join(cols)} FROM {table}"):
			rec = {"_t": table}
			rec.update(dict(zip(cols, row)))
			result[(table, claim_key(table, rec))] = rec
	return result


def db_events(con):
	cols = ["event_id", "ts", "author", "kind", "ckey", "action", "old",
			"new", "reason", "target_sha256", "dump_sha256", "tool_version"]
	result = {}
	for row in con.execute(f"SELECT {','.join(cols)} FROM history WHERE event_id IS NOT NULL"):
		rec = {"_t": "_event"}
		rec.update(dict(zip(cols, row)))
		result[rec["event_id"]] = rec
	return result


def ledger_meta(con):
	return {
		"_t": "_meta",
		"schema": LEDGER_SCHEMA,
		"target_sha256": meta_get(con, "target_sha256") or TARGET_SHA256,
		"dump_sha256": meta_get(con, "dump_sha256"),
		"revtool_schema_version": REVTOOL_SCHEMA_VERSION,
		"projection": "claims-plus-append-only-events",
	}


def sync_diff(con, path):
	remote_meta, remote, remote_events = read_ledger(path)
	local = db_claims(con)
	local_events = db_events(con)
	local_only = sorted(set(local) - set(remote), key=str)
	remote_only = sorted(set(remote) - set(local), key=str)
	differing = sorted(
		(key for key in set(local) & set(remote)
		 if stable_json(local[key]) != stable_json(remote[key])), key=str)
	return {
		"meta": remote_meta,
		"local": local,
		"remote": remote,
		"local_events": local_events,
		"remote_events": remote_events,
		"local_only": local_only,
		"remote_only": remote_only,
		"differing": differing,
		"local_event_only": sorted(set(local_events) - set(remote_events)),
		"remote_event_only": sorted(set(remote_events) - set(local_events)),
	}


def validate_ledger_meta(con, meta, path):
	if meta is None:
		return  # legacy v1 snapshot; next export upgrades it.
	if meta.get("schema") != LEDGER_SCHEMA:
		raise ValueError(f"{path}: unsupported ledger schema {meta.get('schema')!r}")
	target = meta.get("target_sha256")
	local_target = meta_get(con, "target_sha256") or TARGET_SHA256
	if target != local_target:
		raise ValueError(f"{path}: ledger target hash differs from this projection")
	dump_hash = meta.get("dump_sha256")
	local_dump = meta_get(con, "dump_sha256")
	if dump_hash and local_dump and dump_hash != local_dump:
		raise ValueError(f"{path}: ledger Binary Ninja dump hash differs from this projection")


def cmd_sync(args, con):
	if args.action in ("status", "conflicts"):
		path = args.path or default_sync_path(args.db)
		diff = sync_diff(con, path)
		unresolved = con.execute(
			"SELECT id,table_name,claim_key,local_json,remote_json,detected_at "
			"FROM sync_conflicts WHERE resolved=0 ORDER BY id").fetchall()
		print(f"ledger: {path}  ({'present' if os.path.exists(path) else 'MISSING'})")
		print(f"claims: local-only={len(diff['local_only'])} "
			  f"remote-only={len(diff['remote_only'])} differing={len(diff['differing'])}")
		print(f"events: local-only={len(diff['local_event_only'])} "
			  f"remote-only={len(diff['remote_event_only'])}")
		print(f"unresolved conflicts: {len(unresolved)}")
		if args.action == "conflicts":
			for row in unresolved:
				print(f"  #{row[0]} {row[1]} {row[2]} detected={row[5]}")
				print(f"     local : {row[3]}")
				print(f"     remote: {row[4]}")
		return 1 if unresolved or diff["remote_only"] or diff["differing"] else 0

	if args.action == "resolve":
		if not args.path or not str(args.path).isdigit() or args.take not in ("local", "remote"):
			print("usage: revtool sync resolve <conflict-id> --take local|remote",
				  file=sys.stderr)
			return 1
		conflict_id = int(args.path)
		row = con.execute(
			"SELECT table_name,local_json,remote_json FROM sync_conflicts "
			"WHERE id=? AND resolved=0", (conflict_id,)).fetchone()
		if not row:
			print(f"error: unresolved conflict #{conflict_id} not found", file=sys.stderr)
			return 1
		table, local_json, remote_json = row
		if args.take == "remote":
			rec = json.loads(remote_json)
			keys = SYNC_KEYS[table]
			where = " AND ".join(f"{k}=?" for k in keys)
			kv = [rec.get(k) for k in keys]
			cols = [c for c in SYNC_TABLES[table] if c in rec]
			con.execute(f"UPDATE {table} SET {','.join(f'{c}=?' for c in cols)} "
						f"WHERE {where}", [rec[c] for c in cols] + kv)
		con.execute("UPDATE sync_conflicts SET resolved=1 WHERE id=?", (conflict_id,))
		record_history(con, "sync", f"sync-conflict:{conflict_id}",
					   f"resolve-{args.take}", local_json, remote_json,
					   get_author(args))
		con.commit()
		print(f"resolved conflict #{conflict_id}: kept {args.take}")
		return 0

	if args.action == "export":
		path = args.path or default_sync_path(args.db)
		diff = sync_diff(con, path)
		validate_ledger_meta(con, diff["meta"], path)
		# Export may add local work, but it may never erase or silently rewrite
		# teammate work. Import/resolve first when the existing ledger differs.
		unresolved_differences = []
		for key in diff["differing"]:
			table = key[0]
			local_json = stable_json(diff["local"][key])
			remote_json = stable_json(diff["remote"][key])
			claim_json = stable_json(key[1])
			accepted = con.execute(
				"SELECT 1 FROM sync_conflicts WHERE table_name=? AND claim_key=? "
				"AND local_json=? AND remote_json=? AND resolved=1",
				(table, claim_json, local_json, remote_json)).fetchone()
			if not accepted:
				unresolved_differences.append(key)
		if diff["remote_only"] or unresolved_differences or diff["remote_event_only"]:
			print("error: tracked ledger contains work not present identically in this DB; "
				  "run `sync import` and resolve conflicts before export.", file=sys.stderr)
			print(f"  remote-only claims={len(diff['remote_only'])} "
				  f"differing claims={len(unresolved_differences)} "
				  f"remote-only events={len(diff['remote_event_only'])}", file=sys.stderr)
			return 2
		records = [ledger_meta(con)]
		records.extend(diff["local"][key] for key in sorted(diff["local"], key=str))
		records.extend(diff["local_events"][key] for key in sorted(diff["local_events"]))
		lines = [stable_json(rec) for rec in records]
		with open(path, "w", encoding="utf-8") as f:
			f.write("\n".join(lines) + ("\n" if lines else ""))
		digest = sha256_file(path)
		con.execute("INSERT OR REPLACE INTO sync_state(path,sha256,imported_at) VALUES(?,?,?)",
					(os.path.abspath(path), digest, now_iso()))
		con.commit()
		print(f"exported {len(diff['local'])} claims + {len(diff['local_events'])} "
			  f"append-only events -> {path}")
		print(f"  ledger sha256={digest}")
		return 0

	if args.action == "import":
		path = args.path or default_sync_path(args.db)
		if not os.path.exists(path):
			print(f"error: not found: {path}", file=sys.stderr)
			return 1
		meta, remote, events = read_ledger(path)
		validate_ledger_meta(con, meta, path)
		local = db_claims(con)
		added = skipped = conflicts = event_added = 0
		for key, rec in remote.items():
			table, key_values = key
			existing = local.get(key)
			if existing is not None:
				if stable_json(existing) == stable_json(rec):
					skipped += 1
					continue
				local_json, remote_json = stable_json(existing), stable_json(rec)
				con.execute(
					"INSERT OR IGNORE INTO sync_conflicts"
					"(table_name,claim_key,local_json,remote_json,detected_at) VALUES(?,?,?,?,?)",
					(table, stable_json(key_values), local_json, remote_json, now_iso()))
				conflicts += 1
				continue
			cols = [c for c in SYNC_TABLES[table] if c in rec]
			con.execute(
				f"INSERT INTO {table}({','.join(cols)}) VALUES({','.join('?' for _ in cols)})",
				[rec[c] for c in cols])
			added += 1
		event_cols = ["event_id", "ts", "author", "kind", "ckey", "action", "old",
					  "new", "reason", "target_sha256", "dump_sha256", "tool_version"]
		for event_id, rec in events.items():
			before = con.total_changes
			con.execute(
				f"INSERT OR IGNORE INTO history({','.join(event_cols)}) "
				f"VALUES({','.join('?' for _ in event_cols)})",
				[rec.get(c) for c in event_cols])
			event_added += con.total_changes - before
		digest = sha256_file(path)
		if conflicts == 0:
			con.execute("INSERT OR REPLACE INTO sync_state(path,sha256,imported_at) VALUES(?,?,?)",
						(os.path.abspath(path), digest, now_iso()))
		con.commit()
		print(f"import: {added} claims added, {event_added} events added, "
			  f"{skipped} unchanged, {conflicts} CONFLICTS.")
		if conflicts:
			print("  No conflicting row was overwritten. Inspect sync_conflicts and resolve "
				  "the claim explicitly before export.", file=sys.stderr)
			return 2
		return 0
	return 0


# --------------------------------------------------------------------------- #
# export  (seed a verified_*/notes_* style doc from the overlay)
# --------------------------------------------------------------------------- #
def cmd_export(args, con):
	dump = get_dump(con, args)
	row = resolve_func(con, args.target)
	if not row:
		print(f"error: function not found: {args.target}", file=sys.stderr)
		return 1
	addr, name, sig, sb, eb, sl, el = row
	globals_map = load_globals(con)
	g_self = globals_map.get(name)
	locals_map = load_locals(con, addr)
	bindings = load_bindings(con, addr)

	print("=" * 78)
	print(f"{name}" + (f" -> {g_self[0]}" if g_self else ""))
	print(f"@ 0x{addr}   lines {sl}-{el}")
	print("=" * 78)
	print(f"\nSIGNATURE\n  {sig.strip()}")
	if g_self and g_self[2]:
		print(f"\nROLE\n  {g_self[2]}")
	if bindings:
		print("\nBINDINGS")
		for v, t in sorted(bindings.items()):
			print(f"  {v} : {t}*")
			for off, nm, ft, note, tier, disp, reason in con.execute(
				"SELECT offset,new_name,ftype,note,tier,disputed,dispute_reason "
				"FROM fields WHERE type_name=? ORDER BY offset", (t,)):
				mark = f"  [!DISPUTED: {reason}]" if disp else f"  [{tier}]"
				print(f"    +0x{off:x}  {nm}{'  : ' + ft if ft else ''}{mark}"
					  f"{'  // ' + note if note else ''}")
	if locals_map:
		print("\nLOCALS")
		for orig, vals in sorted(locals_map.items()):
			new, vt, note, tier, disp, reason = vals
			mark = f"  [!DISPUTED: {reason}]" if disp else f"  [{tier}]"
			print(f"  {orig} -> {new}{'  : ' + vt if vt else ''}{mark}"
				  f"{'  // ' + note if note else ''}")
	applied = g_self[0] if g_self else None
	notes = con.execute(
		"SELECT target,body FROM notes "
		"WHERE target LIKE ? OR target=? OR target LIKE ? OR target=? ORDER BY target",
		(name + "@%", name, (applied or name) + "@%", applied or name)).fetchall()
	if notes:
		print("\nNOTES")
		for target, body in notes:
			print(f"  {target}: {body}")
	print("\n" + "-" * 78)
	print("Annotated body (revtool view):")
	print("-" * 78)
	# reuse view renderer
	class _A:
		pass
	a = _A()
	a.target = name
	a.raw = False
	a.no_apply = False
	a.no_legend = True
	a.dump = dump
	cmd_view(a, con)
	return 0


# --------------------------------------------------------------------------- #
# helpers
# --------------------------------------------------------------------------- #
def cmd_ingest_calls(args, con):
	"""FORMAT 3 - dynamic call graph + observed-`this` type binding.

    JSONL records: {"kind":"call","caller":"0x..","callee":"0x..","this_vtable":"0x.."?}
    Resolves caller/callee to functions; counts edges; and (the payload static cannot give)
    when a record carries the live `this` vtable, maps it to a class and BINDS the callee's
    arg1 to that class - resolving indirect/virtual dispatch from ground truth.
    """
	records = _read_jsonl(args.log)
	idx = _best_rtti_index(args, con) if any(
		r.get("this_vtable") for r in records
	) else {"class_at_vtbl": {}}
	edges = {}
	callee_class = {}  # callee_addr_int -> {class: count}
	for r in records:
		if r.get("kind") != "call":
			continue
		caller = _as_int(r.get("caller"))
		callee = _as_int(r.get("callee"))
		if callee is None:
			continue
		edges[(caller, callee)] = edges.get((caller, callee), 0) + 1
		vt = _as_int(r.get("this_vtable"))
		if vt is not None:
			cls = idx["class_at_vtbl"].get(vt)
			if cls:
				callee_class.setdefault(callee, {})
				callee_class[callee][cls] = callee_class[callee].get(cls, 0) + 1
	if not edges:
		print("ingest-calls: no `call` records found.")
		return 0
	print(f"ingest-calls: {len(edges)} distinct edges, "
		  f"{len(callee_class)} callees with an observed `this` class.")
	for (caller, callee), cnt in sorted(edges.items(), key=lambda kv: kv[1], reverse=True)[:args.limit]:
		cn, ct = resolve_addr(con, caller) if caller is not None else ("(unknown)", "")
		en, et = resolve_addr(con, callee)
		cls = callee_class.get(callee)
		clsf = ""
		if cls:
			top = max(cls.items(), key=lambda kv: kv[1])[0]
			clsf = f"   this={top}"
		print(f"  {cn} -> {en}  (x{cnt}){clsf}")
	if args.apply:
		author = get_author(args)
		n = 0
		for callee, classmap in callee_class.items():
			top = max(classmap.items(), key=lambda kv: kv[1])[0]
			fr = resolve_func(con, "%08x" % callee)
			if not fr:
				continue
			faddr = fr[0]
			con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (top,))
			con.execute(
				"INSERT INTO bindings(func_addr,var_name,type_name,tier,author,updated_at) "
				"VALUES(?,?,?,?,?,?) ON CONFLICT(func_addr,var_name) DO UPDATE SET "
				"type_name=excluded.type_name, tier=excluded.tier, author=excluded.author, "
				"updated_at=excluded.updated_at",
				(faddr, "arg1", top, "mechanism", author, now_iso()))
			record_history(con, "bind", f"bind:{faddr}:arg1", "set", None, top, author,
						   "dynamic: observed this vtable")
			n += 1
		con.commit()
		print(f"  applied {n} arg1 type bindings (basis: dynamic observed `this`).")
	return 0


def cmd_ingest_allocs(args, con):
	"""FORMAT 4 - sizeof(class) from allocation size + constructor vtable.

    JSONL records:
      {"kind":"alloc","ret":"0x..","size":N,"site":"0x.."}
      {"kind":"ctor","obj":"0x..","vtable":"0x.."}   (the vtable write identifies the class)
      {"kind":"alloc","class":"Name","size":N}        (pre-correlated shortcut)
    Correlates an allocation's returned pointer with the ctor that stamps a vtable into it,
    yielding `sizeof(class)` - which sizes the struct and labels the alloc sites.
    """
	records = _read_jsonl(args.log)
	need_idx = any(r.get("kind") == "ctor" for r in records)
	idx = _best_rtti_index(args, con) if need_idx else {"class_at_vtbl": {}}
	alloc_by_ret = {}
	class_size = {}   # class -> (size, site)
	for r in records:
		if r.get("kind") == "alloc":
			cls = r.get("class")
			size = _as_int(r.get("size"))
			if cls and size is not None:
				if cls not in class_size or size > class_size[cls][0]:
					class_size[cls] = (size, _as_int(r.get("site")))
			ret = _as_int(r.get("ret"))
			if ret is not None and size is not None:
				alloc_by_ret[ret] = (size, _as_int(r.get("site")))
		elif r.get("kind") == "ctor":
			obj = _as_int(r.get("obj"))
			vt = _as_int(r.get("vtable"))
			cls = idx["class_at_vtbl"].get(vt) if vt is not None else None
			if cls and obj in alloc_by_ret:
				size, site = alloc_by_ret[obj]
				if cls not in class_size or size > class_size[cls][0]:
					class_size[cls] = (size, site)
	if not class_size:
		print("ingest-allocs: no class sizes recovered "
			  "(need alloc{class,size} or alloc{ret,size}+ctor{obj,vtable}).")
		return 0
	print(f"ingest-allocs: recovered sizeof for {len(class_size)} classes.")
	author = get_author(args)
	for cls, (size, site) in sorted(class_size.items()):
		sitef = f" (alloc site 0x{site:08x})" if site else ""
		print(f"  sizeof({cls}) = {size} (0x{size:x}) bytes{sitef}")
		if args.apply:
			con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (cls,))
			body = f"sizeof={size} (0x{size:x}) bytes (dynamic){sitef}"
			exists = con.execute(
				"SELECT 1 FROM notes WHERE target=? AND body LIKE 'sizeof=%'", (cls,)).fetchone()
			if not exists:
				con.execute(
					"INSERT INTO notes(target,body,author,tier,updated_at) VALUES(?,?,?,?,?)",
					(cls, body, author, "mechanism", now_iso()))
			con.execute(
				"INSERT INTO observations(subject,value,source,author,ts) VALUES(?,?,?,?,?)",
				(f"sizeof:{cls}", str(size), os.path.basename(args.log), author, now_iso()))
	if args.apply:
		con.commit()
		print(f"  applied sizeof notes (basis: dynamic).")
	return 0


# C type widths used by layout/reconstruct.
_CTYPE_SIZE = {
	"uint8": 1, "int8": 1, "char": 1, "bool": 1,
	"uint16": 2, "int16": 2, "short": 2,
	"uint32": 4, "int32": 4, "int": 4, "float": 4, "ptr": 4, "void*": 4,
	"uint64": 8, "int64": 8, "double": 8,
}


def _ftype_from_size(size):
	return {1: "uint8", 2: "uint16", 4: "uint32", 8: "double"}.get(size, "uint32")


def cmd_ingest_layout(args, con):
	"""FORMAT 5 - struct field layout from guard-page access traces.

    JSONL records: {"kind":"field","type":"Name"|"vtable":"0x..","offset":"0x..",
                    "size":N,"access":"r"|"w"}
    Each distinct (type, offset) becomes a field with a size-derived C type and an
    observed read/write note. Existing NAMED fields are never overwritten - this only
    fills unnamed offsets, so it complements hand RE instead of clobbering it.
    """
	records = _read_jsonl(args.log)
	need_idx = any(r.get("vtable") for r in records if r.get("kind") == "field")
	idx = _best_rtti_index(args, con) if need_idx else {"class_at_vtbl": {}}
	layout = {}   # type -> {offset: {"size":max,"access":set}}
	for r in records:
		if r.get("kind") != "field":
			continue
		typ = r.get("type")
		if not typ:
			vt = _as_int(r.get("vtable"))
			typ = idx["class_at_vtbl"].get(vt) if vt is not None else None
		off = _as_int(r.get("offset"))
		if not typ or off is None:
			continue
		size = _as_int(r.get("size")) or 4
		acc = r.get("access", "?")
		slot = layout.setdefault(typ, {}).setdefault(off, {"size": size, "access": set()})
		slot["size"] = max(slot["size"], size)
		slot["access"].add(acc)
	if not layout:
		print("ingest-layout: no `field` records found.")
		return 0
	author = get_author(args)
	applied = 0
	for typ, offs in sorted(layout.items()):
		print(f"{typ}: {len(offs)} distinct offsets")
		for off, slot in sorted(offs.items()):
			ft = _ftype_from_size(slot["size"])
			acc = "".join(sorted(slot["access"]))
			print(f"  +0x{off:<4x} {ft:<8} [{acc}]")
			if args.apply:
				exists = con.execute(
					"SELECT new_name FROM fields WHERE type_name=? AND offset=?",
					(typ, off)).fetchone()
				if exists:
					continue  # never clobber a hand-named field
				con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (typ,))
				con.execute(
					"INSERT INTO fields(type_name,offset,new_name,ftype,note,tier,author,"
					"evidence,updated_at,basis) VALUES(?,?,?,?,?,?,?,?,?,?) "
					"ON CONFLICT(type_name,offset) DO NOTHING",
					(typ, off, f"field_0x{off:x}", ft, f"observed {acc} width {slot['size']}",
					 "guess", author, "guard-page trace", now_iso(), "dynamic"))
				applied += 1
	if args.apply:
		con.commit()
		print(f"\napplied {applied} new fields (basis: dynamic; unnamed offsets only).")
	return 0


def _classify_samples(values):
	"""Infer a field/global type from observed integer samples.

    Returns (ftype, predicate, claim). Heuristic, recorded with basis=dynamic so a human
    still reviews; the point is to turn a column of hex into a typed hypothesis.
    """
	uniq = sorted(set(values))
	if not uniq:
		return ("uint32", "any", "no samples")
	if len(uniq) == 1:
		v = uniq[0]
		return ("uint32", f"eq:{v}", f"constant 0x{v:x}")
	# float reinterpret: plausible if every nonzero sample decodes to a sane magnitude
	floatish = True
	for v in uniq:
		if v == 0:
			continue
		try:
			fv = struct.unpack("<f", struct.pack("<I", v & 0xffffffff))[0]
		except struct.error:
			floatish = False
			break
		if fv != fv or abs(fv) == float("inf") or not (1e-6 <= abs(fv) <= 1e9):
			floatish = False
			break
	if floatish and any(v not in (0, 1) for v in uniq):
		return ("float", "any", "values decode to sane IEEE-754 floats")
	# pointer-ish: all in the user-address range and 4-aligned-ish
	if all(v == 0 or 0x00010000 <= v < 0x80000000 for v in uniq) and \
			any(v >= 0x00400000 for v in uniq):
		return ("ptr", "any", "values fall in the user/code address range")
	# bitmask: small flag-like values
	if all(bin(v).count("1") <= 4 for v in uniq) and max(uniq) < 0x10000 and len(uniq) > 1:
		union = 0
		for v in uniq:
			union |= v
		return ("uint32", "any", f"flag bits, union=0x{union:x}")
	return ("uint32", f"range:{min(uniq)}:{max(uniq)}", f"int range [{min(uniq)}..{max(uniq)}]")


_SUBJ_FIELD_RE = re.compile(r"^([A-Za-z_]\w*)\+0x([0-9a-fA-F]+)$")


def cmd_infer_types(args, con):
	"""FORMAT 6 - value/type inference from field/global samples.

    JSONL records: {"kind":"sample","subject":"Class+0x78"|"data_f0673f","value":"0x..","as":"f32"?}
    Aggregates samples per subject, classifies the type, and (with --apply) sets the field
    type for "Class+0xNN" subjects and records a dynamic assertion for any subject.
    """
	records = _read_jsonl(args.log)
	samples = {}
	forced = {}
	for r in records:
		if r.get("kind") != "sample":
			continue
		subj = r.get("subject")
		val = _as_int(r.get("value"))
		if not subj or val is None:
			continue
		samples.setdefault(subj, []).append(val)
		if r.get("as"):
			forced[subj] = r["as"]
	if not samples:
		print("infer-types: no `sample` records found.")
		return 0
	author = get_author(args)
	applied_fields = applied_asserts = 0
	for subj, vals in sorted(samples.items()):
		ft, pred, claim = _classify_samples(vals)
		if subj in forced:
			ft = {"f32": "float", "f": "float", "u32": "uint32",
				  "ptr": "ptr"}.get(forced[subj], ft)
		print(f"{subj}: {ft}  ({pred})  n={len(vals)} uniq={len(set(vals))}  -- {claim}")
		if not args.apply:
			continue
		m = _SUBJ_FIELD_RE.match(subj)
		if m:
			typ, off = m.group(1), int(m.group(2), 16)
			exists = con.execute(
				"SELECT new_name FROM fields WHERE type_name=? AND offset=?",
				(typ, off)).fetchone()
			con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (typ,))
			con.execute(
				"INSERT INTO fields(type_name,offset,new_name,ftype,note,tier,author,evidence,"
				"updated_at,basis) VALUES(?,?,?,?,?,?,?,?,?,?) "
				"ON CONFLICT(type_name,offset) DO UPDATE SET ftype=excluded.ftype, "
				"updated_at=excluded.updated_at, basis=excluded.basis",
				(typ, off, exists[0] if exists else f"field_0x{off:x}", ft, f"inferred: {claim}",
				 "guess", author, "value samples", now_iso(), "dynamic"))
			applied_fields += 1
		# HONESTY: the sampled VALUES are real (basis=dynamic), but the type CLASSIFICATION
		# is a heuristic over a handful of samples - it is NOT a confirmed fact. Record it
		# OPEN at tier=guess so it shows up as "needs confirmation", never as ground truth.
		con.execute(
			"INSERT INTO assertions(subject,claim,predicate,basis,method,tier,status,author,"
			"evidence,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)",
			(subj, f"[heuristic] {claim}", pred, "dynamic",
			 "infer-types value-sample classification (heuristic)", "guess", "open",
			 author, f"{len(vals)} samples from {os.path.basename(args.log)}", now_iso()))
		applied_asserts += 1
	if args.apply:
		con.commit()
		print(f"\napplied {applied_fields} field types, {applied_asserts} dynamic assertions.")
	return 0


# --------------------------------------------------------------------------- #
# FORMAT 7 - revtool-native typed C reconstruction (no Ghidra)
# --------------------------------------------------------------------------- #
# A raw offset-deref the rewrite targets: `*(T*)((char*)v + 0xNN)` / `*(T*)(v + 0xNN)`.
# Counting these before/after the rewrite gives an honest coverage number for the output.
_RAW_DEREF_RE = re.compile(r"\*\(\s*\w[\w ]*\*\s*\)\(\s*(?:\(\s*char\s*\*\s*\))?\s*\w+\s*\+\s*0x[0-9a-fA-F]+")


def _ctype_size(ft):
	if not ft:
		return 4
	return _CTYPE_SIZE.get(ft.replace("*", "").strip(), 4)


def _emit_struct(name, fields):
	"""fields: list of (offset, fieldname, ftype, note). Emit a C struct with padding."""
	out = [f"struct {name} {{"]
	cur = 0
	for off, fname, ft, note in sorted(fields, key=lambda x: x[0]):
		if off > cur:
			out.append(f"    char _pad_{cur:x}[0x{off - cur:x}];")
		ctype = (ft or "uint32_t")
		ctype = {"uint8": "uint8_t", "uint16": "uint16_t", "uint32": "uint32_t",
				 "int8": "int8_t", "int16": "int16_t", "int32": "int32_t",
				 "int64": "int64_t", "uint64": "uint64_t", "ptr": "void*"}.get(ctype, ctype)
		cmt = f"  // {note}" if note else ""
		out.append(f"    /* +0x{off:x} */ {ctype} {fname};{cmt}")
		cur = off + _ctype_size(ft)
	out.append("};")
	return "\n".join(out)


def _rewrite_fields(code, var, fields):
	"""Rewrite `*(T*)((char*)var + 0xNN)` and `*(T*)var` (offset 0) to `var->field`."""
	for off, fname in fields:
		if off == 0:
			pat = re.compile(
				r"\*\(\s*\w[\w ]*\*\s*\)\s*" + re.escape(var) + r"\b")
			code = pat.sub(f"{var}->{fname}", code)
		pat2 = re.compile(
			r"\*\(\s*\w[\w ]*\*\s*\)\(\s*\(\s*char\s*\*\s*\)\s*" + re.escape(var)
			+ r"\s*\+\s*0x%x\s*\)" % off)
		code = pat2.sub(f"{var}->{fname}", code)
		# also the no-cast form: *(uint32_t*)(var + 0xNN)
		pat3 = re.compile(
			r"\*\(\s*\w[\w ]*\*\s*\)\(\s*" + re.escape(var) + r"\s*\+\s*0x%x\s*\)" % off)
		code = pat3.sub(f"{var}->{fname}", code)
	return code


def cmd_reconstruct(args, con):
	"""FORMAT 7 - emit typed C for a function from the overlay (no Ghidra).

    Pulls the function body, applies global renames, then rewrites offset-arithmetic field
    accesses on every bound var into `var->field` and prints the struct decls those fields
    imply. The output reads like source instead of `*(uint32_t*)((char*)a1 + 0x78)`.
    """
	dump = get_dump(con, args)
	fr = resolve_func(con, args.target)
	if not fr:
		print(f"error: function not found: {args.target}", file=sys.stderr)
		return 1
	addr, name, sig, sb, eb, sl, el = fr
	code = read_func_lines(dump, sb, eb)

	globals_map = load_globals(con)
	locals_map = load_locals(con, addr)
	bindings = load_bindings(con, addr)

	# strip the dump's leading "ADDR   " gutter so the C reads cleanly
	stripped = []
	for line in code:  # read_func_lines returns a list of lines
		mm = re.match(r"^[0-9a-f]{8}\s{2}(.*)$", line)
		stripped.append(mm.group(1) if mm else line)
	body = "\n".join(stripped)

	# 1) rewrite field accesses per bound var (before global rename so var tokens are intact)
	#    Count raw offset-derefs before/after so the output never pretends to be complete:
	#    only a few `*(T*)((char*)v+0xNN)` shapes are rewritten; nested/indexed/odd ones stay.
	raw_before = len(_RAW_DEREF_RE.findall(body))
	used_types = {}
	for var, typ in bindings.items():
		flds = load_fields(con, typ)  # {offset: (new_name, ftype, note, ...)}
		used_types[typ] = flds
		pairs = [(off, vals[0]) for off, vals in flds.items()]
		body = _rewrite_fields(body, var, pairs)
	raw_after = len(_RAW_DEREF_RE.findall(body))
	rewrote = raw_before - raw_after

	# 2) apply global (sub_/data_) renames only; keep var names as bound
	rename = {k: v[0] for k, v in globals_map.items()}
	body = apply_renames(body, rename)
	# local var renames too (so arg3 -> sectorId reads nicely), but only whole-word
	for orig, vals in locals_map.items():
		body = re.sub(r"\b" + re.escape(orig) + r"\b", vals[0], body)

	out = []
	self_name = globals_map.get(name, (name,))[0]
	out.append(f"// reconstructed by revtool from {name}"
			   + (f" ({self_name})" if self_name != name else "") + f" @ 0x{addr}")
	out.append(f"// dump lines {sl}-{el}; NOT a compiler-faithful decompile - field/name")
	out.append("// overlay applied to the pseudocode body. A readability LENS, not source.")
	out.append(f"// field-rewrite coverage: {rewrote} offset-deref(s) -> v->field, "
			   f"{raw_after} left raw (nested/indexed/unbound).")
	out.append("")
	for typ, flds in sorted(used_types.items()):
		if not flds:
			out.append(f"struct {typ} {{ /* no fields named yet */ }};")
			continue
		fields = [(off, vals[0], vals[1], vals[2]) for off, vals in flds.items()]
		out.append(_emit_struct(typ, fields))
		out.append("")
	if bindings:
		out.append("// bindings: " + ", ".join(f"{v}:{t}*" for v, t in sorted(bindings.items())))
		out.append("")
	out.append(body)
	text = "\n".join(out)
	if args.out:
		with open(args.out, "w", encoding="utf-8") as f:
			f.write(text + "\n")
		print(f"reconstruct: wrote {args.out} ({len(text.splitlines())} lines, "
			  f"{len(used_types)} structs)")
	else:
		print(text)
	return 0
