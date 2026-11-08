"""
===========================================================================
revtool_d3d.py - cmd_d3dstate: the fixed-function D3D9 state timeline

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

from revtool_dump import get_dump, norm_addr, read_func_lines, resolve_func
from revtool_overlay import apply_renames, build_rename_map, load_globals
from revtool_overlay import load_locals
from revtool_pe import D3D9_VTBL, D3DBLEND_NAMES, D3DPT_NAMES, D3DRS_NAMES
from revtool_pe import D3DTA_NAMES, D3DTOP_NAMES, D3DTSS_NAMES, D3DTS_NAMES
from revtool_pe import _D3DRS_VALUE_DECODER, _D3DTSS_ARG_TYPES
from revtool_pe import _D3DTSS_OP_TYPES

def _dstate_rs_value(sid, val_tok):
	"""Decode an enum-valued render state's value, e.g. SRCBLEND 1 -> '1 (ZERO)'."""
	v = _dstate_int(val_tok)
	dec = _D3DRS_VALUE_DECODER.get(sid)
	if v is not None and dec and v in dec:
		return f"{val_tok} ({dec[v]})"
	return val_tok


def _dstate_ta(value):
	"""Decode a D3DTA argument selector incl. modifier bits."""
	base = D3DTA_NAMES.get(value & 0xf, f"0x{value & 0xf:x}")
	mods = ("|COMPLEMENT" if value & 0x10 else "") + ("|ALPHAREPLICATE" if value & 0x20 else "")
	return base + mods


def _dstate_tss(argl):
	"""Decode (stage, type, value) of a SetTextureStageState arg list (last 3 args).
    Returns (text, meta) where meta = (stage:int|None, type:int|None, value:int|None)."""
	if len(argl) < 3:
		return ", ".join(argl), (None, None, None)
	stage_tok, typ_tok, val_tok = argl[-3], argl[-2], argl[-1]
	stage, typ, val = _dstate_int(stage_tok), _dstate_int(typ_tok), _dstate_int(val_tok)
	tname = D3DTSS_NAMES.get(typ, typ_tok)
	if val is not None and typ in _D3DTSS_OP_TYPES:
		vtext = f"{val_tok} ({D3DTOP_NAMES.get(val, '?')})"
	elif val is not None and typ in _D3DTSS_ARG_TYPES:
		vtext = f"{val_tok} ({_dstate_ta(val)})"
	else:
		vtext = val_tok
	return f"stage{stage_tok}, {tname} = {vtext}", (stage, typ, val)


def _dstate_blend_summary(rs_vals, tss_vals):
	"""Human conclusion lines derived from the accumulated blend/stage state."""
	out = []
	src, dst = rs_vals.get(19), rs_vals.get(20)
	if rs_vals.get(27) == 1 and src is not None and dst is not None:
		s, d = D3DBLEND_NAMES.get(src, f"0x{src:x}"), D3DBLEND_NAMES.get(dst, f"0x{dst:x}")
		eq = f"framebuffer = src*{s} + dest*{d}"
		if (src, dst) == (1, 3):
			eq += "   == framebuffer *= srcColor  (MULTIPLICATIVE darken/tint - NOT alpha blending)"
		elif (src, dst) == (5, 6):
			eq += "   (standard SRCALPHA/INVSRCALPHA alpha blend)"
		elif (src, dst) == (2, 2):
			eq += "   (ADDITIVE)"
		elif (src, dst) == (9, 1):
			eq += "   == framebuffer *= destColor swap form (multiplicative)"
		out.append(eq)
	cop = tss_vals.get((0, 1))
	if cop is not None:
		a1 = tss_vals.get((0, 2)); a2 = tss_vals.get((0, 3))
		opn = D3DTOP_NAMES.get(cop, f"0x{cop:x}")
		a1s = _dstate_ta(a1) if a1 is not None else "CURRENT(default arg1=TEXTURE)"
		a2s = _dstate_ta(a2) if a2 is not None else "CURRENT(default)"
		if cop == 2:
			out.append(f"stage0 colour = {a1s}  (SELECTARG1: arg2 ignored)")
		elif cop == 3:
			out.append(f"stage0 colour = {a2s}  (SELECTARG2: arg1 ignored)")
		elif cop == 1:
			out.append("stage0 colour op DISABLED")
		else:
			out.append(f"stage0 colour = {a1s} {opn} {a2s}")
	aop = tss_vals.get((0, 4))
	if aop is not None:
		b1 = tss_vals.get((0, 5)); b2 = tss_vals.get((0, 6))
		opn = D3DTOP_NAMES.get(aop, f"0x{aop:x}")
		b1s = _dstate_ta(b1) if b1 is not None else "(default)"
		if aop == 2:
			out.append(f"stage0 alpha  = {b1s}")
		elif aop == 1:
			out.append("stage0 alpha op DISABLED")
		else:
			b2s = _dstate_ta(b2) if b2 is not None else "(default)"
			out.append(f"stage0 alpha  = {b1s} {opn} {b2s}")
	if 60 in rs_vals:
		out.append("TEXTUREFACTOR written in this function (TFACTOR args read it)")
	return out

_DSTATE_LINE_ADDR = re.compile(r"^([0-9a-f]{8})\s")
_DSTATE_VTBL = re.compile(r"\+ 0x([0-9a-f]+)\)\)\(")
_DSTATE_INT = re.compile(r"^(0x[0-9a-f]+|\d+)$")
_DSTATE_CALLNAME = re.compile(r"\b([A-Za-z_][A-Za-z0-9_]*)\(")


def _dstate_int(tok):
	tok = tok.strip().rstrip(";")
	m = _DSTATE_INT.match(tok)
	if not m:
		return None
	return int(tok, 16) if tok.lower().startswith("0x") else int(tok)


def _dstate_args(code):
	"""Split the argument list of the LAST call on the line (best effort)."""
	end = code.rfind(")")
	if end < 0:
		return []
	depth, start = 0, -1
	for i in range(end, -1, -1):
		c = code[i]
		if c == ")":
			depth += 1
		elif c == "(":
			depth -= 1
			if depth == 0:
				start = i
				break
	if start < 0:
		return []
	inner, args, d, cur = code[start + 1:end], [], 0, ""
	for c in inner:
		if c in "([":
			d += 1
		elif c in ")]":
			d -= 1
		if c == "," and d == 0:
			args.append(cur.strip()); cur = ""
		else:
			cur += c
	if cur.strip():
		args.append(cur.strip())
	return args


def cmd_d3dstate(args, con):
	"""Fixed-function D3D9 state timeline of one function, with lighting tracking.

    Walks the function's pseudocode in order, extracts every device state event
    (SetRenderState* helpers + raw device vtable calls), tracks D3DRS_LIGHTING,
    and WARNS when a material is bound while lighting is off/unknown - the
    proximity-inference trap. `--at 0xADDR` reports the accumulated state there.
    """
	dump = get_dump(con, args)
	row = resolve_func(con, args.target)
	if not row:
		print(f"error: function not found: {args.target}", file=sys.stderr)
		return 1
	addr, name, sig, sb, eb, sl, el = row
	shown = load_globals(con).get(name)
	disp = f"{name}" + (f" [{shown[0]}]" if shown else "")
	at = norm_addr(args.at) if args.at else None
	at_n = int(at, 16) if at else None

	rename = build_rename_map(load_globals(con), load_locals(con, addr))
	lines = read_func_lines(dump, sb, eb)

	# Group continuation lines (same leading address) into one logical statement:
	# the decompiler wraps long calls across lines, which used to hide e.g. the
	# multi-line TEXTUREFACTOR write in sub_8a4d40 from the parser entirely.
	stmts = []   # [addr_str, joined_code]
	cur_addr = None
	for raw in lines:
		m = _DSTATE_LINE_ADDR.match(raw)
		if m:
			cur_addr = m.group(1)
		if cur_addr is None:
			continue
		body = (raw[8:] if m else raw).split("//", 1)[0].strip()
		if not body:
			continue
		if stmts and stmts[-1][0] == cur_addr:
			stmts[-1][1] += " " + body
		else:
			stmts.append([cur_addr, body])

	events = []   # (addr_int, addr_str, kind, text, meta)
	for cur_addr, joined in stmts:
		code = apply_renames(joined, rename)
		a_int = int(cur_addr, 16)
		if cur_addr == addr:
			continue  # the function's own signature/prologue line is not a call event

		# raw device vtable call: (*(...)(*(...)obj + 0xNN))(obj, ...)
		vm = _DSTATE_VTBL.search(code)
		if vm:
			off = int(vm.group(1), 16)
			ent = D3D9_VTBL.get(off)
			if ent:
				fname, kind = ent
				argl = _dstate_args(code)
				if kind == "renderstate" and len(argl) >= 3:
					sid = _dstate_int(argl[-2])
					sname = D3DRS_NAMES.get(sid, f"0x{sid:x}" if sid is not None else argl[-2])
					events.append((a_int, cur_addr, "rs",
								   f"SetRenderState({sname}={_dstate_rs_value(sid, argl[-1])})",
								   (sid, _dstate_int(argl[-1]))))
				elif kind == "tss":
					text, meta = _dstate_tss(argl)
					events.append((a_int, cur_addr, "tss", f"{fname}({text})", meta))
				elif kind == "lightenable" and len(argl) >= 3:
					events.append((a_int, cur_addr, "lighten",
								   f"LightEnable(index={argl[-2]}, enable={argl[-1]})", None))
				elif kind == "draw" and len(argl) >= 2:
					pt = _dstate_int(argl[1])
					ptname = D3DPT_NAMES.get(pt)
					rest = ", ".join(argl[1:])
					events.append((a_int, cur_addr, "draw",
								   f"{fname}({rest})" + (f"   [{ptname}]" if ptname else ""), None))
				elif kind == "transform" and len(argl) >= 2:
					ts = _dstate_int(argl[-2]) if len(argl) >= 3 else _dstate_int(argl[-1])
					tsname = D3DTS_NAMES.get(ts)
					events.append((a_int, cur_addr, "state",
								   f"{fname}({', '.join(argl[1:])})"
								   + (f"   [D3DTS_{tsname}]" if tsname else ""), None))
				else:
					events.append((a_int, cur_addr, kind,
								   f"{fname}({', '.join(argl[1:]) if len(argl) > 1 else ''})", None))
				continue

		# named helper calls (post-rename), e.g. SetRenderStateCached(cache, 0x89, 1)
		for cm in _DSTATE_CALLNAME.finditer(code):
			fn = cm.group(1)
			if re.search(r"SetRenderState", fn):
				argl = _dstate_args(code)
				if len(argl) >= 2:
					sid = _dstate_int(argl[-2])
					sname = D3DRS_NAMES.get(sid, f"0x{sid:x}" if sid is not None else argl[-2])
					events.append((a_int, cur_addr, "rs",
								   f"{fn}({sname}={_dstate_rs_value(sid, argl[-1])})",
								   (sid, _dstate_int(argl[-1]))))
				break
			if re.search(r"SetMaterial", fn):
				events.append((a_int, cur_addr, "material", f"{fn}(...)", None))
				break
			if re.search(r"SetTextureStageState|SetTSS", fn):
				text, meta = _dstate_tss(_dstate_args(code))
				events.append((a_int, cur_addr, "tss", f"{fn}({text})", meta))
				break
			if re.search(r"LightEnable", fn):
				argl = _dstate_args(code)
				events.append((a_int, cur_addr, "lighten", f"{fn}({', '.join(argl)})", None))
				break
			if re.search(r"Draw|Render", fn) and not re.match(r"sub_[0-9a-f]+$", fn):
				events.append((a_int, cur_addr, "call-draw?", f"call {fn}(...) [callee may draw/set state]", None))
				break
			if re.match(r"sub_[0-9a-f]{6,8}$", fn):
				# opaque callee: it may draw or change state; surface it instead of hiding it
				events.append((a_int, cur_addr, "call?", f"call {fn}(...) [unlabeled callee - may draw/set state]", None))
				break

	if not events:
		print(f"{disp}: no D3D state events recognised (not a render-path function?)")
		return 0

	# walk + lighting/blend/stage tracking
	lighting = None        # None = inherited/unknown
	last_material = None   # (addr_str, lighting_at_bind)
	rs_vals = {}           # D3DRS id -> last int value (None values not stored)
	tss_vals = {}          # (stage, type) -> last int value
	warns = []
	print(f"d3dstate {disp}  ({len(events)} events"
		  + (f", state evaluated at 0x{at}" if at else "") + ")")
	print(f"  LIGHTING starts as INHERITED/unknown (set by caller or previous frame)")
	for a_int, a_str, kind, text, meta in events:
		marker = "  "
		note = ""
		if kind == "rs":
			if meta and meta[0] is not None:
				rs_vals[meta[0]] = meta[1]   # value may be None (non-literal expr) - presence still counts
			if "LIGHTING=" in text:
				val = text.rsplit("=", 1)[-1].rstrip(")")
				lighting = _dstate_int(val) if _dstate_int(val) is not None else val
				note = "   <<< FF lighting " + ("ON" if lighting == 1 else "OFF" if lighting == 0 else f"= {lighting}")
		elif kind == "tss":
			if meta and meta[0] is not None and meta[1] is not None and meta[2] is not None:
				tss_vals[(meta[0], meta[1])] = meta[2]
		elif kind == "material":
			last_material = (a_str, lighting)
			if lighting == 0:
				note = "   !! bound while LIGHTING=0 - FF IGNORES the material (inert unless lighting turns on AND no rebind)"
				warns.append(a_str)
			elif lighting is None:
				note = "   ?? lighting INHERITED here - verify caller state before attributing this material to a draw"
				warns.append(a_str)
		elif kind == "draw":
			mat = f"material from 0x{last_material[0]}" if last_material else "material INHERITED"
			lit = "ON" if lighting == 1 else "OFF" if lighting == 0 else "INHERITED"
			note = f"   <<< DRAW with LIGHTING={lit}, {mat}" + \
				   ("" if lighting == 1 else " (material NOT consumed)" if lighting == 0 else "")
		elif kind == "vshader":
			note = "   <<< programmable VS path - FF lighting/material do not apply to these draws"
		if at_n is not None and a_int > at_n:
			marker = " >"   # past the --at point
		print(f"{marker}{a_str}  {text}{note}")

	print()
	lit = "ON" if lighting == 1 else "OFF" if lighting == 0 else "INHERITED/unknown"
	if at_n is not None:
		upto = [e for e in events if e[0] <= at_n]
		lit_at, mat_at = None, None
		rs_at, tss_at = {}, {}
		for a_int, a_str, kind, text, meta in upto:
			if kind == "rs":
				if meta and meta[0] is not None:
					rs_at[meta[0]] = meta[1]
				if "LIGHTING=" in text:
					v = _dstate_int(text.rsplit("=", 1)[-1].rstrip(")"))
					lit_at = v if v is not None else lit_at
			elif kind == "tss":
				if meta and meta[0] is not None and meta[1] is not None and meta[2] is not None:
					tss_at[(meta[0], meta[1])] = meta[2]
			elif kind == "material":
				mat_at = a_str
		lit_s = "ON" if lit_at == 1 else "OFF" if lit_at == 0 else "INHERITED/unknown"
		print(f"state at 0x{at}: LIGHTING={lit_s}; active material = "
			  + (f"bound @0x{mat_at}" if mat_at else "inherited"))
		for line in _dstate_blend_summary(rs_at, tss_at):
			print(f"  {line}")
		if lit_at == 0 and mat_at:
			print("  -> any draw here CANNOT consume the material (FF ignores D3DMATERIAL9 while lighting is off).")
		elif lit_at is None:
			print("  -> lighting is inherited: check the CALLER's state before concluding anything about materials.")
	else:
		print(f"end-of-function: LIGHTING={lit}")
		summary = _dstate_blend_summary(rs_vals, tss_vals)
		if summary:
			print("blend/stage conclusion (accumulated over this function; caller state may differ):")
			for line in summary:
				print(f"  {line}")
	if warns:
		print(f"  {len(warns)} material bind(s) under lighting OFF/unknown: "
			  + ", ".join(f"0x{w}" for w in warns))
		print("  Rule: a SetMaterial is only consumed by a draw if LIGHTING=1 at that draw and no"
			  " rebind happened in between. Confirm with `break exec` on the device SetMaterial impl.")
	return 0


# --------------------------------------------------------------------------
# Live-process reader - resolve runtime-populated (.bss) values by actually
# running the client. SRO_Client.exe has no ASLR (DllCharacteristics=0,
# ImageBase=0x400000) so a static VA == its runtime address; we still rebase
# off the real module base for safety. Read-only ReadProcessMemory from
# OUTSIDE the process - no DLL injection, no patching, no on-disk changes.
# --------------------------------------------------------------------------

IMAGE_BASE_DEFAULT = 0x400000
