"""
===========================================================================
revtool_claims.py - claim commands: rename/type/field/bind/note and the review queue

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
import re

from revtool_debt import STRUCTURE_MIN_OFFSETS, compute_debt, debt_limit
from revtool_debt import depth_gate, depth_satisfied, function_field_offsets
from revtool_dump import get_dump, locate_claim, note_rows_for_aliases
from revtool_dump import note_target_aliases, note_target_is_symbol_shaped
from revtool_dump import read_func_lines, rename_key_can_fire, resolve_func
from revtool_dump import symbol_is_annotated, symbol_is_renamed
from revtool_overlay import load_bindings, load_fields, load_globals
from revtool_paths import DEFAULT_BINARY, TARGET_SHA256
from revtool_store import enforce_basis_tier, get_author, meta_set, now_iso
from revtool_store import record_artifact, record_history, sha256_file
from revtool_store import valid_tier
from revtool_syntax import GLOBAL_TOKEN_RE, eval_predicate, valid_basis

def cmd_rename(args, con):
	author = get_author(args)
	tier = valid_tier(args.tier)
	ts = now_iso()
	basis = getattr(args, "basis", None)
	enforce_basis_tier(basis, tier)
	method = getattr(args, "method", None)
	blindspots = getattr(args, "blindspots", None)
	if args.scope == "global":
		orig, new = args.a, args.b
		# A global rename is keyed on the token as it appears IN THE DUMP, because
		# that is what apply_renames() rewrites. Address-shaped sub_ keys are also
		# consumed as semantic claims by dossiers/planners even when an analyzer
		# gave the dump entry a different token. Keep those claims, but distinguish
		# their ledger use from the text-overlay behavior of `view`.
		# `functions` indexes FUNCTIONS only, so a data_ token is legitimately absent
		# from it; testing membership alone would cry wolf on every data rename. The
		# test is therefore whether the key OCCURS IN THE DUMP, which is exactly what
		# apply_renames needs and is right for both kinds of symbol. Accepting the
		# token on SHAPE instead - the previous rule - stayed silent for 85 keys in
		# this store that are well-formed and absent from the dump.
		can_fire, hint = rename_key_can_fire(con, get_dump(con, args), orig)
		if can_fire is False:
			print(f"warning: {orig} does not occur in the dump. The claim will be "
				  f"stored for address-based dossiers/planners, but this key cannot "
				  f"rewrite dump text and will not appear in `view` output.{hint}",
				  file=sys.stderr)
		# A backend that already holds the function's text (rizin passes
		# disassembly lines as args.gate_lines) feeds the depth gate with
		# it; the dump backend leaves it unset and the gate reads the dump.
		gate = depth_gate(con, args, get_dump(con, args), orig, tier,
						  lines=getattr(args, "gate_lines", None))
		if gate is not None:
			return gate
		kind = "func" if orig.startswith("sub_") else ("data" if orig.startswith("data_") else None)
		prev = con.execute("SELECT new_name FROM globals WHERE orig_name=?", (orig,)).fetchone()
		con.execute(
			"INSERT INTO globals(orig_name,new_name,kind,note,tier,author,evidence,updated_at,"
			"basis,method,blindspots) VALUES(?,?,?,NULL,?,?,?,?,?,?,?) "
			"ON CONFLICT(orig_name) DO UPDATE SET new_name=excluded.new_name, "
			"tier=excluded.tier, author=excluded.author, evidence=excluded.evidence, "
			"updated_at=excluded.updated_at, basis=excluded.basis, method=excluded.method, "
			"blindspots=excluded.blindspots",
			(orig, new, kind, tier, author, args.evidence, ts, basis, method, blindspots))
		record_history(con, "global", f"global:{orig}",
					   "rename" if prev else "set",
					   prev[0] if prev else None, new, author)
		con.commit()
		print(f"global: {orig} -> {new}  [{tier}] by {author}")
	elif args.scope == "local":
		func = resolve_func(con, args.func)
		if not func:
			print(f"error: function not found: {args.func}", file=sys.stderr)
			return 1
		faddr = func[0]
		prev = con.execute(
			"SELECT new_name FROM locals WHERE func_addr=? AND orig_name=?",
			(faddr, args.a)).fetchone()
		con.execute(
			"INSERT INTO locals(func_addr,orig_name,new_name,tier,author,evidence,updated_at) "
			"VALUES(?,?,?,?,?,?,?) "
			"ON CONFLICT(func_addr,orig_name) DO UPDATE SET new_name=excluded.new_name, "
			"tier=excluded.tier, author=excluded.author, evidence=excluded.evidence, "
			"updated_at=excluded.updated_at",
			(faddr, args.a, args.b, tier, author, args.evidence, ts))
		record_history(con, "local", f"local:{faddr}:{args.a}",
					   "rename" if prev else "set",
					   prev[0] if prev else None, args.b, author)
		con.commit()
		print(f"local @ {func[1]} (0x{faddr}): {args.a} -> {args.b}  [{tier}] by {author}")
	return 0


def cmd_type(args, con):
	if args.action == "new":
		note = getattr(args, "note", None)
		author = get_author(args)
		tier = valid_tier(getattr(args, "tier", "guess"))
		basis = getattr(args, "basis", None)
		enforce_basis_tier(basis, tier)
		existed = con.execute("SELECT 1 FROM types WHERE name=?", (args.name,)).fetchone()
		con.execute(
			"INSERT OR IGNORE INTO types(name,note,tier,author,evidence,updated_at,basis) "
			"VALUES(?,?,?,?,?,?,?)",
			(args.name, note, tier, author, getattr(args, "evidence", None),
			 now_iso(), basis))
		if note:  # also backfill if the type was auto-created earlier with NULL note
			con.execute("UPDATE types SET note=? WHERE name=? AND note IS NULL",
						(note, args.name))
		if not existed:
			record_history(con, "type", f"type:{args.name}", "set", None,
						   args.name, author, note)
		con.commit()
		print(f"type: {args.name}  [{tier}] by {author}")
	elif args.action == "rename":
		old, new = args.name, getattr(args, "newname", None)
		if not old or not new:
			print("usage: type rename <oldName> <newName>", file=sys.stderr)
			return 1
		if not con.execute("SELECT 1 FROM types WHERE name=?", (old,)).fetchone():
			print(f"error: type not found: {old}", file=sys.stderr)
			return 1
		if con.execute("SELECT 1 FROM types WHERE name=?", (new,)).fetchone():
			print(f"error: target type already exists: {new}", file=sys.stderr)
			return 1
		author = get_author(args)
		# Rename the type itself + every structured reference to it. A pointer/array
		# C type like "OldName*" is rewritten token-wise so "OldNameFoo" is untouched.
		con.execute("UPDATE types SET name=? WHERE name=?", (new, old))
		con.execute("UPDATE fields SET type_name=? WHERE type_name=?", (new, old))
		con.execute("UPDATE bindings SET type_name=? WHERE type_name=?", (new, old))
		tok = re.compile(r"\b" + re.escape(old) + r"\b")
		ftx = 0
		for tname, off, ft in con.execute(
				"SELECT type_name,offset,ftype FROM fields WHERE ftype LIKE ?",
				(f"%{old}%",)).fetchall():
			newft = tok.sub(new, ft)
			if newft != ft:
				con.execute("UPDATE fields SET ftype=? WHERE type_name=? AND offset=?",
							(newft, tname, off))
				ftx += 1
		record_history(con, "type", f"type:{old}", "rename", old, new, author)
		con.commit()
		print(f"type: {old} -> {new}  (ftype refs updated: {ftx})  by {author}")
	elif args.action == "list":
		for name, note in con.execute("SELECT name,note FROM types ORDER BY name"):
			nf = con.execute("SELECT COUNT(*) FROM fields WHERE type_name=?", (name,)).fetchone()[0]
			print(f"{name}  ({nf} fields){'  - ' + note if note else ''}")
	elif args.action == "show":
		rows = con.execute(
			"SELECT offset,new_name,ftype,note FROM fields WHERE type_name=? ORDER BY offset",
			(args.name,)).fetchall()
		if not rows:
			print(f"(type {args.name} has no fields)")
			return 0
		print(f"type {args.name}:")
		for off, nm, ft, note in rows:
			print(f"  +0x{off:x}  {nm}{'  : ' + ft if ft else ''}{'  // ' + note if note else ''}")
	return 0


def cmd_field(args, con):
	off = int(args.offset, 16)
	author = get_author(args)
	tier = valid_tier(args.tier)
	basis = getattr(args, "basis", None)
	enforce_basis_tier(basis, tier)
	method = getattr(args, "method", None)
	blindspots = getattr(args, "blindspots", None)
	con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (args.type,))
	prev = con.execute(
		"SELECT new_name FROM fields WHERE type_name=? AND offset=?", (args.type, off)).fetchone()
	con.execute(
		"INSERT INTO fields(type_name,offset,new_name,ftype,note,tier,author,evidence,updated_at,"
		"basis,method,blindspots) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) "
		"ON CONFLICT(type_name,offset) DO UPDATE SET new_name=excluded.new_name, "
		"ftype=excluded.ftype, note=excluded.note, tier=excluded.tier, "
		"author=excluded.author, evidence=excluded.evidence, updated_at=excluded.updated_at, "
		"basis=excluded.basis, method=excluded.method, blindspots=excluded.blindspots",
		(args.type, off, args.name, args.ftype, args.note, tier, author, args.evidence, now_iso(),
		 basis, method, blindspots))
	record_history(con, "field", f"field:{args.type}:0x{off:x}",
				   "rename" if prev else "set", prev[0] if prev else None, args.name, author)
	con.commit()
	print(f"field: {args.type}+0x{off:x} = {args.name}  [{tier}] by {author}")
	return 0


def cmd_bind(args, con):
	# A binding names the pointee struct. The renderer and field resolver add the
	# pointer indirection themselves. Accepting `CIFWnd*` here silently creates a
	# distinct type literally named `CIFWnd*` and then models the local as
	# `CIFWnd**`, which is especially easy for a newcomer to miss in a long HLIL
	# view. Reject C declarators instead of storing a plausible-looking bad claim.
	if re.search(r"[*&\[\]]", args.type):
		print(
			f"error: bind expects a pointee type name, not a C declarator: {args.type!r}; "
			"pass the struct name without '*', '&', or array brackets",
			file=sys.stderr,
		)
		return 1
	func = resolve_func(con, args.func)
	if not func:
		print(f"error: function not found: {args.func}", file=sys.stderr)
		return 1
	faddr = func[0]
	author = get_author(args)
	tier = valid_tier(args.tier)
	con.execute("INSERT OR IGNORE INTO types(name,note) VALUES(?,NULL)", (args.type,))
	con.execute(
		"INSERT INTO bindings(func_addr,var_name,type_name,tier,author,updated_at) "
		"VALUES(?,?,?,?,?,?) "
		"ON CONFLICT(func_addr,var_name) DO UPDATE SET type_name=excluded.type_name, "
		"tier=excluded.tier, author=excluded.author, updated_at=excluded.updated_at",
		(faddr, args.var, args.type, tier, author, now_iso()))
	record_history(con, "bind", f"bind:{faddr}:{args.var}", "set", None, args.type, author)
	con.commit()
	print(f"bind @ {func[1]} (0x{faddr}): {args.var} : {args.type}*  [{tier}] by {author}")
	return 0


def cmd_note(args, con):
	author = get_author(args)
	if args.action == "add":
		# Contradiction guard (color0x18c lesson): notes on the same symbol silently
		# accumulated into competing claims. Surface existing notes on the same base
		# symbol BEFORE the new one lands, so the author either reconciles or
		# explicitly supersedes them.
		base = (args.target or "").split("@")[0]
		existing = []
		if base:
			# Resolve the target to every spelling it may have been filed under. The
			# guard used to match only the spelling being written, so adding under
			# `sub_XXXXXX` gave NO heads-up about research already filed under the
			# applied name - the guard was blind precisely when it mattered.
			aliases, known, ambiguous = note_target_aliases(con, base)
			existing = [(nid, tgt, body) for nid, tgt, body, _a, _t
						in note_rows_for_aliases(con, aliases)]
			if ambiguous:
				print(f"  warning: {base} is an AMBIGUOUS name shared by "
					  f"{', '.join(ambiguous)}; notes shown span all of them.",
					  file=sys.stderr)
			if not known and note_target_is_symbol_shaped(base):
				# Not an error: 603 live notes are attached to expressions rather
				# than symbols, so any target string stays legal. But a mistyped
				# symbol files research where nobody will ever search for it.
				print(f"  warning: {base} is not in the function index and has no "
					  f"rename - check the spelling, or this note may be filed where "
					  f"nobody will look.", file=sys.stderr)
		cur = con.execute(
			"INSERT INTO notes(target,body,author,tier,updated_at) VALUES(?,?,?,?,?)",
			(args.target, args.body, author, valid_tier(args.tier), now_iso()))
		new_id = cur.lastrowid
		record_history(con, "note", f"note:{new_id}", "set", None, args.body, author)
		# --supersedes: retire outdated claims VISIBLY instead of letting them coexist.
		superseded = []
		if getattr(args, "supersedes", None):
			for tok in str(args.supersedes).split(","):
				tok = tok.strip().lstrip("#")
				if not tok.isdigit():
					continue
				oid = int(tok)
				old = con.execute("SELECT body FROM notes WHERE id=?", (oid,)).fetchone()
				if not old:
					print(f"  warning: --supersedes {oid}: no such note", file=sys.stderr)
					continue
				if not old[0].startswith("[SUPERSEDED"):
					con.execute("UPDATE notes SET body=?, updated_at=? WHERE id=?",
								(f"[SUPERSEDED by #{new_id}] " + old[0], now_iso(), oid))
					record_history(con, "note", f"note:{oid}", "supersede",
								   old[0], f"superseded by #{new_id}", author)
				superseded.append(oid)
		con.commit()
		print(f"note [{new_id}] on {args.target} by {author}")
		if superseded:
			print(f"  superseded note(s): {', '.join(f'#{i}' for i in superseded)}")
		live_others = [(nid, tgt, body) for nid, tgt, body in existing
					   if nid not in superseded and not body.startswith("[SUPERSEDED")]
		if live_others:
			print(f"  heads-up: {len(live_others)} other live note(s) on {base}* - reconcile or "
				  f"supersede if any now contradict:")
			for nid, tgt, body in live_others[:8]:
				print(f"    [{nid}] {tgt}: {body[:100]}{'...' if len(body) > 100 else ''}")
	elif args.action == "list":
		if not args.target:
			rows = list(con.execute(
				"SELECT id,target,body,author,tier FROM notes ORDER BY target"))
			aliases, known, ambiguous = [], True, []
		else:
			base = args.target.split("@")[0].strip()
			aliases, known, ambiguous = note_target_aliases(con, args.target)
			rows = note_rows_for_aliases(con, aliases, substring=args.target)
		# Note lines keep their exact original format on STDOUT, so anything parsing
		# this output is unaffected. Everything the fix adds goes to STDERR.
		for nid, target, body, auth, tier in rows:
			who = f" ({auth})" if auth else ""
			tg = f" [{tier}]" if tier else ""
			print(f"[{nid}]{tg} {target}{who}: {body}")
		if args.target:
			if ambiguous:
				print(f"warning: {base} is an AMBIGUOUS name shared by "
					  f"{', '.join(ambiguous)}; results span all of them.",
					  file=sys.stderr)
			if rows:
				# State the question that was actually evaluated. An answer whose
				# scope is invisible is how an empty result gets read as "no prior
				# art exists" when it only ever meant "not under that spelling".
				extra = [a for a in aliases if a != base]
				if extra:
					print(f"note list {args.target}: {len(rows)} note(s) via "
						  f"{len(aliases)} spelling(s): {', '.join(aliases)}",
						  file=sys.stderr)
			elif known:
				# A real symbol that genuinely carries no notes. This is a FACT, and
				# a different one from "no such symbol" - the two must never read the
				# same way.
				print(f"note list: {base} is a KNOWN symbol with 0 notes "
					  f"(searched {len(aliases)} spelling(s): {', '.join(aliases)}).",
					  file=sys.stderr)
			elif note_target_is_symbol_shaped(base):
				print(f"error: no notes, and no such symbol: {args.target}",
					  file=sys.stderr)
				print("  nothing was searched beyond the literal string - this is NOT "
					  "evidence that the symbol has no notes.", file=sys.stderr)
				print("  (run `index` first, or try `find`)", file=sys.stderr)
				return 1
			else:
				print(f"note list: 0 notes whose TARGET matches {args.target!r} "
					  f"(note bodies are not searched).", file=sys.stderr)
	elif args.action == "rm":
		tok = str(args.target or "").strip().lstrip("#")
		if not tok.isdigit():
			print(f"error: note rm takes a numeric note id (got {args.target!r}); "
				  f"use `note list <symbol>` to find it.", file=sys.stderr)
			return 1
		nid = int(tok)
		row = con.execute("SELECT body FROM notes WHERE id=?", (nid,)).fetchone()
		if not row:
			# Previously this printed "removed note N" whether or not the note
			# existed - a confident report of work never done.
			print(f"error: no such note: {nid} (nothing was removed)", file=sys.stderr)
			return 1
		con.execute("DELETE FROM notes WHERE id=?", (nid,))
		# Keep the deleted body in the audit log; the history table exists so a prior
		# claim is never silently lost, and recording None lost it.
		record_history(con, "note", f"note:{nid}", "delete", row[0], None, author)
		con.commit()
		print(f"removed note {nid}")
	return 0


def cmd_seen(args, con):
	"""One-shot triage: label an investigated symbol with a quick hypothesis.

    The frictionless way to discharge investigation debt when you are not yet sure
    what something is - a tier-`guess` note is enough to keep the next engineer
    from treating it as un-RE'd.
    """
	author = get_author(args)
	tier = valid_tier(args.tier)
	body = (args.body or "").strip()
	# Anti-garbage floor: an empty / near-empty triage is fake coverage, which is
	# worse than honest blank. Require a real one-liner.
	if len(body) < 8:
		print("error: write a real one-liner (>=8 chars) - what it does or your best guess.",
			  file=sys.stderr)
		print('  e.g. revtool seen sub_4df5b0 "per-frame cam advance; raw delta, no title curve"',
			  file=sys.stderr)
		return 1
	# `seen` writes a note, so a mistyped symbol files the triage where no lookup
	# will reach it AND leaves the real symbol still reading as un-RE'd.
	_al, sym_known, _amb = note_target_aliases(con, args.symbol)
	if not sym_known and note_target_is_symbol_shaped(args.symbol):
		print(f"warning: {args.symbol} is not in the function index and has no "
			  f"rename - check the spelling, or this triage will not discharge the "
			  f"debt you meant to clear.", file=sys.stderr)
	cur = con.execute(
		"INSERT INTO notes(target,body,author,tier,updated_at) VALUES(?,?,?,?,?)",
		(args.symbol, body, author, tier, now_iso()))
	record_history(con, "note", f"note:{cur.lastrowid}", "set", None, body, author, "triage")
	con.commit()
	print(f"triaged {args.symbol} [{tier}] -> note [{cur.lastrowid}] by {author}")
	remaining = compute_debt(con, author=author)
	print(f"investigation debt remaining (you): {len(remaining)}")
	return 0


def cmd_config(args, con):
	"""Persist tool settings (strict enforcement, exploration budget) in the overlay db."""
	setting = args.setting
	if args.value is None:
		if setting == "strict":
			row = con.execute("SELECT value FROM meta WHERE key='strict'").fetchone()
			current = row[0] if row and row[0] is not None else "on (default)"
			print(f"strict = {current}")
		elif setting == "debt_limit":
			print(f"debt_limit = {debt_limit(con)}")
		elif setting == "binary":
			row = con.execute("SELECT value FROM meta WHERE key='binary'").fetchone()
			cur = row[0] if row and row[0] else f"{DEFAULT_BINARY} (default)"
			print(f"binary = {cur}  ({'present' if os.path.exists(cur.split(' (default')[0]) else 'MISSING'})")
		return 0
	if setting == "binary":
		path = os.path.abspath(args.value)
		if not os.path.exists(path):
			print(f"error: no such file: {path}", file=sys.stderr)
			return 1
		digest = sha256_file(path)
		if digest != TARGET_SHA256:
			print(f"error: PE hash {digest} is not the pinned SRO_Client.exe v1.150 "
				  f"hash {TARGET_SHA256}", file=sys.stderr)
			return 1
		con.execute("INSERT OR REPLACE INTO meta(key,value) VALUES('binary',?)", (path,))
		meta_set(con, "target_sha256", digest)
		record_artifact(con, "target", path, producer="PE file")
		con.commit()
		print(f"binary -> {path}  sha256={digest}")
		return 0
	if setting == "strict":
		val = "on" if args.value.strip().lower() in ("on", "1", "true", "yes") else "off"
		con.execute("INSERT OR REPLACE INTO meta(key,value) VALUES('strict',?)", (val,))
		con.commit()
		print(f"strict -> {val}")
	elif setting == "debt_limit":
		try:
			n = max(1, int(args.value))
		except ValueError:
			print("error: debt_limit must be a positive integer", file=sys.stderr)
			return 1
		con.execute("INSERT OR REPLACE INTO meta(key,value) VALUES('debt_limit',?)", (str(n),))
		con.commit()
		print(f"debt_limit -> {n}")
	return 0


def cmd_audit(args, con):
	"""Scan a file (doc, source, anything) for sub_/data_ tokens that have no label.

    Catches the OTHER big way RE work gets lost: a writeup, a Ghidra/IDA session, or
    a code comment names a pile of functions that were never captured in the overlay.
    This surfaces every symbol mentioned in `path` that revtool still reads as un-RE'd
    so you can port the findings in. Exit code is the number of unlabeled symbols (so
    it can gate CI / pre-commit). `--quiet` prints only the unlabeled list.
    """
	try:
		text = open(args.path, encoding="utf-8", errors="replace").read()
	except OSError as e:
		print(f"error: cannot read {args.path}: {e}", file=sys.stderr)
		return 255
	toks = sorted({m.lower() for m in
				   re.findall(r"\b(?:sub|data)_[0-9a-fA-F]{4,}\b", text)})
	if not toks:
		if not args.quiet:
			print(f"no sub_/data_ symbols found in {args.path}")
		return 0
	unlabeled = []
	for t in toks:
		if not symbol_is_annotated(con, t):
			is_fn = con.execute("SELECT 1 FROM functions WHERE name=?", (t,)).fetchone()
			unlabeled.append((t, "func" if is_fn else "data"))
	if not args.quiet:
		print(f"{args.path}: {len(toks)} unique symbol(s), "
			  f"{len(toks) - len(unlabeled)} labeled, {len(unlabeled)} UNLABELED")
	if unlabeled:
		if not args.quiet:
			print("--- unlabeled (capture these so the work isn't lost) ---")
		for sym, kind in unlabeled:
			print(f"  {sym:<16} ({kind})")
	return len(unlabeled)


def cmd_coverage(args, con):
	"""Report how thoroughly ONE function is RE'd: % of referenced symbols labeled,
    plus structural depth (field offsets named, the function's own label/depth).

    "RE'd" here means the function's *direct* references are captured: every callee it
    invokes and every data global it touches has a label, the function itself is named,
    and its struct accesses are bound/named. Exit code = #unlabeled references (scriptable).
    """
	row = resolve_func(con, args.target)
	if not row:
		print(f"error: function not found: {args.target}", file=sys.stderr)
		return 255
	addr, name, sig, sb, eb, sl, el = row
	dump = get_dump(con, args)
	lines = read_func_lines(dump, sb, eb)
	body = "\n".join(lines)

	toks = set(GLOBAL_TOKEN_RE.findall(body))
	callees = sorted(t for t in toks if t.startswith("sub_") and t != name)
	datasyms = sorted(t for t in toks if t.startswith("data_"))
	refs = callees + datasyms
	unl_callees = [t for t in callees if not symbol_is_annotated(con, t)]
	unl_data = [t for t in datasyms if not symbol_is_annotated(con, t)]
	n_unl = len(unl_callees) + len(unl_data)
	sym_pct = 100.0 if not refs else 100.0 * (len(refs) - n_unl) / len(refs)
	# annotated-but-not-renamed: counts as "labeled" (discharges debt) yet the view still
	# shows the raw token, so it's not actually readable. Surface it separately.
	noteonly = [t for t in refs
				if t not in unl_callees and t not in unl_data
				and not symbol_is_renamed(con, t)]
	n_noteonly = len(noteonly)
	renamed = len(refs) - n_unl - n_noteonly
	read_pct = 100.0 if not refs else 100.0 * renamed / len(refs)

	# structural depth: distinct field offsets and how many are named via a binding here
	offsets = function_field_offsets(lines)
	bindings = load_bindings(con, addr)
	fields_by_type = {t: load_fields(con, t) for t in set(bindings.values())}
	named_off = 0
	unl_offsets = []
	for var, off in sorted(offsets):
		t = bindings.get(var)
		if t and fields_by_type.get(t, {}).get(off):
			named_off += 1
		else:
			unl_offsets.append((var, off, t))
	depth_pct = 100.0 if not offsets else 100.0 * named_off / len(offsets)

	self_named = symbol_is_annotated(con, name)
	self_deep = depth_satisfied(con, name, addr)
	structured = len(offsets) >= STRUCTURE_MIN_OFFSETS
	# a function with < STRUCTURE_MIN_OFFSETS field accesses has no depth obligation:
	# "flat", not "shallow". Don't read absence-of-obligation as a deficiency.
	if self_deep:
		depth_label = "deep"
	elif not structured:
		depth_label = "flat (no struct depth to capture)"
	else:
		depth_label = "SHALLOW (owes a breadcrumb)"
	applied = (load_globals(con).get(name) or [None])[0]

	disp = name + (f" -> {applied}" if applied else "")
	print(f"coverage of {disp} @ 0x{addr}")
	print(f"  self:        {'named' if self_named else 'UNNAMED'} / {depth_label}")
	print(f"  callees:     {len(callees) - len(unl_callees)}/{len(callees)} labeled")
	print(f"  data globals:{len(datasyms) - len(unl_data)}/{len(datasyms)} labeled")
	print(f"  field depth: {named_off}/{len(offsets)} offsets named ({depth_pct:.0f}%)")
	print(f"  SYMBOL COVERAGE: {sym_pct:.0f}%  ({len(refs) - n_unl}/{len(refs)} labeled)")
	print(f"  READABLE:        {read_pct:.0f}%  ({renamed}/{len(refs)} renamed; "
		  f"{n_noteonly} note-only)")
	self_ok = self_named and (self_deep or not structured)
	all_named = (n_unl == 0) and self_ok and named_off == len(offsets)
	if all_named and n_noteonly == 0:
		verdict = "FULLY REd"
	elif all_named:
		verdict = f"covered, but {n_noteonly} ref(s) note-only -> raw token still in view"
	else:
		verdict = "incomplete"
	print(f"  VERDICT: {verdict}")
	if unl_callees:
		print("  unlabeled callees:")
		for t in unl_callees:
			print(f"    {t}")
	if unl_data:
		print("  unlabeled data:")
		for t in unl_data:
			print(f"    {t}")
	if noteonly:
		print("  labeled but NOT renamed (view still shows the raw token):")
		for t in noteonly:
			print(f"    {t}")
	if unl_offsets:
		print("  unnamed field offsets:")
		for var, off, t in unl_offsets:
			where = f"{var} : {t}" if t else f"{var} (unbound - bind it to a type)"
			print(f"    +0x{off:x}   {where}")
	return n_unl


# --------------------------------------------------------------------------- #
# confidence / dispute / history / review
# --------------------------------------------------------------------------- #
def cmd_mark(args, con):
	"""Set the confidence tier of an existing claim."""
	author = get_author(args)
	tier = valid_tier(args.tier)
	claim = locate_claim(con, args.target)
	if not claim:
		print(f"error: claim not found: {args.target}", file=sys.stderr)
		print("  forms: global:NAME | local:FUNC:orig | field:TYPE:0xoff | bind:FUNC:var | note:ID | assert:ID",
			  file=sys.stderr)
		return 1
	# Same depth-on-confidence gate as `rename`: promoting a function claim to a
	# confident tier must capture some structure (or pass --shallow).
	if claim["kind"] == "global":
		gate = depth_gate(con, args, get_dump(con, args), claim["label"].split()[0], tier)
		if gate is not None:
			return gate
	# Guardrail: don't let an absence/inferred claim be promoted to a top tier. Use the
	# stored basis, or one supplied on this command.
	basis = args.basis
	if basis is None and "basis" in {r[1] for r in con.execute(
			f"PRAGMA table_info({claim['table']})")}:
		brow = con.execute(
			f"SELECT basis FROM {claim['table']} WHERE {claim['where']}", claim["params"]).fetchone()
		basis = brow[0] if brow else None
	enforce_basis_tier(basis, tier)
	old = con.execute(
		f"SELECT tier FROM {claim['table']} WHERE {claim['where']}", claim["params"]).fetchone()
	sets = "tier=?, author=?, updated_at=?"
	vals = [tier, author, now_iso()]
	if args.evidence is not None:
		sets += ", evidence=?"
		vals.append(args.evidence)
	if args.basis is not None:
		sets += ", basis=?"
		vals.append(valid_basis(args.basis))
	con.execute(f"UPDATE {claim['table']} SET {sets} WHERE {claim['where']}",
				vals + claim["params"])
	record_history(con, claim["kind"], claim["ckey"], "tier",
				   old[0] if old else None, tier, author, args.evidence)
	con.commit()
	print(f"{claim['label']}: tier -> {tier} by {author}")
	return 0


def cmd_dispute(args, con):
	"""Flag a claim as wrong/suspect WITHOUT deleting it (stays visible)."""
	author = get_author(args)
	claim = locate_claim(con, args.target)
	if not claim or claim["kind"] == "note":
		print(f"error: cannot dispute: {args.target} "
			  f"(disputable: global/local/field/bind/assert)", file=sys.stderr)
		return 1
	con.execute(
		f"UPDATE {claim['table']} SET disputed=1, dispute_reason=?, author=?, updated_at=? "
		f"WHERE {claim['where']}",
		[args.reason, author, now_iso()] + claim["params"])
	record_history(con, claim["kind"], claim["ckey"], "dispute", None, None, author, args.reason)
	con.commit()
	print(f"!! DISPUTED {claim['label']}: {args.reason} (by {author})")
	print("   (still visible in `view`; use `resolve` once corrected)")
	return 0


def cmd_resolve(args, con):
	"""Clear a dispute, optionally re-tiering the claim."""
	author = get_author(args)
	claim = locate_claim(con, args.target)
	if not claim:
		print(f"error: claim not found: {args.target}", file=sys.stderr)
		return 1
	sets = "disputed=0, dispute_reason=NULL, author=?, updated_at=?"
	vals = [author, now_iso()]
	if args.tier:
		sets += ", tier=?"
		vals.append(valid_tier(args.tier))
	con.execute(f"UPDATE {claim['table']} SET {sets} WHERE {claim['where']}",
				vals + claim["params"])
	record_history(con, claim["kind"], claim["ckey"], "resolve", None, args.tier, author)
	con.commit()
	print(f"resolved {claim['label']}" + (f" -> [{args.tier}]" if args.tier else ""))
	return 0


def cmd_history(args, con):
	"""Show the audit trail for a claim (or recent global activity)."""
	if args.target:
		claim = locate_claim(con, args.target)
		ckey = claim["ckey"] if claim else args.target
		rows = con.execute(
			"SELECT ts,author,action,old,new,reason FROM history WHERE ckey=? ORDER BY id",
			(ckey,)).fetchall()
		print(f"history for {ckey}:")
		if not rows and claim is None and note_target_is_symbol_shaped(args.target):
			# The ckey fallback means an unresolvable target queries a key that cannot
			# exist, and "(none)" then reads as "this claim has no history". Those are
			# different facts, and this is the audit trail - the one place where a
			# silently empty answer is least acceptable.
			print(f"error: no claim matches {args.target!r} - this is an empty LOOKUP, "
				  f"not an empty history.", file=sys.stderr)
			print("  (use the sub_/data_ token, the applied name, an address, or an "
				  "explicit form: global:NAME | note:ID)", file=sys.stderr)
			return 1
	else:
		rows = con.execute(
			"SELECT ts,author,action,old,new,reason FROM history ORDER BY id DESC LIMIT ?",
			(args.limit,)).fetchall()
		rows = list(reversed(rows))
		print(f"recent activity (last {args.limit}):")
	if not rows:
		print("  (none)")
		return 0
	for ts, author, action, old, new, reason in rows:
		change = ""
		if old is not None or new is not None:
			change = f"  {old!r} -> {new!r}"
		rs = f"  // {reason}" if reason else ""
		print(f"  {ts}  {author or '?':<12} {action}{change}{rs}")
	return 0


def cmd_review(args, con):
	"""List claims needing attention: low tier, disputed, by author, or needs-dynamic."""
	where_extra = ""
	if getattr(args, "debt", False):
		debt = compute_debt(con)
		print("=== INVESTIGATION DEBT (investigated via view/xref, not yet labeled) ===")
		if not debt:
			print("  (none - every investigated symbol carries at least a hypothesis)")
		for sym, kind, count, last_ts, author in debt:
			print(f"  {sym:<26} {kind:<6} x{count:<3} last {last_ts or '?'} by {author or '?'}")
		# Callers an xref revealed that nobody has reviewed yet (non-blocking backlog).
		# `debt` can be large; membership-test it as a set, not a per-caller linear scan
		# (this loop runs once per caller row - tens of thousands on a mature DB).
		debt_syms = {d[0] for d in debt}
		pending = []
		for sym, count in con.execute(
				"SELECT symbol,count FROM access_log WHERE kind='caller' ORDER BY last_ts"):
			if sym not in debt_syms and not symbol_is_annotated(con, sym):
				pending.append((sym, count))
		if pending:
			print("\n=== CALLERS revealed by xref, not yet reviewed ===")
			for sym, count in pending[:40]:
				print(f"  {sym:<26} seen x{count}")
			if len(pending) > 40:
				print(f"  ... {len(pending) - 40} more")
		return 0
	if getattr(args, "needs_dynamic", False):
		# The standing "things that might be lying to us" report: high-tier claims that
		# rest on absence/inference and were NEVER confirmed at runtime. The data_f0673f
		# miss would have been the headline here.
		print("=== high-confidence claims NOT confirmed dynamically (review these) ===")
		any_hit = False
		# Assertions: open/refuted, or weak-basis at high tier.
		for aid, subj, claim, basis, tier, status, blind in con.execute(
			"SELECT id,subject,claim,basis,tier,status,blindspots FROM assertions "
			"WHERE status!='confirmed' ORDER BY status DESC, id"):
			any_hit = True
			bs = f"  blindspots: {blind}" if blind else ""
			print(f"  assert #{aid} [{tier}/{basis}/{status}] {subj}: {claim}{bs}")
		# Renames/fields recorded with a weak basis at a high tier.
		for kind, table, keyexpr in (
			("global", "globals", "'global:'||orig_name"),
			("field", "fields", "'field:'||type_name||':0x'||printf('%x',offset)"),
		):
			for ck, new, basis, tier in con.execute(
				f"SELECT {keyexpr},new_name,basis,tier FROM {table} "
				f"WHERE basis IN ('absence','inferred') "
				f"AND tier IN ('pipeline','verified')"):
				any_hit = True
				print(f"  {ck} (= {new}) [{tier}/{basis}] -- weak basis at high tier")
		if not any_hit:
			print("  (none - every high-confidence claim is observed or dynamically confirmed)")
		return 0
	if args.disputed:
		print("=== DISPUTED claims ===")
		rows = []
		for kind, table, keyexpr in (
			("global", "globals", "'global:'||orig_name"),
			("local", "locals", "'local:'||func_addr||':'||orig_name"),
			("field", "fields", "'field:'||type_name||':0x'||printf('%x',offset)"),
		):
			for ck, new, reason, auth in con.execute(
				f"SELECT {keyexpr},new_name,dispute_reason,author FROM {table} WHERE disputed=1"):
				print(f"  {ck}  (= {new}) by {auth or '?'}: {reason or 'see history'}")
				rows.append(ck)
		for aid, subj, claim, reason, auth in con.execute(
				"SELECT id,subject,claim,dispute_reason,author FROM assertions WHERE disputed=1"):
			print(f"  assert:{aid}  ({subj}: {claim}) by {auth or '?'}: {reason or 'see history'}")
			rows.append(f"assert:{aid}")
		if not rows:
			print("  (none)")
		return 0

	tier = args.tier or "guess"
	valid_tier(tier)
	print(f"=== claims at tier '{tier}'" +
		  (f" by {args.by_author}" if args.by_author else "") + " ===")
	auth_clause = " AND author=?" if args.by_author else ""
	auth_p = [args.by_author] if args.by_author else []
	any_hit = False
	for kind, table, keyexpr in (
		("global", "globals", "'global:'||orig_name"),
		("local", "locals", "'local:'||func_addr||':'||orig_name"),
		("field", "fields", "'field:'||type_name||':0x'||printf('%x',offset)"),
	):
		for ck, new, auth, upd in con.execute(
			f"SELECT {keyexpr},new_name,author,updated_at FROM {table} "
			f"WHERE tier=?{auth_clause} ORDER BY updated_at", [tier] + auth_p):
			print(f"  {ck}  (= {new}) by {auth or '?'}  {upd or ''}")
			any_hit = True
	if not any_hit:
		print("  (none)")
	return 0


# --------------------------------------------------------------------------- #
# assertions / observations  (the static<->dynamic reconciliation loop)
# --------------------------------------------------------------------------- #
def reconcile_assertions(con, subject, value, source, author):
	"""Check every assertion on `subject` against an observed `value`.

    Confirms or REFUTES each machine-checkable assertion; a refuted assertion is also
    auto-disputed so it screams in `view`/`review`. Returns a list of result dicts.
    """
	results = []
	rows = con.execute(
		"SELECT id,claim,predicate,basis,status FROM assertions WHERE subject=?",
		(subject,)).fetchall()
	for aid, claim, predicate, basis, status in rows:
		matches, detail = eval_predicate(predicate, value)
		if matches is None:
			results.append(dict(id=aid, verdict="needs-human", detail=detail, claim=claim))
			con.execute(
				"UPDATE assertions SET observed=?, source=?, updated_at=? WHERE id=?",
				(str(value), source, now_iso(), aid))
			continue
		new_status = "confirmed" if matches else "refuted"
		if matches:
			con.execute(
				"UPDATE assertions SET status=?, observed=?, source=?, basis='dynamic', "
				"updated_at=? WHERE id=?",
				(new_status, str(value), source, now_iso(), aid))
			record_history(con, "assert", f"assert:{aid}", "resolve", status, "confirmed",
						   author, f"observed {subject}={value}: {detail}")
		else:
			reason = f"runtime {subject}={value} contradicts predicate {predicate!r} ({detail})"
			con.execute(
				"UPDATE assertions SET status='refuted', observed=?, source=?, disputed=1, "
				"dispute_reason=?, updated_at=? WHERE id=?",
				(str(value), source, reason, now_iso(), aid))
			record_history(con, "assert", f"assert:{aid}", "dispute", status, "refuted",
						   author, reason)
		results.append(dict(id=aid, verdict=new_status, detail=detail, claim=claim))
	return results
