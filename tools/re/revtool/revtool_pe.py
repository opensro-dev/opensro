"""
===========================================================================
revtool_pe.py - PE reading: vtable and deref over the retail image

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

from revtool_asserts import _ARR_ENTRY_RE, _ARR_SINGLE_RE, _ARR_START_RE
from revtool_asserts import _COL_RE, _VFTABLE_RE
from revtool_dump import evidence_artifact_paths, get_binary
from revtool_dump import symbol_is_annotated
from revtool_paths import DEFAULT_BINARY, PROOF_TOOLS
from revtool_store import _as_int, sha256_file

try:
	from msvc_rtti import rtti_direct_bases, rtti_hierarchies, scan_vtables
except ImportError:  # surfaced loudly by the structural commands
	rtti_direct_bases = rtti_hierarchies = scan_vtables = None

class PEImage:
	def __init__(self, path):
		self.path = path
		with open(path, "rb") as f:
			self.data = f.read()
		d = self.data
		if d[:2] != b"MZ":
			raise ValueError("not a PE (no MZ)")
		pe = int.from_bytes(d[0x3c:0x40], "little")
		if d[pe:pe + 4] != b"PE\x00\x00":
			raise ValueError("not a PE (no PE\\0\\0)")
		n_sections = int.from_bytes(d[pe + 6:pe + 8], "little")
		opt_size = int.from_bytes(d[pe + 20:pe + 22], "little")
		opt = pe + 24
		magic = int.from_bytes(d[opt:opt + 2], "little")
		if magic == 0x10b:        # PE32
			self.image_base = int.from_bytes(d[opt + 28:opt + 32], "little")
		elif magic == 0x20b:      # PE32+
			self.image_base = int.from_bytes(d[opt + 24:opt + 32], "little")
		else:
			raise ValueError(f"unknown optional-header magic {magic:#x}")
		self.image_size = int.from_bytes(d[opt + 56:opt + 60], "little")
		sec = opt + opt_size
		self.sections = []  # (name, va, vsize, raw_ptr, raw_size)
		for i in range(n_sections):
			h = sec + i * 40
			name = d[h:h + 8].rstrip(b"\x00").decode("ascii", "replace")
			vsize = int.from_bytes(d[h + 8:h + 12], "little")
			va = int.from_bytes(d[h + 12:h + 16], "little")
			rsize = int.from_bytes(d[h + 16:h + 20], "little")
			rptr = int.from_bytes(d[h + 20:h + 24], "little")
			self.sections.append((name, va, vsize, rptr, rsize))

	def section_of(self, va):
		rva = va - self.image_base
		for name, sva, vsize, rptr, rsize in self.sections:
			span = max(vsize, rsize)
			if sva <= rva < sva + span:
				return (name, sva, vsize, rptr, rsize)
		return None

	def _offset(self, va):
		s = self.section_of(va)
		if not s:
			return None
		_name, sva, _vsize, rptr, rsize = s
		rva = va - self.image_base
		off = rva - sva + rptr
		if off + 4 > rptr + rsize:      # in virtual-only tail (uninitialised)
			return None
		return off

	def classify(self, va):
		"""Why a read can't resolve, so callers can give a useful hint instead of '----':
          'ok'       - readable raw bytes
          'uninit'   - inside a section but in its virtual-only tail (.bss-style):
                       zero in the image, POPULATED AT RUNTIME -> find the writer
          'unmapped' - not inside any section
        """
		s = self.section_of(va)
		if not s:
			return "unmapped"
		_name, sva, _vsize, rptr, rsize = s
		off = (va - self.image_base) - sva + rptr
		return "ok" if off + 4 <= rptr + rsize else "uninit"

	def read_bytes(self, va, n):
		off = self._offset(va)
		if off is None:
			return None
		return self.data[off:off + n]

	def read_u32(self, va):
		b = self.read_bytes(va, 4)
		if b is None or len(b) < 4:
			return None
		return int.from_bytes(b, "little")

	def in_image(self, va):
		return self.image_base <= va < self.image_base + self.image_size


def open_pe(con, args):
	path = get_binary(con, args)
	if not os.path.exists(path):
		print(f"error: PE binary not found: {path}\n"
			  f"  point revtool at it once:  revtool config binary <path-to-SRO_Client.exe>",
			  file=sys.stderr)
		return None
	try:
		return PEImage(path)
	except (OSError, ValueError) as e:
		print(f"error: cannot read PE {path}: {e}", file=sys.stderr)
		return None


def resolve_addr(con, va):
	"""Map a code VA to (label, status_tag). Exact function header if possible, else
    the containing function with a +offset. status_tag mirrors xref: [name], [labeled],
    [NEEDS RE], or [data/unknown]."""
	a = "%08x" % va
	row = con.execute("SELECT name FROM functions WHERE addr=?", (a,)).fetchone()
	off = 0
	if not row:
		# containing function: greatest header addr <= va (8-wide hex sorts numerically)
		row = con.execute(
			"SELECT name,addr FROM functions WHERE addr<=? ORDER BY addr DESC LIMIT 1",
			(a,)).fetchone()
		if row:
			off = va - int(row[1], 16)
			# guard against "previous function" false positives for far targets
			if off > 0x4000:
				return (f"(0x{va:08x} - no function here)", "[data/unknown]")
		else:
			return (f"(0x{va:08x} - no function here)", "[data/unknown]")
	orig = row[0]
	applied = con.execute(
		"SELECT new_name FROM globals WHERE orig_name=?", (orig,)).fetchone()
	name = applied[0] if applied and applied[0] else orig
	suffix = f"+0x{off:x}" if off else ""
	if applied and applied[0]:
		tag = "[labeled]"
	elif symbol_is_annotated(con, orig):
		tag = "[labeled]"
	else:
		tag = "[NEEDS RE]"
	return (name + suffix, tag)


def _resolve_vtable_slot(pe, con, base, slot_off, class_name=None, for_base=None):
	sec = pe.section_of(base)
	secname = sec[0] if sec else "?"
	target = pe.read_u32(base + slot_off)
	info = {
		"base": base,
		"slot": slot_off,
		"section": secname,
		"class": class_name,
		"for_base": for_base,
		"target": target,
		"name": None,
		"tag": None,
		"error": None,
	}
	if target is None:
		kind = pe.classify(base + slot_off)
		hint = ("uninitialised (.bss/virtual) -> runtime-populated; this is a "
				"RUNTIME vtable/field, find the writer (rizin_recipes.md recipe 1/2)"
				if kind == "uninit" else "address not in any section")
		info["error"] = hint
		return info
	name, tag = resolve_addr(con, target)
	info["name"] = name
	info["tag"] = tag
	return info


def _vtable_label(info):
	parts = []
	if info.get("class"):
		if info.get("for_base"):
			parts.append(f"{info['class']}{{for {info['for_base']}}}")
		else:
			parts.append(info["class"])
	parts.append(f"vtable 0x{info['base']:08x} ({info['section']})")
	return " ".join(parts)


def _collect_vtable_addrs(args, con, pe):
	entries = []
	for raw in args.addr:
		base = _as_int(raw)
		if base is None:
			raise ValueError(f"bad address {raw!r}")
		entries.append({"addr": base, "class": None, "for_base": None, "count": None})

	if getattr(args, "base", None):
		idx = _best_rtti_index(args, con, pe)
		classes = idx["classes"]
		base_cls = args.base
		if base_cls not in classes:
			hit = [c for c in classes if c.lower() == base_cls.lower()]
			if not hit:
				raise ValueError(f"class not found in RTTI: {base_cls}")
			base_cls = hit[0]
		family = {base_cls} | idx["desc"].get(base_cls, set())
		for cls in sorted(family):
			for vaddr, for_base in idx.get("class_vtables", {}).get(cls, []):
				# Default family comparison follows the primary inheritance-chain
				# vtable for each class. Secondary subobject vtables from multiple
				# inheritance (for example CAnimationCallback) use the same byte
				# offsets for a different receiver type, so including them would
				# falsely make unrelated slots look polymorphic.
				if not getattr(args, "all_subobjects", False):
					if for_base is not None:
						continue
				record = idx.get("vtable_records", {}).get(vaddr)
				entries.append({
					"addr": vaddr, "class": cls, "for_base": for_base,
					"count": len(record.targets) if record is not None else None,
				})

	# Preserve order while dropping duplicate concrete vtable addresses.
	seen = set()
	uniq = []
	for e in entries:
		if e["addr"] in seen:
			continue
		seen.add(e["addr"])
		uniq.append(e)
	return uniq


def _print_vtable_slot_classification(pe, con, entries, slot_off):
	infos = [
		_resolve_vtable_slot(pe, con, e["addr"], slot_off, e.get("class"), e.get("for_base"))
		for e in entries
	]
	print(f"vtable slot classification +0x{slot_off:x} across {len(infos)} vtable(s)")
	targets = {}
	unreadable = []
	for info in infos:
		label = _vtable_label(info)
		if info["target"] is None:
			unreadable.append(info)
			print(f"  {label} -> <unreadable - {info['error']}>")
			continue
		print(f"  {label} -> 0x{info['target']:08x}  {info['name']}  {info['tag']}")
		targets.setdefault(info["target"], []).append(info)

	if unreadable:
		print(f"\nverdict: UNKNOWN ({len(unreadable)} unreadable slot(s); resolve runtime writer first)")
		return 1

	if not targets:
		print("\nverdict: UNKNOWN (no readable targets)")
		return 1

	if len(targets) == 1:
		target, hit_infos = next(iter(targets.items()))
		name, tag = resolve_addr(con, target)
		print(f"\nverdict: MONOMORPHIC ({len(hit_infos)} vtable(s), one target)")
		print(f"devirtualize candidate: direct call 0x{target:08x}  {name}  {tag}")
		print("wip rule: call the exported WIP function directly; keep receiver/vtable/slot evidence in comments.")
		return 0

	print(f"\nverdict: POLYMORPHIC ({len(targets)} targets across {len(infos)} vtable(s))")
	for target, hit_infos in sorted(targets.items()):
		name, tag = resolve_addr(con, target)
		vt_list = ", ".join(f"0x{i['base']:08x}" for i in hit_infos)
		print(f"  0x{target:08x}  {name}  {tag}  from {vt_list}")
	print("wip rule: keep an interface/dispatch shape; add JSDoc @see {@link ...} for each concrete WIP target.")
	return 0


def cmd_vtable(args, con):
	"""Dump a vtable from the PE and resolve each slot to a (possibly renamed) function.

    Automates the manual rizin recipe for virtual-call resolution:
      `vtable 0xbcc27c` -> +0x2c  0x004fb7e0  CIFConsole_Update  [labeled]
    Pair with `--slot 0xNN` to read exactly the slot a `(*(*obj+0xNN))()` call uses.
    """
	pe = open_pe(con, args)
	if pe is None:
		return 1
	try:
		entries = _collect_vtable_addrs(args, con, pe)
	except ValueError as e:
		print(f"error: {e}", file=sys.stderr)
		return 1
	if not entries:
		print("error: provide a vtable VA or --base <Class>", file=sys.stderr)
		return 1
	if len(entries) > 1 and args.slot is None:
		print("error: comparing multiple vtables requires --slot 0xNN", file=sys.stderr)
		return 1
	base = entries[0]["addr"]
	sec = pe.section_of(base)
	secname = sec[0] if sec else "?"
	if args.slot is not None:
		slot_off = _as_int(args.slot)
		if slot_off is None:
			print(f"error: bad slot {args.slot!r}", file=sys.stderr)
			return 1
		if len(entries) > 1:
			return _print_vtable_slot_classification(pe, con, entries, slot_off)
		target = pe.read_u32(base + slot_off)
		if target is None:
			kind = pe.classify(base + slot_off)
			hint = ("uninitialised (.bss/virtual) -> runtime-populated; this is a "
					"RUNTIME vtable/field, find the writer (rizin_recipes.md recipe 1/2)"
					if kind == "uninit" else "address not in any section")
			print(f"slot +0x{slot_off:x}: <unreadable - {hint}>")
			return 1
		name, tag = resolve_addr(con, target)
		print(f"vtable 0x{base:08x} ({secname})  +0x{slot_off:x}  ->  "
			  f"0x{target:08x}  {name}  {tag}")
		return 0
	structural_count = entries[0].get("count")
	count = min(args.count, structural_count) if structural_count is not None else args.count
	bound = " structurally bounded" if structural_count is not None else " user-limited"
	print(f"vtable 0x{base:08x} ({secname})  -  {count} slots ({bound.strip()})")
	printed = 0
	for i in range(count):
		slot_off = i * 4
		target = pe.read_u32(base + slot_off)
		if target is None:
			if i == 0 and pe.classify(base) == "uninit":
				print(f"  +0x{slot_off:<4x} <uninitialised (.bss/virtual) -> RUNTIME vtable; "
					  f"find the writer, rizin_recipes.md recipe 1/2>")
			else:
				print(f"  +0x{slot_off:<4x} <unreadable - past section>")
			break
		if not pe.in_image(target):
			# a 0 or non-code word usually marks the end of the vtable (RTTI/next obj)
			print(f"  +0x{slot_off:<4x} 0x{target:08x}  (non-code - vtable likely ends here)")
			if target == 0:
				break
			continue
		name, tag = resolve_addr(con, target)
		print(f"  +0x{slot_off:<4x} 0x{target:08x}  {name}  {tag}")
		printed += 1
	if printed:
		print(f"\ntip: the GWnd/CObjTree update slot is +0x2c; bind the object's type and "
			  f"`coverage` the resolved fn next.")
	return 0


def cmd_deref(args, con):
	"""Read N little-endian dwords from a VA in the PE (the `pxw` equivalent).

    For .rdata descriptors, class records, jump tables - anything the text dump omits.
    """
	pe = open_pe(con, args)
	if pe is None:
		return 1
	try:
		base = int(args.addr, 16)
	except ValueError:
		print(f"error: bad address {args.addr!r}", file=sys.stderr)
		return 1
	sec = pe.section_of(base)
	print(f"deref 0x{base:08x} ({sec[0] if sec else '?'})")
	# The count arrives as the positional `count` or as --count (the
	# positional spelling is the one the help text has always shown);
	# the flag wins when both are given.
	count = getattr(args, "count_flag", None)
	if count is None:
		count = args.count if args.count is not None else 8
	per_row = 4
	any_readable = False
	for r in range((count + per_row - 1) // per_row):
		va = base + r * per_row * 4
		words, ascii_bytes = [], b""
		for c in range(per_row):
			if r * per_row + c >= count:
				break
			w = pe.read_u32(va + c * 4)
			if w is None:
				words.append("--------")
				ascii_bytes += b"...."
			else:
				any_readable = True
				words.append(f"{w:08x}")
				ascii_bytes += w.to_bytes(4, "little")
		printable = "".join(chr(b) if 32 <= b < 127 else "." for b in ascii_bytes)
		print(f"  0x{va:08x}  {' '.join(words):<44}  {printable}")
	if not any_readable:
		kind = pe.classify(base)
		if kind == "uninit":
			print(f"\n  note: 0x{base:08x} is in '{sec[0] if sec else '?'}' but ZERO in the image "
				  f"(.bss-style virtual tail) -> POPULATED AT RUNTIME.\n"
				  f"  A static deref can't see it. Two ways forward:\n"
				  f"    - read it LIVE:  revtool runtime 0x{base:08x}   (launches the client, "
				  f"polls, resolves the value to a function, kills it)\n"
				  f"    - find the writer statically: rizin_recipes.md recipe 1/2.")
		elif kind == "unmapped":
			print(f"\n  note: 0x{base:08x} is not inside any PE section (bad address?).")
	return 0


# --------------------------------------------------------------------------- #
# d3dstate - fixed-function pipeline state timeline for a function
# --------------------------------------------------------------------------- #
# Born from the color0x18c incident (notes 363/368/372): a SetMaterial right
# before a draw was assumed consumed by that draw, but D3DRS_LIGHTING was OFF,
# making the material inert. The consumer of a device binding can only be
# judged against the PIPELINE STATE at the draw, never by call proximity.
# This command makes that state visible so the mistake is structurally hard.

# D3DRENDERSTATETYPE ids seen via SetRenderState(Cached) - common subset.
D3DRS_NAMES = {
	7: "ZENABLE", 8: "FILLMODE", 9: "SHADEMODE", 14: "ZWRITEENABLE",
	15: "ALPHATESTENABLE", 16: "LASTPIXEL", 19: "SRCBLEND", 20: "DESTBLEND",
	22: "CULLMODE", 23: "ZFUNC", 24: "ALPHAREF", 25: "ALPHAFUNC",
	26: "DITHERENABLE", 27: "ALPHABLENDENABLE", 28: "FOGENABLE",
	29: "SPECULARENABLE", 34: "FOGCOLOR", 35: "FOGTABLEMODE", 36: "FOGSTART",
	37: "FOGEND", 38: "FOGDENSITY", 48: "RANGEFOGENABLE", 52: "STENCILENABLE",
	60: "TEXTUREFACTOR", 136: "CLIPPING", 137: "LIGHTING", 139: "AMBIENT",
	140: "FOGVERTEXMODE", 141: "COLORVERTEX", 142: "LOCALVIEWER",
	143: "NORMALIZENORMALS", 145: "DIFFUSEMATERIALSOURCE",
	146: "SPECULARMATERIALSOURCE", 147: "AMBIENTMATERIALSOURCE",
	148: "EMISSIVEMATERIALSOURCE", 152: "VERTEXBLEND", 153: "CLIPPLANEENABLE",
}

# IDirect3DDevice9 vtable byte offsets (slot*4) for state/draw calls.
# 0x158/0x15c added after the color0x1c0 incident (notes 347 vs 373): an undecoded
# `(*(*dev+0x15c))(dev, X)` was misread as a texture bind; it is SetVertexDeclaration.
D3D9_VTBL = {
	0xb0: ("SetTransform", "transform"), 0xbc: ("SetViewport", "state"),
	0xc4: ("SetMaterial", "material"), 0xcc: ("SetLight", "state"),
	0xd4: ("LightEnable", "lightenable"), 0xdc: ("SetClipPlane", "state"),
	0xe4: ("SetRenderState", "renderstate"), 0x104: ("SetTexture", "state"),
	0x10c: ("SetTextureStageState", "tss"), 0x114: ("SetSamplerState", "state"),
	0x144: ("DrawPrimitive", "draw"), 0x148: ("DrawIndexedPrimitive", "draw"),
	0x14c: ("DrawPrimitiveUP", "draw"), 0x150: ("DrawIndexedPrimitiveUP", "draw"),
	0x158: ("CreateVertexDeclaration", "state"),
	0x15c: ("SetVertexDeclaration", "state"),
	0x164: ("SetFVF", "state"), 0x170: ("SetVertexShader", "vshader"),
	0x178: ("SetVertexShaderConstantF", "state"),
	0x190: ("SetStreamSource", "state"), 0x198: ("SetStreamSourceFreq", "state"),
	0x1a0: ("SetIndices", "state"), 0x1ac: ("SetPixelShader", "state"),
	0x1b4: ("SetPixelShaderConstantF", "state"),
}

# Enum decoders. Born from the color0x1c0 incident: `SetTextureStageState(0,1,7)` was
# misremembered as MODULATE (4) when 7 = D3DTOP_ADD, and SRCBLEND=1/DESTBLEND=3 were never
# decoded to ZERO/SRCCOLOR, hiding that the pass is a framebuffer MULTIPLY. Decode the values
# so the reader never has to (mis)recall a D3D enum again.
D3DBLEND_NAMES = {
	1: "ZERO", 2: "ONE", 3: "SRCCOLOR", 4: "INVSRCCOLOR", 5: "SRCALPHA",
	6: "INVSRCALPHA", 7: "DESTALPHA", 8: "INVDESTALPHA", 9: "DESTCOLOR",
	10: "INVDESTCOLOR", 11: "SRCALPHASAT", 14: "BLENDFACTOR", 15: "INVBLENDFACTOR",
}
D3DTOP_NAMES = {
	1: "DISABLE", 2: "SELECTARG1", 3: "SELECTARG2", 4: "MODULATE", 5: "MODULATE2X",
	6: "MODULATE4X", 7: "ADD", 8: "ADDSIGNED", 9: "ADDSIGNED2X", 10: "SUBTRACT",
	11: "ADDSMOOTH", 12: "BLENDDIFFUSEALPHA", 13: "BLENDTEXTUREALPHA",
	14: "BLENDFACTORALPHA", 15: "BLENDTEXTUREALPHAPM", 16: "BLENDCURRENTALPHA",
	18: "PREMODULATE", 19: "MODULATEALPHA_ADDCOLOR", 20: "MODULATECOLOR_ADDALPHA",
	24: "DOTPRODUCT3", 25: "MULTIPLYADD", 26: "LERP",
}
D3DTA_NAMES = {0: "DIFFUSE", 1: "CURRENT", 2: "TEXTURE", 3: "TFACTOR", 4: "SPECULAR",
			   5: "TEMP", 6: "CONSTANT"}
D3DCMP_NAMES = {1: "NEVER", 2: "LESS", 3: "EQUAL", 4: "LESSEQUAL", 5: "GREATER",
				6: "NOTEQUAL", 7: "GREATEREQUAL", 8: "ALWAYS"}
D3DCULL_NAMES = {1: "NONE", 2: "CW", 3: "CCW"}
D3DFOGMODE_NAMES = {0: "NONE", 1: "EXP", 2: "EXP2", 3: "LINEAR"}
D3DFILL_NAMES = {1: "POINT", 2: "WIREFRAME", 3: "SOLID"}
D3DSHADE_NAMES = {1: "FLAT", 2: "GOURAUD", 3: "PHONG"}
D3DBLENDOP_NAMES = {1: "ADD", 2: "SUBTRACT", 3: "REVSUBTRACT", 4: "MIN", 5: "MAX"}
D3DPT_NAMES = {1: "POINTLIST", 2: "LINELIST", 3: "LINESTRIP", 4: "TRIANGLELIST",
			   5: "TRIANGLESTRIP", 6: "TRIANGLEFAN"}
D3DTS_NAMES = {2: "VIEW", 3: "PROJECTION", 256: "WORLD"}
D3DTS_NAMES.update({16 + i: f"TEXTURE{i}" for i in range(8)})

D3DTSS_NAMES = {
	1: "COLOROP", 2: "COLORARG1", 3: "COLORARG2", 4: "ALPHAOP", 5: "ALPHAARG1",
	6: "ALPHAARG2", 7: "BUMPENVMAT00", 8: "BUMPENVMAT01", 9: "BUMPENVMAT10",
	10: "BUMPENVMAT11", 11: "TEXCOORDINDEX", 22: "BUMPENVLSCALE", 23: "BUMPENVLOFFSET",
	24: "TEXTURETRANSFORMFLAGS", 26: "COLORARG0", 27: "ALPHAARG0", 28: "RESULTARG",
	32: "CONSTANT",
}
# TSS types whose value is a D3DTOP op vs a D3DTA argument selector.
_D3DTSS_OP_TYPES = {1, 4}
_D3DTSS_ARG_TYPES = {2, 3, 5, 6, 26, 27, 28}

# Render states whose VALUE is an enum worth decoding.
_D3DRS_VALUE_DECODER = {
	8: D3DFILL_NAMES, 9: D3DSHADE_NAMES, 19: D3DBLEND_NAMES, 20: D3DBLEND_NAMES,
	22: D3DCULL_NAMES, 23: D3DCMP_NAMES, 25: D3DCMP_NAMES, 35: D3DFOGMODE_NAMES,
	140: D3DFOGMODE_NAMES, 171: D3DBLENDOP_NAMES,
}


def _rtti_index(dump):
	"""Parse the dump's RTTI/vftable labels into a class model.

    Returns dict:
      classes        set of class names
      vtable_addr    class -> int VA of its primary vftable
      class_at_vtbl  int VA -> class for primary vftables
      class_vtables  class -> list((int VA, for_base_or_None)) for all vftables
      direct_base    class -> immediate base class (entries[1] of its array)
      anc            class -> set(all ancestor classes)        (from the Base Class Array)
      desc           class -> set(all descendant classes)      (inverse of anc)
    """
	classes = set()
	vtable_addr = {}
	class_at_vtbl = {}
	class_vtables = {}
	direct_base = {}
	anc = {}
	in_owner = None
	entries = []

	def _finalize(owner, ents):
		if not owner or not ents:
			return
		# ents[0] is the class itself; ents[1:] are its ancestors, nearest-first.
		rest = [e for e in ents[1:]]
		if rest:
			direct_base[owner] = rest[0]
			anc.setdefault(owner, set()).update(rest)
			classes.update(rest)

	with open(dump, encoding="utf-8", errors="replace") as f:
		for line in f:
			mv = _VFTABLE_RE.match(line)
			if mv:
				addr = int(mv.group(1), 16)
				cls = mv.group(2)
				for_base = mv.group(3)
				classes.add(cls)
				class_vtables.setdefault(cls, []).append((addr, for_base))
				if for_base is None and cls not in vtable_addr:
					vtable_addr[cls] = addr
					class_at_vtbl[addr] = cls
				continue
			if in_owner is not None:
				me = _ARR_ENTRY_RE.search(line)
				if me:
					entries.append(me.group(1))
					continue
				# Structural lines (`arrayOfBaseClassDescriptors[..] =`, `{`) precede the
				# entries, so only CLOSE once at least one entry has been collected.
				if entries:
					_finalize(in_owner, entries)
					in_owner, entries = None, []
				# else: keep waiting for the entries (fall through to re-check starts)
			ma = _ARR_START_RE.search(line)
			if ma:
				in_owner, entries = ma.group(1), []
				classes.add(in_owner)
				continue
			ms = _ARR_SINGLE_RE.search(line)
			if ms:
				classes.add(ms.group(1))  # single-entry array => root class, no ancestors
				continue
			mc = _COL_RE.search(line)
			if mc:
				classes.add(mc.group(1))
	_finalize(in_owner, entries)

	desc = {}
	for cls, ancestors in anc.items():
		for a in ancestors:
			desc.setdefault(a, set()).add(cls)
	return {
		"classes": classes, "vtable_addr": vtable_addr, "class_at_vtbl": class_at_vtbl,
		"class_vtables": class_vtables, "direct_base": direct_base, "anc": anc, "desc": desc,
		"source": "legacy Binary Ninja dump labels",
	}


def _structural_rtti_index(args, con, pe=None):
	"""Build the shared PE/RTTI class index used by the family planner.

    The reconciled entry inventory supplies candidate code starts, but the
    locator, hierarchy, complete-object offset, and table bounds all come from
    the retail PE.  This function deliberately fails rather than falling back
    silently to dump labels: two class commands giving different structural
    answers is more dangerous than an unavailable command.
    """
	if scan_vtables is None:
		raise ValueError(f"shared RTTI reader is unavailable from {PROOF_TOOLS}")
	pe = pe or open_pe(con, args)
	if pe is None:
		raise ValueError("retail PE is unavailable")
	reconciled_path = evidence_artifact_paths(con, args)["reconciled_function_index"]
	with open(reconciled_path, encoding="utf-8") as stream:
		reconciled = json.load(stream)
	target_hash = sha256_file(pe.path)
	if str(reconciled.get("target_sha256") or "").lower() != target_hash.lower():
		raise ValueError("reconciled function index belongs to another executable")
	starts = {
		int(row["va"])
		for row in reconciled.get("functions") or []
		if row.get("admitted", True)
	}
	sections = [
		(name, pe.image_base + rva, raw_ptr, raw_size)
		for name, rva, _virtual_size, raw_ptr, raw_size in pe.sections
	]
	text = next((row for row in sections if row[0] == ".text"), None)
	if text is None:
		raise ValueError("retail PE has no .text section")
	_name, text_va, _raw, text_size = text
	records = scan_vtables(pe.data, sections, text_va, text_va + text_size, starts)
	hierarchy = rtti_hierarchies(pe.data, sections)
	direct_sets = rtti_direct_bases(pe.data, sections)
	classes = set(hierarchy)
	for values in hierarchy.values():
		classes.update(values)
	vtable_addr = {}
	class_at_vtbl = {}
	class_vtables = {}
	record_by_vtable = {}
	for record in records:
		class_at_vtbl[record.table_va] = record.class_name
		record_by_vtable[record.table_va] = record
		class_vtables.setdefault(record.class_name, []).append((
			record.table_va,
			None if record.complete_object_offset == 0
			else f"this+0x{record.complete_object_offset:x}",
		))
		if record.complete_object_offset == 0:
			vtable_addr.setdefault(record.class_name, record.table_va)
	anc = {
		owner: set(values) - {owner}
		for owner, values in hierarchy.items()
	}
	desc = {}
	for owner, ancestors in anc.items():
		for base in ancestors:
			desc.setdefault(base, set()).add(owner)
	direct_base = {
		owner: next(iter(values))
		for owner, values in direct_sets.items()
		if len(values) == 1
	}
	return {
		"classes": classes,
		"vtable_addr": vtable_addr,
		"class_at_vtbl": class_at_vtbl,
		"class_vtables": class_vtables,
		"vtable_records": record_by_vtable,
		"direct_base": direct_base,
		"direct_bases": {owner: set(values) for owner, values in direct_sets.items()},
		"anc": anc,
		"desc": desc,
		"source": "shared structural PE/MSVC RTTI parser",
	}


def _best_rtti_index(args, con, pe=None):
	"""One structural RTTI answer for commands and dynamic ingesters."""
	return _structural_rtti_index(args, con, pe)
