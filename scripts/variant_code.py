"""
===========================================================================

variant_code.py - what code a variant step answers for, and its key

variant_steps.py records, while a step runs, the files and the functions
of its code that started; this module turns that record into the step's
code key as the checkout holds it now. A Python file keys by its syntax
tree, so a comment or a layout change is not code; a file the step only
imported keys with every function body emptied; a file it ran keys only
the functions that started in it. Anything finer would need a call graph
the recorder already gives for free; anything coarser reruns steps for
code they never ran (#4251, #4337).

===========================================================================
"""
from __future__ import annotations

import ast
import hashlib
import re
import sysconfig
from pathlib import Path

FLAG_LITERAL = re.compile(r"SRO_[A-Z0-9_]+")
# Where interpreter and package code lives: never a step's own code.
INSTALLED_CODE = tuple(
	str(Path(path).resolve()) for path in {sysconfig.get_path(name) for name in ("stdlib", "platstdlib", "purelib", "platlib")}
)


#============================================================================
# A step's code: what it ran, and what that code names
#============================================================================


# ================
# is_installed_code
#
# A file of the interpreter or of an installed package: pinned by the
# toolchain, never a step's own code.
# ================
def is_installed_code(path: str) -> bool:
	return path.startswith("<") or path.startswith(INSTALLED_CODE)


# ================
# code_location
#
# How the journal names one code file: repository-relative inside the
# checkout, so a release built from a fresh freeze checkout keys every
# step exactly as the last one did (r204i: each new rel-<sha> checkout
# moved every key, #4251); absolute outside it.
# ================
def code_location(path: Path, repository_root: Path) -> str:
	try:
		return path.resolve().relative_to(repository_root.resolve()).as_posix()
	except ValueError:
		return str(path.resolve())


# ================
# code_path
#
# The file a recorded code location names under this checkout.
# ================
def code_path(location: str, repository_root: Path) -> Path:
	path = Path(location)
	return path if path.is_absolute() else repository_root / location


# ================
# code_key
#
# A digest of the step's code files, each by its location and content,
# and of the SRO_* flags they name: the step reruns when its code changes
# or when a flag it names moves. A file that is gone keys as gone.
#
# A Python file keys by its syntax tree (code_content), not its bytes: a
# comment or a blank line is not code. ran names the files whose code the
# step ran (record_run); every other Python file in the closure was only
# imported, so its function bodies key as empty. The smoke's one-comment
# edit to alexandria_npcs.py had made 22 of 28 steps stale (#4251).
#
# functions narrows a ran file further, to the functions that started in
# it (by qualified name): a shared module keyed whole made every step
# that called one of its helpers stale when any other helper changed.
# r204n reran 23 of 28 steps for 1850 s on edits to alexandria_npcs.py,
# build_progress.py and mall_avatar_import.py functions those steps
# never called (#4337). A record without functions (journaled before
# they were kept) keys its ran files whole, as it was keyed then.
# ================
def code_key(files: list[Path], repository_root: Path, environ, ran: frozenset[str] = frozenset(),
		functions: dict[str, tuple] | None = None) -> str:
	digest = hashlib.sha256()
	flags: dict[str, str] = {}
	for path in sorted(files, key=lambda item: code_location(item, repository_root)):
		location = code_location(path, repository_root)
		digest.update(location.encode("utf-8") + b"\0")
		try:
			content = path.read_bytes()
		except OSError:
			digest.update(b"gone\n")
			continue
		keep = None if functions is None else functions.get(location)
		keyed = code_content(path, content, location in ran, keep)
		digest.update(hashlib.sha256(keyed).hexdigest().encode("ascii") + b"\n")
		for name in FLAG_LITERAL.findall(content.decode("utf-8", "ignore")):
			value = environ.get(name)
			flags[name] = hashlib.sha256(value.encode("utf-8")).hexdigest() if value is not None else ""
	for name in sorted(flags):
		digest.update(f"flag\0{name}\0{flags[name]}\n".encode("utf-8"))
	return digest.hexdigest()


# ================
# frozen_functions
#
# A record's functions ({location: [qualified names]}) in a form a cache
# key can hold, or None for a record journaled before they were kept.
# ================
def frozen_functions(functions: dict | None):
	if functions is None:
		return None
	return tuple(sorted((location, tuple(names)) for location, names in functions.items()))


# ================
# BodyStripper
#
# Empties every function body but those keep names (qualified names, as
# a code object's co_qualname gives them: "f", "Class.method"), leaving
# the def itself: its name, its decorators and the defaults its arguments
# evaluate when the module loads, which is all a function the step never
# started can do. A kept function keeps its nested defs whole; a nested
# def of a function that never started cannot have started either.
# ================
class BodyStripper(ast.NodeTransformer):
	def __init__(self, keep=frozenset()):
		self.keep = keep
		self.scope = []

	def visit_FunctionDef(self, node):
		if ".".join(self.scope + [node.name]) in self.keep:
			return node
		node.body = [ast.Pass()]
		return node

	visit_AsyncFunctionDef = visit_FunctionDef

	def visit_ClassDef(self, node):
		self.scope.append(node.name)
		self.generic_visit(node)
		self.scope.pop()
		return node


# ================
# code_content
#
# What a code file keys by. A Python file is its syntax tree, which
# drops comments, blank lines and layout; when its code did not run in
# the step, the tree has its function bodies emptied (BodyStripper). The
# recorder sees every function that starts (record_run), so a body it
# did not see start cannot have touched the step; no step runs Python in
# a child process (the children are Node scripts and ffmpeg, keyed as
# bytes). keep, when given, names the functions that started in a file
# that ran: the other bodies key as empty too. A file that does not
# parse, and every other file, keys by its bytes.
# ================
def code_content(path: Path, content: bytes, ran: bool, keep=None) -> bytes:
	if path.suffix != ".py":
		return content
	try:
		tree = ast.parse(content)
	except (SyntaxError, ValueError):
		return content
	if not ran:
		tree = BodyStripper().visit(tree)
	elif keep is not None:
		tree = BodyStripper(frozenset(keep)).visit(tree)
	return ast.dump(tree).encode("utf-8")
