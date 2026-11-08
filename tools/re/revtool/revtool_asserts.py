"""
===========================================================================
revtool_asserts.py - assert/observe/asserts: value predicates over the overlay

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
import bisect
import mmap
import re

from revtool_claims import reconcile_assertions
from revtool_dump import get_dump, resolve_func
from revtool_store import enforce_basis_tier, get_author, now_iso
from revtool_store import record_history, valid_tier
from revtool_syntax import STATIC_WEAK_BASES, valid_basis

def cmd_assert(args, con):
	author = get_author(args)
	if args.action == "add":
		tier = valid_tier(args.tier)
		basis = valid_basis(args.basis)
		enforce_basis_tier(basis, tier)
		if basis == "absence" and not args.blindspots:
			print("warning: absence claims should record --blindspots "
				  "(what your search method cannot see), e.g. 'base+disp, computed-ptr, dynamic'.",
				  file=sys.stderr)
		cur = con.execute(
			"INSERT INTO assertions(subject,claim,predicate,basis,method,blindspots,tier,"
			"expected,author,evidence,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
			(args.subject, args.claim, args.predicate, basis, args.method, args.blindspots,
			 tier, args.expected, author, args.evidence, now_iso()))
		aid = cur.lastrowid
		record_history(con, "assert", f"assert:{aid}", "set", None, args.claim, author,
					   f"basis={basis} predicate={args.predicate}")
		con.commit()
		flag = "" if basis not in STATIC_WEAK_BASES else "  (NEEDS DYNAMIC CONFIRMATION)"
		print(f"assert [{aid}] {args.subject}: {args.claim}  "
			  f"[{tier}/{basis}]{flag} by {author}")
		return 0
	if args.action == "list":
		q = ("SELECT id,subject,claim,predicate,basis,tier,status,observed,disputed,"
			 "dispute_reason FROM assertions")
		clauses, params = [], []
		if args.subject:
			clauses.append("subject LIKE ?")
			params.append(f"%{args.subject}%")
		if args.status:
			clauses.append("status=?")
			params.append(args.status)
		if clauses:
			q += " WHERE " + " AND ".join(clauses)
		q += " ORDER BY status DESC, id"
		any_hit = False
		for aid, subj, claim, pred, basis, tier, status, observed, disp, reason in \
				con.execute(q, params):
			any_hit = True
			mark = {"open": "[ ]", "confirmed": "[OK]", "refuted": "[XX]"}.get(status, "[?]")
			obs = f"  observed={observed}" if observed is not None else ""
			dr = f"  !! {reason}" if disp else ""
			print(f"{mark} #{aid} {subj}  ({pred or 'no-pred'}) "
				  f"[{tier}/{basis}]{obs}{dr}")
			print(f"      {claim}")
		if not any_hit:
			print("(no assertions)")
		return 0
	return 0


def cmd_observe(args, con):
	"""Record a single runtime observation and reconcile it against assertions."""
	author = get_author(args)
	con.execute(
		"INSERT INTO observations(subject,value,source,author,ts) VALUES(?,?,?,?,?)",
		(args.subject, args.value, args.source, author, now_iso()))
	results = reconcile_assertions(con, args.subject, args.value, args.source, author)
	con.commit()
	print(f"observed {args.subject} = {args.value}"
		  + (f"  (source: {args.source})" if args.source else ""))
	if not results:
		print("  (no assertions on this subject)")
	for r in results:
		tag = {"confirmed": "CONFIRMED", "refuted": "REFUTED !!", "needs-human": "needs review"}\
			.get(r["verdict"], r["verdict"])
		print(f"  assert #{r['id']}: {tag}  ({r['detail']})  -- {r['claim']}")
	return 0


# Parsers for native-trace log lines -> (subject, value) observations.
_TRACE_GLOBAL_RE = re.compile(r"(data_[0-9a-fA-F]+)\([^)]*\)=(-?[0-9]+(?:\.[0-9]+)?)")
_TRACE_FOGRS_RE = re.compile(
	r"FOG_RS\s+(\w+)\s+state=0x[0-9A-Fa-f]+\s+value=0x[0-9A-Fa-f]+\s+int=(-?\d+)\s+float=(\S+)")


def _decode_hex_run(hexrun):
	"""`b'30 00 00 00 '` -> 'aaa' up to (not including) the first NUL byte."""
	out = bytearray()
	for tok in hexrun.split():
		b = int(tok, 16)
		if b == 0:
			break
		out.append(b)
	return out.decode("latin-1", "replace")
# Member extraction is deliberately limited to Hungarian `m_*` fields: high signal, ~zero
# false positives. Method calls in asserts are mostly STL noise (.size()/.end()) so we do
# NOT mine them - the raw expression is still printed for the human to read.
_MEMBER_RE = re.compile(r"\bm_[A-Za-z]\w*")


def _assert_expr_useful(expr):
	"""True if an assert expression carries RE signal (an identifier/comparison/call), as
    opposed to a bare format/sentinel string like "%s", "0", or "" that names nothing."""
	if not expr:
		return False
	if "%" in expr and not (_MEMBER_RE.search(expr) or "::" in expr
							or "==" in expr or "!=" in expr or "(" in expr):
		return False
	return bool(re.search(r"[A-Za-z_]", expr))


def _dedup_exprs(exprs):
	"""Collapse identical (src, expr) pairs to (pair, count), preserving first-seen order."""
	counts = {}
	order = []
	for pair in exprs:
		if pair not in counts:
			order.append(pair)
		counts[pair] = counts.get(pair, 0) + 1
	return [(pair, counts[pair]) for pair in order]


def _func_byte_index(con):
	"""(rows, starts) for bisecting a dump byte offset to its containing function.

    rows: list of (start_byte, end_byte, addr, name) sorted by start_byte.
    """
	rows = con.execute(
		"SELECT start_byte,end_byte,addr,name FROM functions ORDER BY start_byte").fetchall()
	starts = [r[0] for r in rows]
	return rows, starts


def _containing_func(rows, starts, byte_off):
	i = bisect.bisect_right(starts, byte_off) - 1
	if i >= 0:
		sb, eb, addr, name = rows[i]
		if sb <= byte_off < eb:
			return (addr, name)
	return (None, None)


def cmd_asserts(args, con):
	"""FORMAT 1 - static assert-string scraper.

    The client is an assert-heavy build: every ASSERT carries its `__FILE__` and the
    stringified condition, which routinely names real members (`m_sDungeonInfo`),
    methods (`REGIONMGR.IsEnableRegist()`) and invariants (`iRid <= 0x7FFF`). Scraping
    them maps each function to its ORIGINAL source module and yields rename/field
    candidates - zero runtime needed.
    """
	dump = get_dump(con, args)
	if not os.path.exists(dump):
		print(f"error: dump not found: {dump}", file=sys.stderr)
		return 1
	rows, starts = _func_byte_index(con)
	by_file = {}                  # file -> count
	by_func = {}                  # (addr,name) -> {"file":set,"members":set,"exprs":[...]}
	# records: (src_file, expr_text_or_None, byte_offset). expr=None => pointer-form whose
	# data symbol resolved to nothing useful (still attributes the function to its source).
	records = []
	n_inline = n_ptr = n_ptr_resolved = 0
	needed = {}                   # data_sym -> list of (src, byte_offset)  (pass-one refs)
	with open(dump, "rb") as f:
		mm = mmap.mmap(f.fileno(), 0, access=mmap.ACCESS_READ)
		try:
			# PASS 1a - inline asserts: `"file.cpp", "expr"`
			for m in ASSERT_PAIR_RE.finditer(mm):
				src = m.group(1).decode("ascii", "replace").lstrip(".\\/")
				expr = m.group(2).decode("ascii", "replace")
				records.append((src, expr, m.start()))
				n_inline += 1
			# PASS 1b - pointer asserts: `"file.cpp", &data_xxxx`  (collect the symbols)
			for m in ASSERT_PTR_RE.finditer(mm):
				src = m.group(1).decode("ascii", "replace").lstrip(".\\/")
				sym = m.group(2).decode("ascii", "replace")
				needed.setdefault(sym, []).append((src, m.start()))
				n_ptr += 1
			# PASS 2 - resolve the needed data symbols to their string literals: first the
			# `char const ... = "..."` form, then the raw hex-dump form for short strings.
			data_map = {}
			if needed:
				want = set(needed)
				for m in DATA_STR_DEF_RE.finditer(mm):
					nm = m.group(1).decode("ascii", "replace")
					if nm in want:
						data_map[nm] = m.group(2).decode("ascii", "replace")
						if len(data_map) == len(want):
							break
				missing = want - set(data_map)
				if missing:
					for m in DATA_LABEL_RE.finditer(mm):
						nm = m.group(1).decode("ascii", "replace")
						if nm in missing:
							data_map[nm] = _decode_hex_run(m.group(2))
							missing.discard(nm)
							if not missing:
								break
			for sym, occ in needed.items():
				txt = data_map.get(sym)
				for src, off in occ:
					if txt is not None:
						n_ptr_resolved += 1
					records.append((src, txt, off))
		finally:
			mm.close()

	# Aggregate. Every assert attributes its function to a source module; member names are
	# mined only from expressions that actually carry signal (not "%s"/"0"/sentinels).
	total = 0
	for src, expr, off in records:
		total += 1
		by_file[src] = by_file.get(src, 0) + 1
		addr, name = _containing_func(rows, starts, off)
		rec = by_func.setdefault((addr, name),
								 {"file": set(), "members": set(), "exprs": []})
		rec["file"].add(src)
		if _assert_expr_useful(expr):
			for mm2 in _MEMBER_RE.finditer(expr):
				rec["members"].add(mm2.group(0))
			rec["exprs"].append((src, expr))
		elif expr is not None:
			rec["exprs"].append((src, expr))           # e.g. "%s" - shown, not mined
		else:
			rec["exprs"].append((src, "(ptr; unresolved)"))

	if total == 0:
		print("(no assert strings found)")
		return 0

	# --- targeted views ---
	if args.func:
		fr = resolve_func(con, args.func)
		if not fr:
			print(f"error: function not found: {args.func}", file=sys.stderr)
			return 1
		key = (fr[0], fr[1])
		rec = by_func.get(key)
		if not rec:
			print(f"{fr[1]}: no asserts found in this function")
			return 0
		print(f"{fr[1]} @ 0x{fr[0]}")
		print(f"  source: {', '.join(sorted(rec['file']))}")
		if rec["members"]:
			print(f"  members: {', '.join(sorted(rec['members']))}")
		for (src, expr), n in _dedup_exprs(rec["exprs"]):
			mult = f"  (x{n})" if n > 1 else ""
			print(f"    [{src}] {expr}{mult}")
		return 0

	if args.file:
		sel = {k: v for k, v in by_func.items()
			   if any(args.file.lower() in s.lower() for s in v["file"])}
		print(f"asserts in *{args.file}*: {sum(len(v['exprs']) for v in sel.values())} "
			  f"in {len(sel)} functions")
		for (addr, name), rec in sorted(sel.items(), key=lambda kv: (kv[0][1] or "")):
			label = name or "(no function)"
			applied = con.execute("SELECT new_name FROM globals WHERE orig_name=?",
								  (name,)).fetchone() if name else None
			disp = (applied[0] if applied and applied[0] else label)
			mem = f"  members: {', '.join(sorted(rec['members']))}" if rec["members"] else ""
			print(f"\n  {disp} @ 0x{addr}{mem}")
			for (src, expr), n in _dedup_exprs(rec["exprs"])[:args.limit]:
				mult = f"  (x{n})" if n > 1 else ""
				print(f"    {expr}{mult}")
		return 0

	if args.suggest:
		cand = [(k, v) for k, v in by_func.items() if k[1] and v["members"]]
		cand.sort(key=lambda kv: len(kv[1]["members"]), reverse=True)
		print(f"rename/field candidates (functions with named members in asserts): {len(cand)}")
		for (addr, name), rec in cand[:args.limit]:
			applied = con.execute("SELECT new_name FROM globals WHERE orig_name=?",
								  (name,)).fetchone()
			tag = "" if (applied and applied[0]) else "  [unnamed]"
			print(f"  {name} @ 0x{addr}{tag}")
			print(f"    src={', '.join(sorted(rec['file']))}  "
				  f"members={', '.join(sorted(rec['members']))}")
		return 0

	if args.apply_notes:
		author = get_author(args)
		n = 0
		for (addr, name), rec in by_func.items():
			if not name:
				continue
			srcs = ", ".join(sorted(rec["file"]))
			mems = ", ".join(sorted(rec["members"]))
			body = f"src={srcs}" + (f"; asserts name: {mems}" if mems else "")
			# idempotent: skip if a src= note already exists on this function
			exists = con.execute(
				"SELECT 1 FROM notes WHERE target=? AND body LIKE 'src=%'", (name,)).fetchone()
			if exists:
				continue
			con.execute("INSERT INTO notes(target,body,author,tier,updated_at) VALUES(?,?,?,?,?)",
						(name, body, author, "mechanism", now_iso()))
			n += 1
		con.commit()
		print(f"asserts: attached source/member notes to {n} functions "
			  f"(basis: observed assert strings).")
		return 0

	# --- default: summary ---
	print(f"asserts: {total} assert strings ({n_inline} inline, {n_ptr} pointer-form, "
		  f"{n_ptr_resolved} of those resolved to a literal) across {len(by_file)} source "
		  f"files, {len([k for k in by_func if k[1]])} functions.")
	print("\ntop source modules by assert count:")
	for src, cnt in sorted(by_file.items(), key=lambda kv: kv[1], reverse=True)[:args.limit]:
		print(f"  {cnt:5d}  {src}")
	print("\nnext: `asserts --file maploader.cpp` | `asserts --suggest` | "
		  "`asserts --apply-notes`")
	return 0


# --------------------------------------------------------------------------- #
# RTTI / vtable index (shared by `classes` and the dynamic ingest commands)
# --------------------------------------------------------------------------- #
_VFTABLE_RE = re.compile(
	r"^([0-9a-f]{8})\s+struct\s+[\w:<>]+::VTable\s+([\w:<>]+)::`vftable'"
	r"(?:\{for `([\w:<>]+)'\})?")
_COL_RE = re.compile(r"_RTTICompleteObjectLocator\s+([\w:<>]+)::`RTTI Complete Object Locator'")
# A class's Base Class Array lists, nearest-first, the class itself ([0]) then all of its
# ancestors ([1:]). MSVC enumerates the FULL ancestor set here, so one array == the whole
# inheritance chain for that class (no recursion needed). Two text shapes:
#   multi:  `struct CObjChild::`RTTI Base Class Array' = { ...[0x0]=A::...; [0x1]=B::... }`
#   single: `void* CObj::`RTTI Base Class Array' = CObj::`RTTI Base Class Descriptor...`  (root)
_ARR_START_RE = re.compile(r"struct ([\w:<>]+)::`RTTI Base Class Array' =")
_ARR_SINGLE_RE = re.compile(
	r"void\* ([\w:<>]+)::`RTTI Base Class Array' = ([\w:<>]+)::`RTTI Base Class Descriptor")
_ARR_ENTRY_RE = re.compile(r"\[0x[0-9a-fA-F]+\]\s*=\s*([\w:<>]+)::`RTTI Base Class Descriptor")


ASSERT_PAIR_RE = re.compile(
	rb'"([^"\n]{1,80}\.(?:cpp|cxx|cc|hpp|h))"\s*,\s*"([^"\n]{1,400})"')
# Pointer-form assert: the stringified condition is passed as `&data_xxxx` instead of an
# inline literal, e.g. `sub_467c50(arg3, 0x17f, ".\maploader.cpp", &data_c43d30)`. The file
# is still an inline literal. group1=file, group2=the data symbol to resolve in pass two.


ASSERT_PTR_RE = re.compile(
	rb'\bsub_[0-9a-fA-F]+\(\s*[^,()\n]{0,48}?,\s*(?:0x[0-9a-fA-F]+|\d+)\s*,\s*'
	rb'"([^"\n]{1,80}\.(?:cpp|cxx|cc|hpp|h))"\s*,\s*&(data_[0-9a-fA-F]+)\s*\)')
# Data string definition: `char const data_xxxx[0x..] = "literal", 0`. Resolves the pointer.


DATA_STR_DEF_RE = re.compile(
	rb'\bchar const (data_[0-9a-fA-F]+)\[0x[0-9a-fA-F]+\]\s*=\s*"((?:[^"\\\n]|\\.)*)"')
# Short strings the decompiler leaves as a raw hex dump instead of a `char const` literal:
#   `00c422d4  data_c422d4:`  /  `00c422d4   30 00 00 00   0...`  ->  "0"
# Capture the label then the hex-byte run on the following dump line; decode until the NUL.


DATA_LABEL_RE = re.compile(
	rb'(data_[0-9a-fA-F]+):[ \t]*\r?\n[0-9a-f]{8}\s+((?:[0-9a-f]{2} ){1,32})')
