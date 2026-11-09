"""
test_revtool_seed.py - the contributor seed: deterministic build,
overlay names, the size cap, the no-executable-bytes scan, and the
rizin apply against a fake pipe (the real pipe is dogfooded on the
maintainer machine, never in the suite).

    py -3 -m unittest tools.re.revtool.test_revtool_seed
"""

from __future__ import annotations

import hashlib
import io
import json
import os
import sqlite3
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import revtool  # noqa: E402
import revtool_seed  # noqa: E402


def _fixture_db(path, target_sha="ab" * 32):
	con = sqlite3.connect(path)
	con.executescript(
		"CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);"
		"CREATE TABLE functions (addr TEXT PRIMARY KEY, name TEXT NOT NULL,"
		" signature TEXT NOT NULL, start_byte INTEGER NOT NULL,"
		" end_byte INTEGER NOT NULL, start_line INTEGER NOT NULL,"
		" end_line INTEGER NOT NULL);"
		"CREATE TABLE globals (orig_name TEXT PRIMARY KEY, new_name TEXT NOT NULL,"
		" kind TEXT, note TEXT);")
	con.execute("INSERT INTO meta VALUES ('target_sha256', ?)", (target_sha,))
	con.executemany(
		"INSERT INTO functions VALUES (?, ?, '', 0, 0, 0, 0)",
		[("00401020", "sub_401020"), ("00401000", "sub_401000"),
		 ("00403000", "sub_403000")])
	con.execute(
		"INSERT INTO globals VALUES ('sub_403000', 'CIFMainFrame_Draw', 'func', NULL)")
	con.commit()
	return con


def _fixture_ledger(path, target_sha="ab" * 32):
	lines = [
		json.dumps({"_t": "_meta", "target_sha256": target_sha}),
		json.dumps({"_t": "globals", "orig_name": "sub_403000",
					"new_name": "CIFMainFrame_Draw"}),
	]
	Path(path).write_text("\n".join(lines) + "\n", encoding="utf-8")
	return lines


class BuildTests(unittest.TestCase):
	def setUp(self):
		self.dir = tempfile.TemporaryDirectory()
		self.addCleanup(self.dir.cleanup)
		self.db = os.path.join(self.dir.name, "revtool.db")
		self.ledger = os.path.join(self.dir.name, "revtool_symbols.jsonl")
		self.con = _fixture_db(self.db)
		self.addCleanup(self.con.close)
		self.lines = _fixture_ledger(self.ledger)

	def test_payload_carries_index_names_and_ledger(self):
		payload = revtool_seed.build_payload(self.con, self.ledger, None, "client")
		self.assertEqual(payload["binary_sha256"], "ab" * 32)
		self.assertEqual(
			payload["functions"],
			[[0x401000, "sub_401000"], [0x401020, "sub_401020"],
			 [0x403000, "CIFMainFrame_Draw"]])
		self.assertEqual(payload["ledger"], self.lines)

	def test_hash_sources_must_agree(self):
		other = os.path.join(self.dir.name, "other.jsonl")
		_fixture_ledger(other, target_sha="cd" * 32)
		with self.assertRaises(revtool_seed.SeedError):
			revtool_seed.build_payload(self.con, other, None, "client")

	def test_encode_is_deterministic_and_round_trips(self):
		payload = revtool_seed.build_payload(self.con, self.ledger, None, "client")
		seed = os.path.join(self.dir.name, "seed.xz")
		Path(seed).write_bytes(revtool_seed.encode_seed(payload))
		self.assertEqual(
			revtool_seed.encode_seed(payload), revtool_seed.encode_seed(payload))
		loaded = revtool_seed.load_seed(seed)
		self.assertEqual(loaded["functions"], payload["functions"])
		self.assertEqual(loaded["binary_sha256"], payload["binary_sha256"])

	def test_build_command_writes_seed_and_refuses_over_cap(self):
		out = os.path.join(self.dir.name, "out.xz")
		args = SimpleNamespace(
			db=self.db, ledger=None, binary=None, seed_binary=None,
			target="client", out=out)
		with redirect_stdout(io.StringIO()):
			self.assertEqual(revtool_seed.cmd_seed_build(args, self.con), 0)
		self.assertTrue(os.path.exists(out))
		with mock.patch.object(revtool_seed, "SEED_SIZE_LIMIT", 8):
			with redirect_stdout(io.StringIO()):
				with self.assertRaises(SystemExit):
					revtool_seed.cmd_seed_build(args, self.con)


class ScanTests(unittest.TestCase):
	def test_planted_binary_run_is_caught(self):
		run = b"CIFMainFrame::DrawText"  # 22 printable bytes
		binary = b"\x55\x8b\xec" + run + b"\x5d\xc3"
		payload = json.dumps({"name": run.decode()}).encode()
		hit = revtool_seed.find_text_run(payload, binary)
		self.assertIsNotNone(hit)
		self.assertIn(hit[0], run)
		self.assertGreaterEqual(len(hit[0]), revtool_seed.TEXT_RUN_BYTES)

	def test_clean_payload_passes(self):
		binary = bytes(range(256)) * 4
		payload = b'{"functions":[[4198400,"sub_401000"]]}'
		self.assertIsNone(revtool_seed.find_text_run(payload, binary))


class FakePipe:
	"""rzpipe stand-in: af/afn mutate a fake aflj, Ps is recorded."""

	def __init__(self, drop=()):
		self.funcs = {}
		self.drop = set(drop)
		self.saved = None

	def cmd(self, text):
		for line in text.split("\n"):
			parts = line.split()
			if not parts:
				continue
			if parts[0] == "af+" and parts[2] == "@":
				va = int(parts[3], 16)
				if va not in self.drop:
					self.funcs[va] = parts[1]
			elif parts[0] == "Ps":
				self.saved = parts[1].strip('"')
				Path(self.saved).write_bytes(b"rzdb")
		return ""

	def cmdj(self, text):
		if text != "aflj":
			raise AssertionError(text)
		return [{"offset": va, "name": name}
				for va, name in sorted(self.funcs.items())]


class ApplyTests(unittest.TestCase):
	def _payload(self):
		return {
			"format": revtool_seed.SEED_FORMAT, "version": 1,
			"target": "client", "binary_sha256": "ab" * 32,
			"binary_sha_source": "db meta",
			"functions": [[0x401000, "sub_401000"], [0x401020, "CIFDraw"],
						  [0x401040, "sub_401040"], [0x401060, "CIFHide"]],
			"ledger": [],
		}

	def test_apply_defines_names_and_saves(self):
		pipe = FakePipe(drop={0x401060})
		with tempfile.TemporaryDirectory() as td:
			rzdb = os.path.join(td, "out.rzdb")
			defined, named, ok, sampled = revtool_seed.apply_seed(
				self._payload(), "binary.exe", rzdb,
				opener=lambda binary: pipe)
			# The project landed via the tmp save; the lock is gone.
			self.assertTrue(os.path.exists(rzdb))
			self.assertTrue(pipe.saved.startswith(rzdb + ".tmp-"))
			self.assertFalse(os.path.exists(pipe.saved))
			self.assertFalse(os.path.exists(rzdb + ".lock"))
		# 0x401060 was merged away by rizin: not defined, never renamed.
		self.assertEqual((defined, named), (3, 3))
		self.assertEqual((ok, sampled), (1, 1))
		self.assertEqual(pipe.funcs[0x401020], "CIFDraw")

	def test_apply_refuses_a_held_lock(self):
		pipe = FakePipe()
		with tempfile.TemporaryDirectory() as td:
			rzdb = os.path.join(td, "out.rzdb")
			Path(rzdb + ".lock").write_text("held\n", encoding="utf-8")
			with self.assertRaises(revtool_seed.SeedError):
				revtool_seed.apply_seed(
					self._payload(), "binary.exe", rzdb,
					opener=lambda binary: pipe)
			# The other writer's lock is left alone.
			self.assertTrue(os.path.exists(rzdb + ".lock"))
			self.assertIsNone(pipe.saved)

	def test_apply_refuses_a_mismatched_binary(self):
		with tempfile.TemporaryDirectory() as td:
			binary = os.path.join(td, "other.exe")
			Path(binary).write_bytes(b"not the seed binary")
			seed = os.path.join(td, "seed.xz")
			Path(seed).write_bytes(revtool_seed.encode_seed(self._payload()))
			args = SimpleNamespace(
				seed=seed, binary=binary, seed_binary=None,
				rzdb=os.path.join(td, "x.rzdb"), limit=0, force=False)
			with self.assertRaises(SystemExit):
				revtool_seed.cmd_seed_apply(args, None)


class ParserTests(unittest.TestCase):
	def test_seed_commands_parse(self):
		args = revtool.build_parser().parse_args(
			["seed", "build", "--target", "server"])
		self.assertIs(args.handler, revtool_seed.cmd_seed_build)
		args = revtool.build_parser().parse_args(["seed", "verify", "s.xz"])
		self.assertIs(args.handler, revtool_seed.cmd_seed_verify)
		args = revtool.build_parser().parse_args(["seed", "apply", "s.xz"])
		self.assertIs(args.handler, revtool_seed.cmd_seed_apply)

	def test_seed_binary_flag_lands_on_the_subcommand(self):
		args = revtool.build_parser().parse_args(
			["seed", "apply", "s.xz", "--binary", "SRO_Client.exe"])
		self.assertEqual(args.seed_binary, "SRO_Client.exe")
		self.assertIsNone(args.binary)


if __name__ == "__main__":
	unittest.main()
