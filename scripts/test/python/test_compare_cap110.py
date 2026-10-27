"""
===========================================================================

test_compare_cap110.py - cross-version comparison without licensed data

Synthetic tables exercise renumbering, shifted columns, ambiguous keys,
experience changes and rejection of unsafe archive manifest entries.

===========================================================================
"""
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "analysis"))
import compare_cap110 as comparison  # noqa: E402


# ================
# ComparisonTests
# ================
class ComparisonTests(unittest.TestCase):
	# ================
	# test_ids_do_not_define_identity
	# ================
	def test_ids_do_not_define_identity(self):
		left = [["1", "10", "ITEM_A", "x"], ["1", "11", "ITEM_REMOVED", "y"]]
		right = [["1", "11", "ITEM_A", "z"], ["1", "10", "ITEM_NEW", "y"]]
		report = comparison.compare_rows(left, right, 2)
		self.assertEqual(report["sharedUniqueCodes"], 1)
		self.assertEqual(report["renumberedSharedCodes"], 1)
		self.assertEqual(report["addedCodes"], ["ITEM_NEW"])
		self.assertEqual(report["removedCodes"], ["ITEM_REMOVED"])
		self.assertEqual(report["columns"][3]["differences"], [{"code": "ITEM_A", "native": "x", "later": "z"}])

	# ================
	# test_ambiguous_keys_are_not_silently_overwritten
	# ================
	def test_ambiguous_keys_are_not_silently_overwritten(self):
		left = [["1", "1", "A"], ["1", "2", "A"]]
		right = [["1", "3", "A"]]
		report = comparison.compare_rows(left, right, 2)
		self.assertEqual(report["nativeDuplicateCodes"], ["A"])
		self.assertEqual(report["sharedUniqueCodes"], 0)
		self.assertEqual(report["addedCodes"], [])

	# ================
	# test_shorter_layout_is_not_padded_or_guessed
	# ================
	def test_shorter_layout_is_not_padded_or_guessed(self):
		report = comparison.compare_rows([["1", "1", "A", "10"]], [["1", "2", "A"]], 2)
		self.assertEqual(report["nativeWidths"], {4: 1})
		self.assertEqual(report["laterWidths"], {3: 1})
		self.assertEqual(report["columns"][3]["compared"], 0)

	# ================
	# test_curve_uses_level_keys_and_wide_integers
	# ================
	def test_curve_uses_level_keys_and_wide_integers(self):
		report = comparison.compare_levels([["110", "1250000000"], ["1", "118"]], [["1", "118"], ["110", "4040000000"]])
		self.assertEqual(report["differences"], [{"level": 110, "nativeExp": 1250000000, "laterExp": 4040000000}])
		with self.assertRaises(ValueError):
			comparison.compare_levels([["1", "118"], ["1", "119"]], [])

	# ================
	# test_manifest_cannot_escape_or_repeat_a_shard
	# ================
	def test_manifest_cannot_escape_or_repeat_a_shard(self):
		for text in ("../itemdata_1.txt", "itemdata_1.txt\nITEMDATA_1.TXT", ""):
			with self.subTest(text=text), patch.object(comparison.archive, "table_text", return_value=text.encode("ascii")):
				with self.assertRaises(ValueError):
					comparison.read_tables(None, {}, "itemdata")


class MainPinTests(unittest.TestCase):
	def setUp(self):
		comparison.archive._verified_archives.clear()
		self.root = tempfile.TemporaryDirectory()
		self.addCleanup(self.root.cleanup)
		self.vsro = Path(self.root.name)

	def run_main(self):
		argv = ["compare_cap110.py", "--vsro-root", str(self.vsro)]
		with patch.object(sys, "argv", argv):
			comparison.main()

	def test_main_refuses_a_client_that_is_not_the_pinned_one(self):
		(self.vsro / "Media.pk2").write_bytes(b"some other client")
		with self.assertRaisesRegex(SystemExit, "does not match the pinned"):
			self.run_main()

	def test_main_refuses_a_missing_archive(self):
		with self.assertRaisesRegex(SystemExit, "not found"):
			self.run_main()


if __name__ == "__main__":
	unittest.main()
