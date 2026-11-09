"""
===========================================================================
revtool_overlay.py - overlay loaders, note banners, rename application and view/find/xref

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
import re
import textwrap

from revtool_debt import investigation_gate, print_debt_footer
from revtool_debt import record_access, warn_if_previously_investigated
from revtool_dump import get_dump, iter_dump_symbol_hits, norm_addr
from revtool_dump import read_func_lines, require_dump, resolve_func
import revtool_dump
from revtool_dump import resolve_scan_symbol, symbol_is_annotated
from revtool_store import get_author
from revtool_syntax import IDENT_RE, TIER_BADGE, field_accesses

def load_globals(con):
	# tuple: (new, kind, note, tier, disputed, dispute_reason)
	return {r[0]: (r[1], r[2], r[3], r[4], r[5], r[6]) for r in con.execute(
		"SELECT orig_name,new_name,kind,note,tier,disputed,dispute_reason FROM globals")}


def load_locals(con, func_addr):
	# tuple: (new, vtype, note, tier, disputed, dispute_reason)
	return {r[0]: (r[1], r[2], r[3], r[4], r[5], r[6]) for r in con.execute(
		"SELECT orig_name,new_name,vtype,note,tier,disputed,dispute_reason "
		"FROM locals WHERE func_addr=?", (func_addr,))}


def load_bindings(con, func_addr):
	return {r[0]: r[1] for r in con.execute(
		"SELECT var_name,type_name FROM bindings WHERE func_addr=? AND disputed=0",
		(func_addr,))}


def load_fields(con, type_name):
	# tuple: (new, ftype, note, tier, disputed, dispute_reason)
	return {r[0]: (r[1], r[2], r[3], r[4], r[5], r[6]) for r in con.execute(
		"SELECT offset,new_name,ftype,note,tier,disputed,dispute_reason "
		"FROM fields WHERE type_name=?", (type_name,))}


def load_line_notes(con, func_addr, func_name):
	"""Notes attached to addresses inside this function -> {addr: [bodies]}."""
	out = {}
	like1 = func_name + "@%"
	for target, body in con.execute(
		"SELECT target,body FROM notes WHERE target LIKE ? OR target GLOB ?",
		(like1, "[0-9a-f][0-9a-f][0-9a-f][0-9a-f]*"),
	):
		addr = None
		if "@" in target:
			addr = norm_addr(target.split("@", 1)[1])
		else:
			addr = norm_addr(target)
		if addr:
			out.setdefault(addr, []).append(body)
	return out


_TIER_RANK = {"verified": 0, "pipeline": 1, "mechanism": 2, "guess": 3,
			  "unknown": 4, None: 5, "": 5}


def collect_related_notes(con, targets):
	"""Symbol-level notes (notes table) whose target is any symbol this function
    touches: the function itself, the types bound here, the fields it accesses, and
    the globals/locals used. EXCLUDES @addr line notes (shown inline in the body).

    This is the anti-burial mechanism: a wrong/important note on a *field* or *global*
    (e.g. m_objDistance) now surfaces the moment you `view` any function that uses it,
    instead of hiding until someone runs `note list`.

    Returns list of (target, tier, body) sorted verified-first.
    """
	targets = sorted({t for t in targets if t})
	if not targets:
		return []
	rows = con.execute(
		"SELECT target, tier, body FROM notes WHERE target IN (%s) "
		"AND target NOT LIKE '%%@%%'" % ",".join("?" for _ in targets),
		targets).fetchall()
	rows.sort(key=lambda r: (_TIER_RANK.get(r[1], 5), r[0]))
	return rows


def render_notes_banner(rows, field_notes):
	"""Loud, unmissable NOTES box for `view`. `rows` = [(target,tier,body)],
    `field_notes` = [(label, tier, body)]. Prints nothing if both empty."""
	items = [(t, tier, b) for (t, tier, b) in rows]
	items += [(lbl, tier, b) for (lbl, tier, b) in field_notes]
	if not items:
		return
	bar = "=" * 78
	disputed = sum(1 for _, tier, b in items
				   if tier in (None, "", "guess", "unknown")
				   or "CORRECTION" in (b or "") or "WRONG" in (b or "").upper())
	head = f">>> NOTES ({len(items)}) on this function & everything it touches - READ BEFORE TRUSTING THE CODE"
	print(head)
	if disputed:
		print(f"    ({disputed} low-confidence/correction note(s) below - verify, don't assume)")
	print("-" * 78)
	for target, tier, body in items:
		tag = f"[{tier or '?'}]"
		for j, seg in enumerate(textwrap.wrap(body or "", 92) or [""]):
			if j == 0:
				print(f"  * {tag} {target}:")
				print(f"      {seg}")
			else:
				print(f"      {seg}")
	print(bar)


# --------------------------------------------------------------------------- #
# function lookup
# --------------------------------------------------------------------------- #
def build_rename_map(globals_map, locals_map):
	"""Combined original-token -> new-name map (globals + this func's locals)."""
	rename = {}
	for orig, vals in globals_map.items():
		rename[orig] = vals[0]
	for orig, vals in locals_map.items():
		rename[orig] = vals[0]  # locals win over globals on collision
	return rename


def apply_renames(code, rename):
	if not rename:
		return code
	def repl(m):
		tok = m.group(0)
		return rename.get(tok, tok)
	return IDENT_RE.sub(repl, code)


def annotate_fields(code, bindings, fields_by_type, rename):
	"""Return a list of 'var->field [tier]' annotations for fields on this line.

    Handles both decompiler shapes: displacement `var + 0xNN` (byte offset) and index
    `var[0xNN]` (dword index -> byte offset = idx*4). The index*4 assumption only ever
    *adds* an annotation when it lands exactly on a defined byte-offset field, so a
    non-dword array index simply misses rather than mislabels.
    """
	hits = []
	seen = set()

	def emit(var, off):
		tname = bindings.get(var)
		if not tname:
			return
		fmap = fields_by_type.get(tname)
		if not fmap:
			return
		fld = fmap.get(off)  # (new, ftype, note, tier, disputed, reason)
		if not fld:
			return
		key = (var, off)
		if key in seen:
			return
		seen.add(key)
		shown_var = rename.get(var, var)
		badge = TIER_BADGE.get(fld[3], "?")
		if fld[4]:  # disputed
			hits.append(f"{shown_var}->{fld[0]} [!DISPUTED: {fld[5] or 'see history'}]")
		else:
			hits.append(f"{shown_var}->{fld[0]} [{badge}]")

	for var, off in field_accesses(code):
		emit(var, off)
	return hits


def cmd_view(args, con):
	dump = get_dump(con, args)
	row = resolve_func(con, args.target)
	if not row:
		print(f"error: function not found: {args.target}", file=sys.stderr)
		print("  (run `index` first, or try `find`)", file=sys.stderr)
		return 1
	addr, name, sig, sb, eb, sl, el = row
	gate = investigation_gate(con, args, name)
	if gate is not None:
		return gate
	lines = read_func_lines(dump, sb, eb)

	# optional --lines A:B filter on dump line numbers
	lo_line, hi_line = None, None
	if getattr(args, "lines", None):
		parts = args.lines.split(":")
		lo_line = int(parts[0]) if parts[0] else None
		hi_line = int(parts[1]) if len(parts) > 1 and parts[1] else None
		# Warn early if the requested window does not overlap this function, so a
		# mistyped range doesn't look like an empty/broken function.
		if (lo_line is not None and lo_line > el) or (hi_line is not None and hi_line < sl):
			print(f"warning: --lines {args.lines} is outside {name} "
				  f"(dump lines {sl}-{el}); nothing to show.", file=sys.stderr)

	def in_range(i):
		n = sl + i  # dump line number of lines[i]
		if lo_line is not None and n < lo_line:
			return False
		if hi_line is not None and n > hi_line:
			return False
		return True

	# Only charge investigation debt when the view actually rendered something. An
	# out-of-range --lines window shows nothing, and you can't label what you never saw.
	any_shown = any(in_range(i) for i in range(len(lines)))

	# header / overlay summary
	globals_map = load_globals(con)
	g_self = globals_map.get(name)
	applied_name = g_self[0] if g_self else None

	if args.raw:
		if any_shown:
			warn_if_previously_investigated(con, name, stream=sys.stderr)
		print("\n".join(ln for i, ln in enumerate(lines) if in_range(i)))
		if any_shown:
			record_access(con, name, "viewed", get_author(args))
			con.commit()
			print_debt_footer(con, name, author=get_author(args))
		return 0

	locals_map = load_locals(con, addr)
	bindings = load_bindings(con, addr)
	types_needed = set(bindings.values())
	fields_by_type = {t: load_fields(con, t) for t in types_needed}
	rename = build_rename_map(globals_map, locals_map)
	line_notes = load_line_notes(con, addr, name)

	# --- anti-burial note collection -------------------------------------- #
	# Gather notes on EVERYTHING this function touches (itself, the types bound
	# here, their fields, and the globals/locals it references) so a note on a
	# field/global (e.g. the old m_objDistance "view-depth" claim) can no longer
	# hide until someone runs `note list`. Previously `view` only showed notes
	# whose target was the function name.
	body_text = "\n".join(ln for i, ln in enumerate(lines) if in_range(i))
	body_tokens = set(re.findall(r"[A-Za-z_]\w*", body_text))
	note_targets = {name, applied_name}
	note_targets |= set(types_needed)
	field_note_items = []  # (label, tier, body) from the fields.note column
	for tname, fmap in fields_by_type.items():
		for off, (fnew, ftype, fnote, ftier, fdisp, freason) in fmap.items():
			note_targets.add(fnew)
			if fnote:
				field_note_items.append((f"{tname}+0x{off:x} {fnew}", ftier, fnote))
	for orig, vals in {**globals_map, **locals_map}.items():
		if orig in body_tokens or (vals[0] and vals[0] in body_tokens):
			note_targets.add(orig)
			note_targets.add(vals[0])
	related_notes = collect_related_notes(con, note_targets)

	# Header banner
	print("=" * 78)
	title = name + (f"  ->  {applied_name}" if applied_name else "")
	tier_tag = f"  [{g_self[3]}]" if g_self else ""
	print(f"{title}   @ 0x{addr}   (lines {sl}-{el}){tier_tag}")
	if g_self and g_self[4]:
		print(f"  !! DISPUTED: {g_self[5] or 'see `revtool history`'}")
	if g_self and g_self[2]:
		print(f"  role: {g_self[2]}")
	if bindings:
		b = ", ".join(f"{v}:{t}" for v, t in sorted(bindings.items()))
		print(f"  bindings: {b}")
	# Surface any disputed symbol used in this function up front.
	disputed_used = []
	for orig, vals in {**{k: v for k, v in globals_map.items()},
					   **{k: v for k, v in locals_map.items()}}.items():
		if vals[4] and re.search(r"\b" + re.escape(orig) + r"\b", "\n".join(lines)):
			disputed_used.append((orig, vals[0], vals[5]))
	if disputed_used:
		print("  ---- DISPUTED symbols used here (do not trust): ----")
		for orig, new, reason in disputed_used:
			print(f"     {new} <- {orig}: {reason or 'see history'}")
	if any_shown:
		warn_if_previously_investigated(con, name)
	print("=" * 78)

	# Loud NOTES box: prior RE on this function and everything it references.
	render_notes_banner(related_notes, field_note_items)

	# Body
	used = {}
	for i, ln in enumerate(lines):
		if not in_range(i):
			continue
		m = re.match(r"^([0-9a-fA-F]{8})(\s+)(.*)$", ln)
		if not m:
			print(ln)
			continue
		laddr, ws, code = m.group(1).lower(), m.group(2), m.group(3)
		field_hits = annotate_fields(code, bindings, fields_by_type, rename)
		shown = apply_renames(code, rename) if not args.no_apply else code
		# track which renames were actually used (for legend)
		if not args.no_apply:
			for orig in rename:
				if re.search(r"\b" + re.escape(orig) + r"\b", code):
					used[orig] = rename[orig]
		suffix = ""
		ann = []
		if field_hits:
			ann.extend(field_hits)
		if laddr in line_notes:
			ann.extend("NOTE: " + n for n in line_notes[laddr])
		if ann:
			suffix = "    // " + " | ".join(ann)
		print(f"{laddr}{ws}{shown}{suffix}")

	# Legend (with tier + dispute markers)
	if used and not args.no_legend:
		print("-" * 78)
		print("renamed in this view (new <- orig  [tier]):")
		for orig, new in sorted(used.items(), key=lambda kv: kv[1]):
			vals = globals_map.get(orig) or locals_map.get(orig)
			if vals and vals[4]:
				mark = f"[!DISPUTED: {vals[5] or 'see history'}]"
			else:
				mark = f"[{vals[3]}]" if vals else ""
			print(f"  {new} <- {orig}  {mark}")
	if not args.raw:
		print("-" * 78)
		print("(use --raw for the pristine original)")
	if any_shown:
		record_access(con, name, "viewed", get_author(args))
		con.commit()
		print_debt_footer(con, name, author=get_author(args))
	return 0


# --------------------------------------------------------------------------- #
# find
# --------------------------------------------------------------------------- #
def cmd_find(args, con):
	pat = args.pattern.lower()
	rows = con.execute(
		"SELECT f.addr,f.name,f.signature FROM functions f "
		"LEFT JOIN globals g ON g.orig_name=f.name "
		"WHERE lower(f.name) LIKE ? OR lower(f.signature) LIKE ? "
		"OR lower(IFNULL(g.new_name,'')) LIKE ? "
		"ORDER BY f.addr",
		(f"%{pat}%", f"%{pat}%", f"%{pat}%"),
	).fetchall()
	if not rows:
		print("(no matches)")
		return 0
	gmap = load_globals(con)
	for addr, name, sig in rows[: args.limit]:
		applied = gmap.get(name)
		tag = f"  [{applied[0]}]" if applied else ""
		# Notes badge so RE'd functions advertise their notes at search time
		# (target = name, applied name, or any <name>@addr line note).
		ncount = con.execute(
			"SELECT COUNT(*) FROM notes WHERE target=? OR target=? OR target LIKE ?",
			(name, applied[0] if applied else name, name + "@%")).fetchone()[0]
		badge = f"  <{ncount} note{'s' if ncount != 1 else ''} - `view`/`note list {name}`>" if ncount else ""
		print(f"0x{addr}  {name}{tag}{badge}")
		print(f"    {sig.strip()}")
	if len(rows) > args.limit:
		print(f"... {len(rows) - args.limit} more (use --limit)")
	return 0


# --------------------------------------------------------------------------- #
# xref  (fast scan over the whole dump via mmap)
# --------------------------------------------------------------------------- #
def cmd_xref(args, con):
	dump = get_dump(con, args)
	require_dump(dump)
	gmap = load_globals(con)
	sym = args.symbol
	# Resolve an applied (renamed) name or a bare address back to the dump's ORIGINAL
	# token, so `xref <newname>` and `xref 0x401000` behave like `view` does. Shared
	# with the debt ledger so both agree on what a target denotes.
	scan_sym = resolve_scan_symbol(con, sym)
	gate = investigation_gate(con, args, scan_sym)
	if gate is not None:
		return gate
	# function start offsets, file order, for "which function" mapping
	frows = con.execute(
		"SELECT start_byte,addr,name FROM functions ORDER BY start_byte").fetchall()
	starts = [r[0] for r in frows]

	hits = []
	for line_start, line in iter_dump_symbol_hits(dump, scan_sym):
		fi = bisect.bisect_right(starts, line_start) - 1
		fname = frows[fi][2] if 0 <= fi < len(frows) else "?"
		laddr = line[:8].decode("ascii", "replace")
		text = line.decode("utf-8", "replace")
		# strip the leading "<addr><spaces>" so we don't print it twice
		tm = re.match(r"^[0-9a-fA-F]{8}\s+", text)
		text = text[tm.end():] if tm else text.strip()
		hits.append((laddr, fname, text))

	applied = gmap.get(scan_sym)
	label = scan_sym + (f" ({applied[0]})" if applied else "")
	if scan_sym != sym:
		label = f"{sym} -> {label}"
	print(f"xref {label}: {len(hits)} line(s)")
	# Can the overlay identify this target as something labelable? A canonical
	# sub_/data_/loc_/jump_table_ hit proves that; arbitrary text hits remain useful
	# xref results but are search probes, not rename debt. The scan above IS the dump
	# arm of that test, so its result is passed in rather than repeated - and the
	# ledger asks the same question through the same function.
	identified = revtool_dump.symbol_is_identifiable(con, scan_sym, dump,
										found_in_dump=bool(hits), consult_dump=False)
	if not identified:
		if hits:
			print(f"note: {scan_sym} matched dump text but is not a labelable anonymous "
				  f"symbol; treating it as a research query, not rename debt.",
				  file=sys.stderr)
		else:
			# Zero hits for a symbol the overlay cannot identify means the scan never
			# had a valid symbol needle. That is not "nothing references this".
			print(f"warning: {scan_sym} is not a known symbol, so 0 lines is NOT "
				  f"evidence that it is unreferenced - check the spelling (`find`).",
				  file=sys.stderr)
	if identified:
		warn_if_previously_investigated(con, scan_sym)
	last_fn = None
	for laddr, fname, text in hits[: args.limit]:
		fn_new = gmap.get(fname)
		fn_disp = fname + (f"/{fn_new[0]}" if fn_new else "")
		if fname != last_fn:
			tag = ""
			if fname not in ("?", scan_sym):
				tag = "  [labeled]" if symbol_is_annotated(con, fname) else "  [NEEDS RE]"
			print(f"\n  in {fn_disp}:{tag}")
			last_fn = fname
		print(f"    {laddr}  {text}")
	if len(hits) > args.limit:
		print(f"\n... {len(hits) - args.limit} more (use --limit)")

	# Ledger: record this xref and surface the callers it revealed as discovered
	# work, so a caller never silently reads as "never RE'd". Unlabeled callers
	# land in `review --debt`; this xref itself is debt until `scan_sym` is labeled.
	#
	# BUT NOT FOR A TARGET THAT IS NOT A LABELABLE SYMBOL. Charging debt against a
	# text query creates a row that cannot be discharged by labeling the program -
	# and then the footer demands a name for an opcode, enum, path, or string key.
	# Keep the xref results and access history, but do not bill the probe.
	author = get_author(args)
	if not identified:
		print(f"\nno investigation debt charged: {scan_sym} is a search probe, not an "
			  f"anonymous program symbol; there is nothing to label.",
			  file=sys.stderr)
		return 0
	record_access(con, scan_sym, "xref", author)
	callers = []
	for _laddr, fname, _text in hits:
		if fname in ("?", scan_sym) or fname in callers:
			continue
		callers.append(fname)
	n_unlabeled = 0
	for fname in callers:
		if not symbol_is_annotated(con, fname):
			n_unlabeled += 1
			record_access(con, fname, "caller", author)
	con.commit()
	if callers:
		print(f"\ncallers: {len(callers)}  |  unlabeled (need RE): {n_unlabeled}")
	print_debt_footer(con, scan_sym, author=get_author(args))
	return 0


# --------------------------------------------------------------------------- #
# rename / type / field / bind / note
# --------------------------------------------------------------------------- #
