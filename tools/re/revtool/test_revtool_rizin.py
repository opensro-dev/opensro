"""
test_revtool_rizin.py - checks for revtool's rizin backend.

Runs against a fake rzpipe session, so no rizin install and no retail
binary are needed. A real end-to-end run (env SRO_RIZIN_IT=1 plus
SRO_RIZIN_BINARY) is opt-in. Run with:

    py -3 test_revtool_rizin.py            (module's directory on path)
"""

from __future__ import annotations

import contextlib
import io
import os
import sys
import tempfile
import types
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
if os.environ.get("REVTOOL_HOME"):
	sys.path.insert(0, os.environ["REVTOOL_HOME"])

try:
	import revtool  # noqa: E402
	import revtool_rizin as rz  # noqa: E402
	_MISSING = ""
except ImportError as exc:
	# revtool.py lands with its own port; until then (and on any
	# machine without it) this suite skips instead of erroring, so a
	# clean checkout stays green. Where revtool lives (REVTOOL_HOME
	# pointing at it) the suite runs in full below.
	revtool = rz = None
	_MISSING = f"revtool is not importable here: {exc}"


OPS_402A70 = [
	{"offset": 0x402A70, "disasm": "mov eax, dword [arg_8h]"},
	{"offset": 0x402A74, "disasm": "call fcn.00401100"},
	{"offset": 0x402A79, "disasm": "mov dword [0xf5b3d8], eax"},
	{"offset": 0x402A7F, "disasm": "ret"},
]

AFLJ = [
	{"offset": 0x401100, "name": "fcn.00401100", "realsz": 32,
	 "signature": "fcn.00401100(int32_t arg_4h);",
	 "callrefs": [], "datarefs": []},
	{"offset": 0x402A70, "name": "fcn.00402a70", "realsz": 139,
	 "signature": "fcn.00402a70(int32_t arg_4h, int32_t arg_8h);",
	 "callrefs": [{"from": 0x402A74, "to": 0x401100, "type": "CALL"}],
	 "datarefs": [{"from": 0x402A79, "to": 0xF5B3D8, "type": "DATA"}]},
]

AFSJ = {
	0x401100: {"ret": "int32_t", "cc": "cdecl",
	           "args": [{"name": "arg_4h", "type": "int32_t"}]},
	0x402A70: {"ret": "void", "cc": "cdecl",
	           "args": [{"name": "arg_8h", "type": "int32_t"},
	                    {"name": "arg_4h", "type": "int32_t"}]},
}


class FakeRizin:
	"""The RizinSession surface, canned."""

	rzdb = None

	def __init__(self):
		self.saved = False

	def functions(self):
		return list(AFLJ)

	def signature_parts(self, va):
		return AFSJ.get(va, {})

	def func_ops(self, va):
		return list(OPS_402A70) if va == 0x402A70 else []

	def xrefs_to(self, va):
		if va == 0x401100:
			return [{"from": 0x402A74, "to": 0x401100, "type": "CALL"}]
		return []

	def cmdj(self, text):
		if text.startswith("pdj 1 @"):
			return [{"offset": 0x402A74, "disasm": "call fcn.00401100"}]
		return None

	def save(self):
		self.saved = True


def make_args(**kw):
	base = dict(
		db=None, binary=None, author="test", strict=False, no_strict=False,
		target=None, raw=False, no_apply=False, no_legend=False, lines=None,
		force=False, pattern=None, limit=40, symbol=None, path=None,
		quiet=False, analyze="aac", rzdb=None,
	)
	base.update(kw)
	return types.SimpleNamespace(**base)


def seed_db(tmp):
	db = os.path.join(tmp, "revtool.db")
	binary = os.path.join(tmp, "SRO_Client.exe")
	with open(binary, "wb") as fh:
		fh.write(b"MZ-fake-binary")
	con = revtool.open_db(db)
	con.execute(
		"INSERT OR REPLACE INTO functions "
		"(addr,name,signature,start_byte,end_byte,start_line,end_line) "
		"VALUES (?,?,?,?,?,?,?)",
		("00401100", "sub_401100", "int32_t __cdecl sub_401100(int32_t arg_4h)", 0, 1, 0, 0))
	con.execute(
		"INSERT OR REPLACE INTO functions "
		"(addr,name,signature,start_byte,end_byte,start_line,end_line) "
		"VALUES (?,?,?,?,?,?,?)",
		("00402a70", "sub_402a70", "void __cdecl sub_402a70(int32_t arg_4h, int32_t arg_8h)", 1, 2, 0, 0))
	con.execute(
		"INSERT OR REPLACE INTO globals "
		"(orig_name,new_name,kind,note,tier,disputed,dispute_reason) "
		"VALUES (?,?,?,?,?,?,?)",
		("sub_401100", "HelperFree", "func", None, "verified", 0, None))
	con.execute(
		"INSERT OR REPLACE INTO globals "
		"(orig_name,new_name,kind,note,tier,disputed,dispute_reason) "
		"VALUES (?,?,?,?,?,?,?)",
		("data_f5b3d8", "g_counter", "data", None, "mechanism", 0, None))
	con.commit()
	return con, binary


def capture(fn, *a, **kw):
	buf = io.StringIO()
	with contextlib.redirect_stdout(buf):
		rc = fn(*a, **kw)
	return rc, buf.getvalue()


@unittest.skipIf(_MISSING, _MISSING)
class TestSpelling(unittest.TestCase):
	def test_bn_token(self):
		self.assertEqual(rz.bn_token("fcn.00402a70", 0x402A70), "sub_402a70")
		self.assertEqual(rz.bn_token("sub.00679b90", 0x679B90), "sub_679b90")
		self.assertEqual(rz.bn_token("sym.imp.Sleep", 0x501000), "sym.imp.Sleep")
		self.assertEqual(rz.bn_token(None, 0x401000), "sub_401000")

	def test_bn_signature(self):
		sig = rz.bn_signature(AFLJ[1], AFSJ[0x402A70], "sub_402a70")
		self.assertEqual(
			sig, "void __cdecl sub_402a70(int32_t arg_8h, int32_t arg_4h)")
		fallback = rz.bn_signature(AFLJ[0], {}, "sub_401100")
		self.assertEqual(fallback, "sub_401100(int32_t arg_4h)")

	def test_normalize_line(self):
		data = {0xF5B3D8: "data_f5b3d8"}
		self.assertEqual(rz.normalize_line("call fcn.00401100", data),
		                 "call sub_401100")
		self.assertEqual(rz.normalize_line("mov dword [0xf5b3d8], eax", data),
		                 "mov dword [data_f5b3d8], eax")
		self.assertEqual(rz.normalize_line("cmp eax, 0x80", data), "cmp eax, 0x80")


@unittest.skipIf(_MISSING, _MISSING)
class TestCommands(unittest.TestCase):
	def open_seeded(self):
		tmp = tempfile.TemporaryDirectory()
		self.addCleanup(tmp.cleanup)
		con, binary = seed_db(tmp.name)
		self.addCleanup(con.close)  # LIFO: closes before tmp.cleanup
		return con, binary, tmp.name

	def test_index(self):
		con, binary, tmp = self.open_seeded()
		con.execute("DELETE FROM functions")
		con.commit()
		args = make_args(db=os.path.join(tmp, "revtool.db"), binary=binary)
		fake = FakeRizin()
		rc, out = capture(rz.cmd_index, args, con, fake)
		self.assertEqual(rc, 0)
		rows = con.execute(
			"SELECT addr,name,signature FROM functions ORDER BY addr").fetchall()
		self.assertEqual([r[1] for r in rows], ["sub_401100", "sub_402a70"])
		self.assertEqual(rows[1][0], "00402a70")
		self.assertTrue(rows[1][2].startswith("void __cdecl sub_402a70("))
		self.assertIn("indexed 2 functions", out)
		self.assertEqual(
			con.execute("SELECT value FROM meta WHERE key='backend'").fetchone()[0],
			"rizin")

	def test_view_applies_overlay(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(target="sub_402a70", binary=binary)
		rc, out = capture(rz.cmd_view, args, con, FakeRizin())
		self.assertEqual(rc, 0)
		self.assertIn("call HelperFree", out)       # rename applied
		self.assertIn("data_f5b3d8", out)            # known datum spelled
		self.assertIn("renamed in this view", out)

	def test_view_raw_keeps_rizin_spelling(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(target="0x402a70", binary=binary, raw=True)
		rc, out = capture(rz.cmd_view, args, con, FakeRizin())
		self.assertEqual(rc, 0)
		self.assertIn("call fcn.00401100", out)
		self.assertNotIn("HelperFree", out)

	def test_xref_groups_callers(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(symbol="HelperFree", binary=binary)
		rc, out = capture(rz.cmd_xref, args, con, FakeRizin())
		self.assertEqual(rc, 0)
		self.assertIn("xref HelperFree -> sub_401100 (HelperFree): 1 reference(s)", out)
		self.assertIn("in sub_402a70", out)
		self.assertIn("call sub_401100", out)

	def test_xref_rejects_text_queries(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(symbol="some_opcode_text", binary=binary)
		rc, _out = capture(rz.cmd_xref, args, con, FakeRizin())
		self.assertEqual(rc, 1)

	def test_coverage_counts_unlabeled(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(target="sub_402a70", binary=binary)
		rc, out = capture(rz.cmd_coverage, args, con, FakeRizin())
		# sub_401100 is renamed (labeled); data_f5b3d8 is labeled too.
		self.assertEqual(rc, 0)
		self.assertIn("SYMBOL COVERAGE: 100%", out)
		self.assertIn("data globals:1/1 labeled", out)

	def test_find_delegates_without_rizin(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(pattern="402a70", binary=binary)
		rc, out = capture(revtool.cmd_find, args, con)
		self.assertEqual(rc, 0)
		self.assertIn("sub_402a70", out)


@unittest.skipIf(_MISSING, _MISSING)
@unittest.skipUnless(os.environ.get("SRO_RIZIN_IT"),
                     "set SRO_RIZIN_IT=1 (+SRO_RIZIN_BINARY) for the live run")
class TestLiveRizin(unittest.TestCase):
	def test_index_view_xref_live(self):
		binary = os.environ["SRO_RIZIN_BINARY"]
		tmp = tempfile.TemporaryDirectory()
		self.addCleanup(tmp.cleanup)
		db = os.path.join(tmp.name, "revtool.db")
		con = revtool.open_db(db)
		self.addCleanup(con.close)
		args = make_args(db=db, binary=binary, analyze="aac")
		rzdb = os.path.join(tmp.name, "t.rzdb")
		session = rz.RizinSession(binary, rzdb=rzdb, analyze="aac")
		try:
			rc, out = capture(rz.cmd_index, args, con, session)
			self.assertEqual(rc, 0)
			n = con.execute("SELECT COUNT(*) FROM functions").fetchone()[0]
			self.assertGreater(n, 1000)
		finally:
			session.close()


if __name__ == "__main__":
	unittest.main()
