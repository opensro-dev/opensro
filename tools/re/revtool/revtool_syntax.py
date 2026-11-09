"""
===========================================================================
revtool_syntax.py - tiers, bases and the dump grammar (regexes and header parsing)

Part of revtool, the non-destructive symbol overlay for the SRO_Client
pseudocode dump. revtool.py is the entry point; this module owns one
command group or layer and is imported by it (and by revtool_rizin.py
through revtool's re-exports). The dump is an immutable Binary Ninja
hypothesis snapshot: original PE bytes and exact proofs outrank it,
and nothing here edits it.
===========================================================================
"""

import re

TIERS = ["unknown", "guess", "mechanism", "pipeline", "verified"]
TIER_RANK = {t: i for i, t in enumerate(TIERS)}
# Compact one-letter badge for the inline/legend view.
TIER_BADGE = {
	"unknown": "?",
	"guess": "g",
	"mechanism": "m",
	"pipeline": "p",
	"verified": "V",
}

# Evidence BASIS - HOW a claim was reached. This is the lesson from the
# data_f0673f "statically 0" miss: a flat confidence tier hid the difference
# between "I saw the write" and "I searched and found nothing".
#   observed  positive disassembly/decompiler evidence (the write/read is right here)
#   dynamic   confirmed by a runtime trace / injected observation
#   inferred  derived from other claims or reasoning (no direct evidence)
#   absence   concluded from a search that found NOTHING (absence of evidence)
BASES = ["observed", "dynamic", "inferred", "absence"]
# A claim resting only on absence-of-evidence or inference CANNOT be 'verified' from
# static work: absence searches have blind spots (e.g. absolute-address /x cannot see
# base+disp stores), and inference can be wrong. Only positive 'observed' evidence or a
# 'dynamic' runtime confirmation may carry the top tier.
STATIC_WEAK_BASES = {"absence", "inferred"}
# Assertion lifecycle.
ASSERT_STATUS = ["open", "confirmed", "refuted"]


def valid_basis(b):
	if b not in BASES:
		raise SystemExit(f"error: basis must be one of {', '.join(BASES)} (got {b!r})")
	return b


def eval_predicate(predicate, observed_value):
	"""Check a machine-readable predicate against an observed value.

    Returns (matches, detail): matches is True/False, or None when the predicate is
    not machine-checkable (human review needed). Numeric compares use a small tolerance.

    Supported predicates:
      zero | nonzero
      eq:N | ne:N | lt:N | le:N | gt:N | ge:N
      range:LO:HI            (inclusive)
      const                  (human/multi-sample; not auto-checkable from one value)
      any                    (always matches; a pure observation sink)
    """
	if not predicate:
		return None, "no predicate"
	p = predicate.strip().lower()
	if p in ("const", "any"):
		return (True, "non-falsifiable predicate") if p == "any" else (None, "const needs multiple samples")
	try:
		obs = float(observed_value)
	except (TypeError, ValueError):
		return None, f"observed {observed_value!r} is not numeric"
	tol = 1e-3

	def near(a, b):
		return abs(a - b) <= tol + 1e-6 * max(abs(a), abs(b))

	if p == "zero":
		return near(obs, 0.0), f"{obs} {'==' if near(obs, 0.0) else '!='} 0"
	if p == "nonzero":
		return (not near(obs, 0.0)), f"{obs} {'!=' if not near(obs, 0.0) else '=='} 0"
	if ":" in p:
		op, rest = p.split(":", 1)
		if op == "range":
			lo_s, _, hi_s = rest.partition(":")
			lo, hi = float(lo_s), float(hi_s)
			ok = (lo - tol) <= obs <= (hi + tol)
			return ok, f"{obs} {'in' if ok else 'outside'} [{lo}, {hi}]"
		try:
			target = float(rest)
		except ValueError:
			return None, f"bad predicate operand {rest!r}"
		cmp = {
			"eq": near(obs, target),
			"ne": not near(obs, target),
			"lt": obs < target - tol,
			"le": obs <= target + tol,
			"gt": obs > target + tol,
			"ge": obs >= target - tol,
		}
		if op in cmp:
			return cmp[op], f"{obs} {op} {target}"
	return None, f"unknown predicate {predicate!r}"

# --------------------------------------------------------------------------- #
# Paths
# --------------------------------------------------------------------------- #
ADDR_RE = re.compile(rb"^([0-9a-fA-F]{8})(\s+)(.*)$")

# A function header line, e.g.
#   00401040    int32_t __fastcall sub_401040(struct IRegionManager::VTable** arg1)
HEADER_RE = re.compile(
	rb"__(?:fastcall|thiscall|cdecl|stdcall)\s+(sub_[0-9a-fA-F]+|[A-Za-z_]\w*)\s*\("
)

# Global symbols anywhere in code (text mode):
GLOBAL_TOKEN_RE = re.compile(r"\b((?:sub_|data_|loc_|jump_table_)[0-9a-fA-F]+)\b")
# Names already recovered by the decompiler (imports, CRT helpers, class methods) are
# readable labels, not labeling debt. The index only vouches for debt when its name is
# still one of the decompiler's anonymous function spellings.
ANONYMOUS_FUNCTION_RE = re.compile(
	r"(?:sub_|j_sub_|nullsub_)[0-9a-fA-F]+", re.IGNORECASE)

# Struct field access, displacement form: <ident> + 0xNN  (ident may be a renamed/bound var)
FIELD_ACCESS_RE = re.compile(r"\b([A-Za-z_]\w*)\s*\+\s*0x([0-9a-fA-F]+)\b")

# Struct field access, index form: <ident>[0xNN]. This decompiler dword-indexes typed
# pointers (e.g. self[0x137] == byte +0x4dc), so the byte offset is index*4.
INDEX_ACCESS_RE = re.compile(r"\b([A-Za-z_]\w*)\s*\[\s*0x([0-9a-fA-F]+)\s*\]")

# `*(T*)var + 0xNN` adds the offset to the DEREFERENCED value (a vtable slot / pointed-to
# datum), NOT to `var`. So `var + 0xNN` here is a vtable-style indirection, not a field of
# `var`. We detect the deref-cast immediately preceding the var: `*(...*)`, allowing an
# optional `&` (the `**(T**)&global + 0xNN` vtable-call shape the decompiler emits for a
# global object pointer). Note this does NOT match a plain cast `(char*)var + 0xNN` (no
# leading `*`), which IS a real field access.
DEREF_CAST_TAIL_RE = re.compile(r"\*\s*\([^()]*\*\s*\)\s*&?\s*$")

# Identifier immediately preceding a base token (i.e. `<ident> [*] <base>`). In a C
# array declaration `TYPE name[0xNN];` this captures the type token, letting us tell
# a declaration apart from a true `name[idx]` indexed access.
PRECEDING_IDENT_RE = re.compile(r"([A-Za-z_]\w*)\s*\**\s*$")
# `&` immediately before a base token: `&name[0xNN]` is address-of-element (pointer
# advance / array addressing), not a struct field load. Used to skip the `p = &p[N]`
# row-cursor stride idiom that would otherwise report a phantom field at N*4.
AMP_TAIL_RE = re.compile(r"&\s*$")
# An open subscript bracket immediately before a base token: `base[idx + 0xNN]`. The
# `idx + 0xNN` is subscript (index) arithmetic, not a field of `idx`.
BRACKET_TAIL_RE = re.compile(r"\[\s*$")
# Code symbols and generated jump-table labels can appear in target arithmetic such as
# `sub_86ca90+0x91` or `jump_table_50cce4[2]`. Those are addresses into `.text`/
# jump-table storage, not struct receiver fields. Keep `data_` globals eligible because
# `data_cec870+0x494` is commonly a real singleton/member access in this dump.
NON_FIELD_BASE_PREFIXES = ("sub_", "loc_", "jump_table_")
# A struct-field byte offset has a sane upper bound. The largest genuine field in this
# codebase is CRTNavMeshTerrain+0x1b384 (~111 KB). Anything >= 1 MiB is never a member
# of a receiver object -- it's an absolute .data/.rdata address the decompiler wrote as
# `base + 0xADDR` (e.g. `arg2 + 0xc1bce4`), a sentinel/mask constant (`result + 0x7fffffff`,
# `x + 0xffffffff`), or a WRAPPED NEGATIVE index whose *4 explodes (`ebx[0xffffffff]` ->
# 0x3fffffffc). A 1 MiB ceiling keeps every real field (>=9x margin over the largest known)
# while dropping all of that arithmetic noise so it can't inflate coverage/depth.
FIELD_OFFSET_MAX = 0x100000
# Control-flow keywords that can legitimately precede an indexed access (`return v[i]`)
# and so must NOT be treated as a declaration's type token. (str form of DECL_KEYWORDS.)
DECL_KEYWORD_STRS = {
	"if", "while", "for", "switch", "catch", "return", "else", "do",
	"sizeof", "case", "goto", "default", "try", "throw",
}

# Binary Ninja frequently splits stack aggregates into scalar locals and then
# renders ordinary coordinate/count arithmetic in the same `base + 0xNN`
# spelling it uses for pointer displacement. Those scalars must not create
# fictitious struct-field debt in `coverage`. Pointer declarations deliberately
# do not match because `*` interrupts the required whitespace before the name.
SCALAR_LOCAL_DECL_RE = re.compile(
	r"^\s*(?:(?:const|volatile)\s+)*"
	r"(?:bool|char|wchar_t|float|double|"
	r"u?int(?:8|16|32|64)_t|short|int|long(?:\s+long)?|"
	r"(?:signed|unsigned)(?:\s+(?:char|short|int|long(?:\s+long)?))?)"
	r"\s+([A-Za-z_]\w*)\b"
)


def field_accesses(code):
	"""(var, byte_offset) struct-field accesses in a code string, both displacement
    (`var + 0xNN`) and index (`var[0xNN]`) shapes. Non-field arithmetic is filtered so it
    can't be mistaken for a member: vtable-indirect `*(T*)var + 0xNN`, code-symbol bases
    (`sub_`/`loc_`/`jump_table_`), implausibly large offsets (>= FIELD_OFFSET_MAX absolute
    addresses / sentinel constants / wrapped-negative indices), and subscript/declaration/
    address-of forms."""
	out = []
	for mm in FIELD_ACCESS_RE.finditer(code):
		base = mm.group(1)
		if base.startswith(NON_FIELD_BASE_PREFIXES):
			continue
		before = code[:mm.start(1)]
		if DEREF_CAST_TAIL_RE.search(before):
			continue  # `*(T*)var + off` -> vtable/indirect, not a field of var
		# `base[idx + 0xNN]` is INDEX arithmetic on `base`'s element type -- the
		# `+ 0xNN` belongs to the subscript, and `idx` is usually a loop counter,
		# not a pointer. e.g. `ecx_12[j_2 + 0x189]` must NOT yield a `j_2 + 0x189`
		# field. A real field base is preceded by `(`, `*`, `&`, an operator or line
		# start -- never by an open bracket. Skip when preceded by `[`.
		if BRACKET_TAIL_RE.search(before):
			continue
		off = int(mm.group(2), 16)
		if off >= FIELD_OFFSET_MAX:
			continue  # absolute address / sentinel constant, not a struct field
		out.append((base, off))
	for mm in INDEX_ACCESS_RE.finditer(code):
		base = mm.group(1)
		if base.startswith(NON_FIELD_BASE_PREFIXES):
			continue
		before = code[:mm.start(1)]
		# `TYPE name[0xNN];` is a C array DECLARATION (the bracket is the array
		# dimension, not an index) -- e.g. `char var_38[0x30];`. A real indexed
		# access (`eax_36[0x38]`) is preceded by an operator / paren / line start,
		# never by a bare type identifier. Skip when the base ident is preceded by
		# another identifier that is not a control keyword (return foo[i] etc.).
		pm = PRECEDING_IDENT_RE.search(before)
		if pm and pm.group(1) not in DECL_KEYWORD_STRS:
			continue
		# `&name[0xNN]` is ADDRESS arithmetic (take address of an element), the
		# decompiler's pointer-advance idiom `p = &p[0x11]` (stride), NOT a field
		# load. The `&` immediately before the base marks address-of, so skip it.
		if AMP_TAIL_RE.search(before):
			continue
		off = int(mm.group(2), 16) * 4
		if off >= FIELD_OFFSET_MAX:
			continue  # wrapped-negative index (e.g. base[0xffffffff]) exploded by *4
		out.append((base, off))
	return out

# A token we are allowed to rename in-place (locals + globals are matched by the
# overlay map; this is just the generic identifier shape).
IDENT_RE = re.compile(r"[A-Za-z_]\w*")

# Control-flow / statement keywords that look like "name(" but are NOT functions.
# Used to reject false positives when detecting headers structurally (a `<addr> {`
# line preceded by a same-address declaration).
DECL_KEYWORDS = {
	b"if", b"while", b"for", b"switch", b"catch", b"return", b"else", b"do",
	b"sizeof", b"case", b"goto", b"default", b"try", b"throw",
}
# The trailing identifier (function name) just before a '(' on a line.
DECL_NAME_RE = re.compile(rb"([A-Za-z_][\w:~]*)\s*$")
# A compiler annotation the decompiler emits BETWEEN the return type and the function
# name, e.g. `int32_t __convention("regparm") __allshr(int32_t, int32_t, char)`. Its
# argument list is NOT the function's parameter list, so it must be removed before the
# prefix is validated as a return type. Restricted to `__`-prefixed keywords with a
# flat argument list: surveying all 95,290 indexed headers found `__convention` to be
# the only annotation that ever occupies this position.
HEADER_ANNOTATION_RE = re.compile(rb"\b__\w+\s*\([^()]*\)\s*")
# A valid C return type, as emitted by the decompiler: optional qualifiers, an
# identifier (possibly ::-qualified, optionally one template arg), then */& and
# whitespace. Anchored to the whole prefix so expression fragments are rejected.
_RET_TYPE_RE = re.compile(
	rb"^(?:(?:struct|union|enum|const|unsigned|signed|volatile)\s+)*"
	rb"[A-Za-z_][\w:]*(?:\s*<[^>]*>)?[\s\*&]*$"
)
# Operator substrings that can never appear in a return type but do appear in
# multi-line expression / condition continuations (the main false-positive risk
# for the structural `<addr> {` rule).
_NON_TYPE_OPS = (b"&&", b"||", b"==", b"!=", b"<=", b">=", b"(", b")", b"+",
				 b"=", b"!", b"?", b"[", b"]", b"->", b"++", b"--", b"/",
				 b"%", b",", b".")


def header_name_for_line(addr_b, code):
	"""Function name for a header line under index rules (a) and (b), else None.

    (a) an explicit calling convention near the line start; (b) a self-referential
    `sub_<own-address>(` definition, which covers convention-less leaf functions.
    Rule (c) - the structural `<addr> {` follower - is header_decl_name(); it needs the
    PREVIOUS line and so stays in the scan loop.

    Extracted from `index` so that the indexer and anything AUDITING the index apply
    one rule rather than two copies of it that agree until one of them is fixed.
    """
	hm = HEADER_RE.search(code)
	if hm and hm.start() < 60:
		return hm.group(1)
	expected = b"sub_" + addr_b.lstrip(b"0")
	if re.search(rb"\b" + expected + rb"\s*\(", code):
		return expected
	return None


def depth0_ident_calls(code):
	"""Every `IDENT(` at parenthesis depth 0 on `code`, in order.

    Returns (ident, ident_start, paren_pos) tuples. Depth matters because a function's
    own parameters live at depth >= 1, so the only depth-0 candidates are the parts of
    the DECLARATOR itself: any annotation groups, and the function name.
    """
	out = []
	depth = 0
	for i in range(len(code)):
		c = code[i:i + 1]
		if c == b"(":
			if depth == 0:
				m = DECL_NAME_RE.search(code[:i])
				if m:
					out.append((m.group(1), m.start(), i))
			depth += 1
		elif c == b")" and depth:
			depth -= 1
	return out


def header_decl_name(code):
	"""If `code` (bytes) looks like a function DEFINITION, return its name token.

    A definition is `<return-type> <name>(` -- e.g. `int80_t sub_9c6920(...)`.
    Returns None for calls/statements/control flow (`if (...)`, `foo(x);`) and for
    multi-line expression continuations (`&& sub_x(arg))`), so the structural
    `<addr> {` rule stays false-positive free. (Real headers either carry a calling
    convention or are `sub_<addr>`; this only adds convention-less NAMED functions.)

    The name is taken from the LAST depth-0 `IDENT(`, not the first '(' on the line.
    Scanning to the first '(' names the function after an intervening ANNOTATION: in
      009c68f0  int32_t __convention("regparm") __allshr(int32_t, int32_t, char) __pure
    the first '(' opens the annotation's argument list, so the old scan returned
    `__convention` and 29 distinct addresses were all indexed under that one keyword -
    a name that is a calling-convention marker rather than any function's identity.
    """
	cands = depth0_ident_calls(code)
	if not cands:
		return None
	name, name_start, _paren = cands[-1]
	if name in DECL_KEYWORDS:
		return None
	# The prefix is the return type plus any annotations; drop the annotations so a
	# legitimate `int32_t __convention("regparm")` prefix still reads as `int32_t`.
	rettype = HEADER_ANNOTATION_RE.sub(b" ", code[:name_start]).strip()
	# Must have a return type (bare `name(` is a call) that looks like a real type.
	if not rettype:
		return None
	if any(op in rettype for op in _NON_TYPE_OPS):
		return None
	if not _RET_TYPE_RE.match(rettype):
		return None
	return name


# --------------------------------------------------------------------------- #
# Database
# --------------------------------------------------------------------------- #
