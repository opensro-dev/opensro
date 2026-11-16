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
import json
import os
import sys
import tempfile
import types
import unittest
from unittest import mock

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
		self.analyzed = []

	def functions(self):
		return list(AFLJ)

	def signature_parts(self, va):
		return AFSJ.get(va, {})

	def signature_parts_many(self, vas):
		return {va: self.signature_parts(va) for va in vas}

	def analyze_project(self, step):
		self.analyzed.append(step)
		return 0.0

	def strings(self):
		# izj starts this string one character early, as rizin did live.
		return [{"vaddr": 0x500000, "type": "ascii", "string": "oUIIT_MSG_GUILD_RECALL"},
		        {"vaddr": 0x500100, "type": "ascii", "string": "unused text"}]

	def func_ops(self, va):
		return list(OPS_402A70) if va == 0x402A70 else []

	def xrefs_to(self, va):
		if va == 0x401100:
			return [{"from": 0x402A74, "to": 0x401100, "type": "CALL"}]
		if va == 0x500001:
			return [{"from": 0x402A78, "to": 0x500001, "type": "DATA"}]
		return []

	def cmdj(self, text):
		if text.startswith("pdj 1 @"):
			return [{"offset": 0x402A74, "disasm": "call fcn.00401100"}]
		return None

	def save(self):
		self.saved = True

	def close(self):
		pass


class FakePipe:
	"""The rzpipe surface RizinSession.save drives: `Ps` writes a file."""

	def __init__(self):
		self.saved_to = None

	def cmd(self, text):
		if text.startswith('Ps "') and text.endswith('"'):
			self.saved_to = text[4:-1]
			with open(self.saved_to, "wb") as fh:
				fh.write(b"rzdb-bytes")
		return ""

	def quit(self):
		pass


def bare_session(rzdb):
	"""A RizinSession with a fake pipe, without opening rizin."""
	session = rz.RizinSession.__new__(rz.RizinSession)
	session.rzdb = rzdb
	session._pipe = FakePipe()
	return session


def make_args(**kw):
	base = dict(
		db=None, binary=None, author="test", strict=False, no_strict=False,
		target=None, raw=False, no_apply=False, no_legend=False, lines=None,
		force=False, pattern=None, limit=40, symbol=None, path=None,
		quiet=False, analyze="aac", rzdb=None,
		scope=None, a=None, b=None, tier="guess", evidence=None, basis=None,
		method=None, blindspots=None, shallow=False, gate_lines=None,
		ledger=None, json=False, action=None, take=None,
	)
	base.update(kw)
	return types.SimpleNamespace(**base)


def rizin_db(tmp):
	"""A small rizin-backed overlay: fake binary, project, artifact row."""
	db = os.path.join(tmp, "contributor.db")
	rzdb = os.path.join(tmp, "contributor.rzdb")
	binary = os.path.join(tmp, "SRO_Client.exe")
	with open(binary, "wb") as fh:
		fh.write(b"MZ-fake-binary")
	with open(rzdb, "wb") as fh:
		fh.write(b"fake-project")
	con = revtool.open_db(db)
	revtool.meta_set(con, "backend", "rizin")
	revtool.meta_set(con, "target_sha256", revtool.sha256_file(binary))
	con.execute(
		"INSERT OR REPLACE INTO functions "
		"(addr,name,signature,start_byte,end_byte,start_line,end_line) "
		"VALUES (?,?,?,?,?,?,?)",
		("00401100", "sub_401100", "int32_t sub_401100()", 0, 1, 0, 0))
	con.execute(
		"INSERT OR REPLACE INTO functions "
		"(addr,name,signature,start_byte,end_byte,start_line,end_line) "
		"VALUES (?,?,?,?,?,?,?)",
		("00402a70", "sub_402a70", "void sub_402a70()", 1, 2, 0, 0))
	revtool.record_artifact(
		con, "rizin-analysis", rzdb, record_count=2,
		producer="rizin aflj analysis", version=rz.RIZIN_VERSION)
	con.commit()
	return con, db, rzdb, binary


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

	def test_index_note_names_a_loaded_project(self):
		con, binary, tmp = self.open_seeded()
		args = make_args(db=os.path.join(tmp, "revtool.db"), binary=binary)
		fake = FakeRizin()
		fake.from_project = True
		rc, out = capture(rz.cmd_index, args, con, fake)
		self.assertEqual(rc, 0)
		self.assertIn("seed-applied", out)
		# A seeded project has no references until the pass runs.
		self.assertEqual(fake.analyzed, ["aar"])

	def test_index_refs_none_skips_the_pass_and_xref_warns(self):
		con, binary, tmp = self.open_seeded()
		args = make_args(db=os.path.join(tmp, "revtool.db"), binary=binary, refs="none")
		fake = FakeRizin()
		fake.from_project = True
		capture(rz.cmd_index, args, con, fake)
		self.assertEqual(fake.analyzed, [])
		args = make_args(symbol="sub_402a70", binary=binary)
		err = io.StringIO()
		with contextlib.redirect_stderr(err):
			rc, _out = capture(rz.cmd_xref, args, con, fake)
		self.assertEqual(rc, 0)
		self.assertIn("--refs none", err.getvalue())

	def test_xref_takes_an_indexed_function_name(self):
		con, binary, _tmp = self.open_seeded()
		con.execute("UPDATE functions SET name = 'Helper_Named' WHERE addr = '00401100'")
		args = make_args(symbol="Helper_Named", binary=binary)
		rc, out = capture(rz.cmd_xref, args, con, FakeRizin())
		self.assertEqual(rc, 0)
		self.assertIn("1 reference(s)", out)
		self.assertNotIn("not a known symbol", out)
		self.assertIn("callers: 1", out)

	def test_strings_finds_the_user_past_an_early_start(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(pattern="guild_recall", binary=binary, regex=False)
		rc, out = capture(rz.cmd_strings, args, con, FakeRizin())
		self.assertEqual(rc, 0)
		self.assertIn("1 match(es)", out)
		self.assertIn("sub_402a70  [NEEDS RE]  @ 00402a78", out)

	def test_strings_labels_a_named_user(self):
		con, binary, _tmp = self.open_seeded()
		con.execute("UPDATE functions SET name = 'CIFGuild_OnCreate' WHERE addr = '00402a70'")
		args = make_args(pattern="GUILD_RECALL", binary=binary, regex=False)
		_rc, out = capture(rz.cmd_strings, args, con, FakeRizin())
		self.assertIn("CIFGuild_OnCreate  [labeled]", out)

	def test_string_ref_offsets_follow_char_width(self):
		self.assertEqual(rz.string_ref_offsets({"type": "utf16le"}, 2), [0, 2, 4])
		self.assertEqual(rz.string_ref_offsets({"type": "ascii"}, 0), [0])
		self.assertEqual(len(rz.string_ref_offsets({"type": "ascii"}, 999)), rz.STRING_REF_SCAN_MAX)

	def test_rename_global_stores_claim_and_view_applies_it(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(scope="global", a="sub_402a70", b="WidgetDraw")
		rc, out = capture(rz.cmd_rename, args, con, FakeRizin())
		self.assertEqual(rc, 0)
		self.assertIn("global: sub_402a70 -> WidgetDraw", out)
		row = con.execute(
			"SELECT new_name FROM globals WHERE orig_name='sub_402a70'").fetchone()
		self.assertEqual(row[0], "WidgetDraw")
		view_args = make_args(target="sub_402a70", binary=binary)
		rc, out = capture(rz.cmd_view, view_args, con, FakeRizin())
		self.assertEqual(rc, 0)
		self.assertIn("WidgetDraw", out)

	def test_rename_local_stores_claim(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(scope="local", func="sub_402a70", a="arg_8h", b="widget")
		rc, out = capture(rz.cmd_rename, args, con, FakeRizin())
		self.assertEqual(rc, 0)
		row = con.execute(
			"SELECT new_name FROM locals "
			"WHERE func_addr='00402a70' AND orig_name='arg_8h'").fetchone()
		self.assertEqual(row[0], "widget")

	def test_rename_guess_tier_opens_no_session(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(scope="global", a="sub_402a70", b="WidgetDraw")
		with mock.patch.object(rz, "open_session",
		                       side_effect=AssertionError("session opened")):
			rc, _out = capture(rz.cmd_rename, args, con, FakeRizin())
		self.assertEqual(rc, 0)

	def test_rename_deep_tier_unmeasured_disassembly_passes(self):
		# Strict + mechanism: the gate measures the disassembly, finds no
		# `var + 0xNN` shape in it, and passes rather than refusing.
		con, binary, _tmp = self.open_seeded()
		args = make_args(scope="global", a="sub_402a70", b="WidgetDraw",
		                 tier="mechanism")
		with mock.patch.object(rz, "open_session",
		                       return_value=FakeRizin()) as opened:
			rc, _out = capture(rz.cmd_rename, args, con, FakeRizin())
		self.assertEqual(rc, 0)
		opened.assert_called_once()
		row = con.execute(
			"SELECT new_name FROM globals WHERE orig_name='sub_402a70'").fetchone()
		self.assertEqual(row[0], "WidgetDraw")

	def test_depth_gate_measures_supplied_lines(self):
		con, binary, _tmp = self.open_seeded()
		args = make_args(tier="mechanism")
		structured = [f"00402a{0x70 + i:02x}  mov eax, this + 0x{0x10 + 4 * i:x}"
		              for i in range(4)]
		err = io.StringIO()
		with contextlib.redirect_stderr(err):
			rc = revtool.depth_gate(con, args, None, "sub_402a70",
			                        "mechanism", lines=structured)
		self.assertEqual(rc, 2)
		self.assertIn("REFUSED (strict)", err.getvalue())
		with contextlib.redirect_stderr(io.StringIO()):
			rc = revtool.depth_gate(con, args, None, "sub_402a70",
			                        "mechanism", lines=structured[:3])
		self.assertIsNone(rc)
		# No lines and no dump: unmeasurable passes, never a crash.
		rc = revtool.depth_gate(con, args, None, "sub_402a70", "mechanism")
		self.assertIsNone(rc)

	def test_session_save_writes_tmp_then_replaces(self):
		tmp = tempfile.TemporaryDirectory()
		self.addCleanup(tmp.cleanup)
		rzdb = os.path.join(tmp.name, "p.rzdb")
		session = bare_session(rzdb)
		session.save()
		self.assertEqual(session._pipe.saved_to, f"{rzdb}.tmp-{os.getpid()}")
		with open(rzdb, "rb") as fh:
			self.assertEqual(fh.read(), b"rzdb-bytes")
		self.assertFalse(os.path.exists(session._pipe.saved_to))
		self.assertFalse(os.path.exists(rzdb + ".lock"))

	def test_session_save_refuses_a_held_lock(self):
		tmp = tempfile.TemporaryDirectory()
		self.addCleanup(tmp.cleanup)
		rzdb = os.path.join(tmp.name, "p.rzdb")
		with open(rzdb + ".lock", "w") as fh:
			fh.write("99999\n")
		session = bare_session(rzdb)
		with self.assertRaises(SystemExit):
			session.save()
		self.assertIsNone(session._pipe.saved_to)
		# The foreign lock is left exactly as found.
		with open(rzdb + ".lock") as fh:
			self.assertEqual(fh.read(), "99999\n")

	def test_session_save_without_project_is_a_noop(self):
		session = bare_session(None)
		session.save()
		self.assertIsNone(session._pipe.saved_to)


@unittest.skipIf(_MISSING, _MISSING)
class TestDoctorRizin(unittest.TestCase):
	def open_rizin(self):
		tmp = tempfile.TemporaryDirectory()
		self.addCleanup(tmp.cleanup)
		con, db, rzdb, binary = rizin_db(tmp.name)
		self.addCleanup(con.close)  # LIFO: closes before tmp.cleanup
		return con, db, rzdb, binary

	def test_doctor_passes_a_healthy_rizin_db(self):
		con, db, rzdb, binary = self.open_rizin()
		args = make_args(db=db, binary=binary, rzdb=rzdb)
		rc, out = capture(revtool.cmd_doctor, args, con)
		self.assertEqual(rc, 0)
		self.assertIn("doctor: PASS", out)
		self.assertIn("PASS  rizin-project", out)
		self.assertIn("PASS  db-function-index", out)

	def test_doctor_json_shape(self):
		con, db, rzdb, binary = self.open_rizin()
		args = make_args(db=db, binary=binary, rzdb=rzdb, json=True)
		rc, out = capture(revtool.cmd_doctor, args, con)
		self.assertEqual(rc, 0)
		result = json.loads(out)
		self.assertEqual(result["schema"], "sro-revtool-doctor-v1")
		self.assertTrue(result["ok"])

	def test_cli_doctor_on_an_unindexed_db_fails_closed(self):
		# A contributor's first `--backend rizin doctor`, before any
		# `index`: the DB carries no backend mark yet. It used to fall
		# into the dump doctor and crash on args.lock.
		tmp = tempfile.TemporaryDirectory()
		self.addCleanup(tmp.cleanup)
		binary = os.path.join(tmp.name, "SRO_Client.exe")
		with open(binary, "wb") as fh:
			fh.write(b"MZ")
		db = os.path.join(tmp.name, "fresh.db")
		rc, out = capture(rz.main, ["--db", db, "--binary", binary, "doctor"])
		self.assertEqual(rc, 2)
		self.assertIn("doctor: FAIL", out)

	def test_doctor_fails_without_the_project(self):
		con, db, rzdb, binary = self.open_rizin()
		os.remove(rzdb)
		args = make_args(db=db, binary=binary, rzdb=rzdb)
		rc, out = capture(revtool.cmd_doctor, args, con)
		self.assertEqual(rc, 2)
		self.assertIn("FAIL  rizin-project", out)

	def test_doctor_fails_on_a_swapped_binary(self):
		con, db, rzdb, binary = self.open_rizin()
		with open(binary, "ab") as fh:
			fh.write(b"x")
		args = make_args(db=db, binary=binary, rzdb=rzdb)
		rc, out = capture(revtool.cmd_doctor, args, con)
		self.assertEqual(rc, 2)
		self.assertIn("FAIL  db-target-binding", out)

	def test_doctor_requires_an_export_once_claims_exist(self):
		con, db, rzdb, binary = self.open_rizin()
		con.execute(
			"INSERT INTO globals(orig_name,new_name,kind,tier) "
			"VALUES('sub_401100','HelperFree','func','guess')")
		con.commit()
		args = make_args(db=db, binary=binary, rzdb=rzdb)
		rc, out = capture(revtool.cmd_doctor, args, con)
		self.assertEqual(rc, 2)
		self.assertIn("FAIL  ledger", out)
		sync_args = make_args(db=db, action="export")
		rc, _out = capture(revtool.cmd_sync, sync_args, con)
		self.assertEqual(rc, 0)
		rc, out = capture(revtool.cmd_doctor, args, con)
		self.assertEqual(rc, 0)
		self.assertIn("PASS  ledger-claims", out)


class Rizin082Pipe:
	"""
	================
	Rizin082Pipe

	What rizin 0.8.2 does with a newline script, as measured on
	2026-11-09: `?e` does not exist and `@@` starts an iterator, so a
	script holding either fails to parse whole and answers nothing;
	`echo` prints its word; afsj on an address with no function
	prints nothing at all.
	================
	"""

	def __init__(self, known):
		self.known = known

	def cmd(self, text):
		if "?e " in text or "@@" in text:
			return ""
		out = []
		for line in text.split("\n"):
			if line.startswith("echo "):
				out.append(line[5:] + "\n")
			elif line.startswith("afsj @ "):
				va = int(line[7:], 16)
				if va in self.known:
					out.append(json.dumps(self.known[va]) + "\n")
		return "".join(out)


class TestSignatureBatch(unittest.TestCase):
	def test_batch_survives_rizin_parsing_and_gaps(self):
		session = object.__new__(rz.RizinSession)
		session._pipe = Rizin082Pipe(AFSJ)
		vas = [0x401000, 0x7FFFFF] + sorted(AFSJ)
		parts = session.signature_parts_many(vas)
		self.assertEqual(parts[0x7FFFFF], {})
		for va, want in AFSJ.items():
			self.assertEqual(parts[va], want, hex(va))


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
			# The batched afsj must reach real rizin intact: a separator
			# rizin cannot parse empties every answer silently.
			vas = [f["offset"] for f in session.functions()[:50]]
			parts = session.signature_parts_many(vas)
			self.assertGreater(sum(1 for p in parts.values() if p), 40)
		finally:
			session.close()


if __name__ == "__main__":
	unittest.main()
