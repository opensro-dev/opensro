"""
test_revtool_port.py - contracts the repo port adds on top of the
research tool: one path resolver (no hard-coded drives), the deref
positional count, and the --backend rizin dispatch.

    py -3 -m unittest tools.re.revtool.test_revtool_port
"""

from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path
from unittest import mock

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import revtool  # noqa: E402
import revtool_paths  # noqa: E402


class ResolverTests(unittest.TestCase):
	def test_defaults_live_under_the_repo_state_dir(self):
		self.assertTrue(revtool_paths.DEFAULT_DB.endswith(
			os.path.join(".state", "revtool", "revtool.db")))
		self.assertTrue(revtool_paths.EVIDENCE_CURRENT.startswith(
			revtool_paths.DATA_DIR))

	def test_data_dir_env_override_wins(self):
		import importlib
		with mock.patch.dict(os.environ, {"REVTOOL_DATA_DIR": "X:/elsewhere"}):
			importlib.reload(revtool_paths)
			self.assertEqual(revtool_paths.DATA_DIR, "X:/elsewhere")
			self.assertTrue(revtool_paths.DEFAULT_DB.startswith("X:/elsewhere"))
		importlib.reload(revtool_paths)
		self.assertNotEqual(revtool_paths.DATA_DIR, "X:/elsewhere")

	def test_no_drive_letter_paths_in_defaults(self):
		for value in (revtool_paths.DEFAULT_DB, revtool_paths.DEFAULT_LEDGER,
					  revtool_paths.DEFAULT_EVIDENCE_LOCK,
					  revtool_paths.EVIDENCE_CURRENT, revtool_paths.EVIDENCE_PROGRESS):
			self.assertNotIn("sro-native-exact", value)
			self.assertNotIn("cpp-replacement-proof", value)


class DerefParserTests(unittest.TestCase):
	def parse(self, argv):
		return revtool.build_parser().parse_args(argv)

	def test_positional_count_is_accepted(self):
		args = self.parse(["deref", "0xce8a38", "5"])
		self.assertEqual(args.count, 5)
		self.assertIsNone(args.count_flag)

	def test_flag_count_still_works(self):
		args = self.parse(["deref", "0xce8a38", "--count", "7"])
		self.assertIsNone(args.count)
		self.assertEqual(args.count_flag, 7)

	def test_count_defaults_to_none_for_the_handler_default(self):
		args = self.parse(["deref", "0xce8a38"])
		self.assertIsNone(args.count)
		self.assertIsNone(args.count_flag)


class BackendDispatchTests(unittest.TestCase):
	def test_rizin_backend_is_dispatched_and_flag_stripped(self):
		fake = mock.Mock(return_value=7)
		with mock.patch.dict(sys.modules, {"revtool_rizin": mock.Mock(main=fake)}):
			rc = revtool.main(["--backend", "rizin", "--db", "x.db", "index"])
		self.assertEqual(rc, 7)
		fake.assert_called_once_with(["--db", "x.db", "index"])

	def test_rizin_backend_equals_spelling(self):
		fake = mock.Mock(return_value=0)
		with mock.patch.dict(sys.modules, {"revtool_rizin": mock.Mock(main=fake)}):
			revtool.main(["--backend=rizin", "status"])
		fake.assert_called_once_with(["status"])

	def test_unknown_backend_is_refused(self):
		with self.assertRaises(SystemExit):
			revtool.main(["--backend", "ghidra", "status"])


if __name__ == "__main__":
	unittest.main()
