"""
===========================================================================

test_title_import.py - the title table cut's contract (#3502 slice 1).

RefTitle rows join their SN code's English Speech&Name format; a title
whose text no chunk resolves is left out (and counted), a format
without %s resolves nothing, and a malformed RefTitle row refuses.
The rendered file is one title per line, sorted by id.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import tempfile
import unittest
from pathlib import Path

import title_import as titles  # noqa: E402

REF_TEXT = "\n".join([
	"2\tTITLE_NORMAL_INVESTMENT_TRADE_03\t0\tSN_TITLE_NORMAL_INVESTMENT_TRADE_03\t0\t0\tNULL\tNULL",
	"1\tTITLE_NORMAL_GOLD_USE_01\t0\tSN_TITLE_NORMAL_GOLD_USE_01\t0\t0\tNULL\tNULL",
	"5\tTITLE_UNIQUE_MOB_SNAKE_SLAYER\t2\tSN_TITLE_UNIQUE_MOB_SNAKE_SLAYER\t0\t0\tNULL\tNULL",
	"9\tTITLE_EVENT_PLAIN\t4\tSN_TITLE_EVENT_PLAIN\t0\t0\tNULL\tNULL",
])


def speech_row(code, english):
	# The Speech&Name layout: code third, English tenth (arabia_import).
	return "\t".join(["1", "6499", code, "Korean", "0", "0", "0", "0", "0", english,
		"0", "0", "0", "Turkish", "Spanish", "Arabic", "German"])


SPEECH_TEXT = "\n".join([
	speech_row("SN_TITLE_NORMAL_GOLD_USE_01", "<Big Spender> %s"),
	speech_row("SN_TITLE_NORMAL_INVESTMENT_TRADE_03", "<Investor> %s"),
	speech_row("SN_TITLE_EVENT_PLAIN", "No placeholder"),
	speech_row("SN_NPC_SHOP", "<Shop> %s"),
])


class TitleImportTest(unittest.TestCase):
	# ================
	# test_rows_join_and_sort
	# ================
	def test_rows_join_and_sort(self):
		rows, unresolved = titles.build_rows(REF_TEXT, [SPEECH_TEXT])
		self.assertEqual(rows, [
			(1, "TITLE_NORMAL_GOLD_USE_01", 0, "<Big Spender> %s"),
			(2, "TITLE_NORMAL_INVESTMENT_TRADE_03", 0, "<Investor> %s"),
		])
		# The snake-slayer text is absent and the event format has no %s:
		# both stay unresolved, in table order.
		self.assertEqual(unresolved, ["SN_TITLE_UNIQUE_MOB_SNAKE_SLAYER", "SN_TITLE_EVENT_PLAIN"])

	# ================
	# test_render_shape
	# ================
	def test_render_shape(self):
		rows, _ = titles.build_rows(REF_TEXT, [SPEECH_TEXT])
		body = titles.render(rows).decode("utf-8")
		lines = body.splitlines()
		self.assertTrue(lines[0].startswith("# opensro-titles-v1"))
		self.assertEqual(lines[1], "1\tTITLE_NORMAL_GOLD_USE_01\t0\t<Big Spender> %s")
		self.assertTrue(body.endswith("\n"))

	# ================
	# test_malformed_ref_row_refuses
	# ================
	def test_malformed_ref_row_refuses(self):
		with self.assertRaises(ValueError):
			titles.parse_ref_titles("x\tTITLE\t0\tSN_TITLE_X")
		with self.assertRaises(ValueError):
			titles.parse_ref_titles("1\tTITLE\t9\tSN_TITLE_X")
		with self.assertRaises(ValueError):
			titles.parse_ref_titles("1\tTITLE")


class TitleBuildTest(unittest.TestCase):
	# ================
	# test_plan_skips_when_no_client_named
	# ================
	def test_plan_skips_when_no_client_named(self):
		with tempfile.TemporaryDirectory() as scratch:
			run, root = titles.title_plan({}, default_root=Path(scratch) / "absent")
		self.assertFalse(run)
		self.assertIsNone(root)

	# ================
	# test_plan_runs_for_a_named_client_with_its_archive
	# ================
	def test_plan_runs_for_a_named_client_with_its_archive(self):
		with tempfile.TemporaryDirectory() as scratch:
			root = Path(scratch)
			(root / "Media.pk2").write_bytes(b"pk2")
			run, named = titles.title_plan({"SRO_V1696_ROOT": str(root)})
		self.assertTrue(run)
		self.assertEqual(named, root)

	# ================
	# test_plan_refuses_a_named_client_without_its_archive
	# ================
	def test_plan_refuses_a_named_client_without_its_archive(self):
		with tempfile.TemporaryDirectory() as scratch:
			with self.assertRaises(SystemExit):
				titles.title_plan({"SRO_V1696_ROOT": scratch})

	# ================
	# test_verify_media_refuses_an_archive_off_the_pin
	# ================
	def test_verify_media_refuses_an_archive_off_the_pin(self):
		with tempfile.TemporaryDirectory() as scratch:
			(Path(scratch) / "Media.pk2").write_bytes(b"not the pinned archive")
			with self.assertRaises(SystemExit):
				titles.verify_media(scratch)

	# ================
	# test_add_titles_skip_reaps_a_stale_table
	# ================
	def test_add_titles_skip_reaps_a_stale_table(self):
		with tempfile.TemporaryDirectory() as scratch:
			out = Path(scratch) / "variant"
			target = out / titles.TEXT_ROOT / "titles.txt"
			target.parent.mkdir(parents=True)
			target.write_bytes(b"stale")
			summary = titles.add_titles(Path(scratch), out, {}, default_root=Path(scratch) / "absent")
			self.assertEqual(summary, {"enabled": False, "titles": 0})
			self.assertFalse(target.exists())


if __name__ == "__main__":
	unittest.main()
