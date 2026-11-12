"""
===========================================================================

test_pet_import.py - the later clients' pet import

Synthetic rows cover the default-on switch, kind classification,
selection (existing, deferred fellow kinds, skipped kinds without a
scroll or a model), identity retention and reassignment, and the
pinned-archive source's import, refusal and removal, all without a
real client tree.

===========================================================================
"""
import hashlib
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
import unittest.mock

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import avatar_archive  # noqa: E402
import cap110_items as items  # noqa: E402
import pet_import as importer  # noqa: E402

CHAR_COLUMNS = 104


# ================
# char_row / scroll
# ================
def char_row(code, identity, level="1", model="cos\\newpet.bsr"):
	row = ["0"] * CHAR_COLUMNS
	row[importer.CHAR_ID] = str(identity)
	row[importer.CHAR_CODE] = code
	row[importer.CHAR_NAME_KEY] = "SN_" + code
	row[importer.CHAR_MODEL] = model
	row[importer.CHAR_LEVEL] = level
	return row


def scroll(code, identity, assoc):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE] = "1"
	row[items.IDENTITY] = str(identity)
	row[items.CODENAME] = code
	row[items.NAME_KEY] = "SN_" + code
	row[importer.ITEM_ASSOCIATED_CHAR] = assoc
	return row


def native_wolf():
	return ([char_row("COS_P_WOLF_001", 6106)], [scroll("ITEM_PET_WOLF", 3000, "COS_P_WOLF")])


class SwitchTests(unittest.TestCase):
	def test_unset_is_on(self):
		with unittest.mock.patch.dict(os.environ, {}, clear=True):
			self.assertTrue(importer.enabled())

	def test_explicit_off_is_off(self):
		for value in importer.FLAG_OFF:
			with unittest.mock.patch.dict(os.environ, {importer.FLAG: value}):
				self.assertFalse(importer.enabled())

	def test_unreadable_value_is_refused(self):
		with unittest.mock.patch.dict(os.environ, {importer.FLAG: "sometimes"}):
			with self.assertRaises(ValueError):
				importer.enabled()


class KindTests(unittest.TestCase):
	def test_kinds_follow_prefix_and_shape(self):
		self.assertEqual(importer.kind_of("COS_P_FOX", [char_row("COS_P_FOX_001", 1), char_row("COS_P_FOX_040", 2, "40")]), "attack")
		self.assertEqual(importer.kind_of("COS_P_DOG", [char_row("COS_P_DOG", 1)]), "pickup")
		self.assertEqual(importer.kind_of("COS_T_MULE", [char_row("COS_T_MULE", 1)]), "transport")
		self.assertEqual(importer.kind_of("COS_C_CAMEL9", [char_row("COS_C_CAMEL9", 1)]), "vehicle")
		self.assertEqual(importer.kind_of("COS_F_FAIRY", [char_row("COS_F_FAIRY", 1)]), "fellow")
		self.assertEqual(importer.kind_of("COS_GUARD_CH", [char_row("COS_GUARD_CH", 1)]), "excluded")


class SelectionTests(unittest.TestCase):
	def select(self, later_chars, later_items, native=None, earlier=(), previous=None, missing=lambda family, rows: []):
		native_chars, native_items = native or native_wolf()
		return importer.select_pets(native_chars, native_items, set(earlier), later_chars, later_items,
			previous=previous, missing=missing)

	def test_new_pickup_kind_is_chosen_with_its_scroll(self):
		chars, item_rows, report = self.select(
			[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")])
		self.assertEqual([row[importer.CHAR_CODE] for row in chars], ["COS_P_DOG"])
		self.assertEqual([row[items.CODENAME] for row in item_rows], ["ITEM_PET_DOG"])
		kind = report["added"][0]
		self.assertEqual((kind["kind"], kind["levels"], kind["scrolls"]), ("pickup", [1], ["ITEM_PET_DOG"]))
		self.assertEqual(kind["needs"], ["petdesign entry"])

	def test_attack_kind_reports_its_level_span_and_skill_need(self):
		chars, _, report = self.select(
			[char_row("COS_P_FOX_001", 9001), char_row("COS_P_FOX_040", 9002, "40")],
			[scroll("ITEM_PET_FOX", 9003, "COS_P_FOX")])
		self.assertEqual(len(chars), 2)
		kind = report["added"][0]
		self.assertEqual((kind["kind"], kind["levels"]), ("attack", [1, 40]))
		self.assertIn("pet skill rows", kind["needs"])

	def test_fellow_kind_rides_the_attack_lane(self):
		# Fellows are admitted like attack kinds (#3328): rows and scroll
		# import, they fight with their native rows, and the report owes
		# no design entry (the expansion's design is attack-pet only).
		chars, item_rows, report = self.select(
			[char_row("COS_F_FAIRY", 9001)], [scroll("ITEM_PET_FAIRY", 9002, "COS_F_FAIRY")])
		self.assertEqual((len(chars), len(item_rows)), (1, 1))
		kind = report["added"][0]
		self.assertEqual((kind["family"], kind["kind"]), ("COS_F_FAIRY", "fellow"))
		self.assertEqual(kind["needs"], [])

	def test_kind_without_a_scroll_is_skipped(self):
		chars, _, report = self.select([char_row("COS_P_DOG", 9001)], [])
		self.assertEqual(chars, [])
		self.assertEqual(report["skipped"][0]["missing"], ["summon scroll"])

	def test_kind_without_a_model_is_skipped(self):
		chars, _, report = self.select(
			[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")],
			missing=lambda family, rows: ["model"])
		self.assertEqual(chars, [])
		self.assertEqual(report["skipped"][0]["missing"], ["model"])

	def test_native_family_is_existing(self):
		chars, _, report = self.select([char_row("COS_P_WOLF_001", 6106)], [])
		self.assertEqual(chars, [])
		self.assertEqual(report["existing"], ["COS_P_WOLF"])

	def test_partial_family_imports_only_its_fresh_rows(self):
		chars, _, report = self.select(
			[char_row("COS_P_WOLF_001", 6106), char_row("COS_P_WOLF_040", 9001, "40")],
			[scroll("ITEM_PET_WOLF2", 9002, "COS_P_WOLF")])
		self.assertEqual([row[importer.CHAR_CODE] for row in chars], ["COS_P_WOLF_040"])
		self.assertEqual(report["added"][0]["levels"], [40])

	def test_earlier_source_wins_the_kind(self):
		chars, _, report = self.select(
			[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")],
			earlier={"COS_P_DOG"})
		self.assertEqual(chars, [])
		self.assertEqual(report["existing"], ["COS_P_DOG"])

	def test_reserved_identity_is_not_handed_out(self):
		# Identities already placed in the staging tree by an earlier
		# source or another importer are taken even though the native
		# tables never held them.
		chars, item_rows, _ = self.select(
			[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")])
		plain = (chars[0][importer.CHAR_ID], item_rows[0][items.IDENTITY])
		chars, item_rows, _ = importer.select_pets(
			*native_wolf(), set(), [char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")],
			reserved_ids={9001, 9002})
		self.assertNotEqual((chars[0][importer.CHAR_ID], item_rows[0][items.IDENTITY]), plain)
		self.assertNotIn(int(chars[0][importer.CHAR_ID]), (9001, 9002))
		self.assertNotIn(int(item_rows[0][items.IDENTITY]), (9001, 9002))

	def test_taken_identity_gets_a_fresh_one_and_previous_is_retained(self):
		native = ([char_row("COS_P_WOLF_001", 9001)], [])
		chars, _, _ = self.select([char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")], native=native)
		self.assertGreater(int(chars[0][importer.CHAR_ID]), 9001)
		chars, _, _ = self.select([char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")],
			native=native, previous={"COS_P_DOG": 4242})
		self.assertEqual(chars[0][importer.CHAR_ID], "4242")


class ArchiveTests(unittest.TestCase):
	def _tree(self, root, chars, item_rows):
		text_root = root / importer.TEXT_ROOT
		text_root.mkdir(parents=True, exist_ok=True)
		(text_root / "characterdata_5000.txt").write_bytes(importer._encode(chars))
		(text_root / "characterdata.txt").write_bytes(importer._encode([["characterdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(importer._encode(item_rows))
		(text_root / "itemdata.txt").write_bytes(importer._encode([["itemdata_5000.txt"]]))

	# The pets ride the archive the avatar pass reads: entries at their
	# pk2 paths under media/ and data/, the whole zip pinned by sha256.
	# Returns (zip path, pin file); the pin names the zip's real hash.
	def _archive(self, folder, entries):
		zip_path = Path(folder) / "opensro_avatars.zip"
		avatar_archive.write_archive(zip_path, entries)
		pin_path = Path(folder) / "avatars_archive_pins.json"
		pin_path.write_text(json.dumps({
			"format": avatar_archive.PIN_FORMAT,
			"sha256": hashlib.sha256(zip_path.read_bytes()).hexdigest()}), encoding="utf-8")
		return zip_path, pin_path

	def _entries(self, chars, item_rows, texts=(), mixed_case=False):
		text = "media/" + importer.tables.TEXTDATA
		shard = "CharacterData_5000.txt" if mixed_case else "characterdata_5000.txt"
		entries = {
			f"{text}characterdata.txt": importer._encode([[shard]]),
			f"{text}{shard}": importer._encode(chars),
			f"{text}itemdata.txt": importer._encode([["itemdata_5000.txt"]]),
			f"{text}itemdata_5000.txt": importer._encode(item_rows),
			"data/res/cos/newpet.bsr": b"bsr",
		}
		if texts:
			entries[f"{text}textdata_object.txt"] = importer._encode(texts)
		return entries

	def _run(self, zip_path, pin_path, native, output, env=None):
		if env is None:
			env = {importer.AVATARS_ENV: str(zip_path)}
		with unittest.mock.patch.object(importer, "AVATARS_PIN_FILE", pin_path), \
				unittest.mock.patch.dict(os.environ, env, clear=True):
			return importer.add_archive_pets(native, output)

	def test_archive_source_imports_and_unset_removes(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			output.mkdir()
			# Shard names in a manifest are mixed-case in the real
			# clients (CharacterData_5000.txt); selection must fold.
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")], mixed_case=True))
			report = self._run(zip_path, pin_path, native, output)
			self.assertTrue(report["enabled"])
			self.assertEqual([kind["family"] for kind in report["added"]], ["COS_P_DOG"])
			text_root = output / importer.TEXT_ROOT
			self.assertTrue((text_root / importer.ARCHIVE_SOURCE["char_shard"]).exists())
			self.assertTrue((output / "Data_extracted" / "res" / "cos" / "newpet.bsr").exists())
			marker = json.loads((output / importer.ARCHIVE_SOURCE["report"]).read_text(encoding="utf-8"))
			self.assertEqual(marker["idMap"]["COS_P_DOG"]["target"], 9001)
			removed = self._run(zip_path, pin_path, native, output, env={})
			self.assertFalse(removed["enabled"])
			self.assertFalse((text_root / importer.ARCHIVE_SOURCE["char_shard"]).exists())

	def test_an_archive_with_the_wrong_sha256_is_refused(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			output.mkdir()
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")]))
			pin_path.write_text(json.dumps({
				"format": avatar_archive.PIN_FORMAT, "sha256": "0" * 64}), encoding="utf-8")
			with unittest.mock.patch.object(importer, "AVATARS_PIN_FILE", pin_path), \
					unittest.mock.patch.dict(os.environ, {importer.AVATARS_ENV: str(zip_path)}, clear=True):
				with self.assertRaises(SystemExit):
					importer.add_archive_pets(native, output)
			# The refusal lands before a byte is read: nothing imported.
			text_root = output / importer.TEXT_ROOT
			self.assertFalse((text_root / importer.ARCHIVE_SOURCE["char_shard"]).exists())
			self.assertFalse((output / importer.ARCHIVE_SOURCE["report"]).exists())
			self.assertFalse((output / "Data_extracted").exists())

	def test_an_archive_without_pet_tables_imports_nothing(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			output.mkdir()
			zip_path, pin_path = self._archive(base, {"data/res/cos/newpet.bsr": b"bsr"})
			report = self._run(zip_path, pin_path, native, output)
			self.assertFalse(report["enabled"])
			self.assertFalse((output / importer.TEXT_ROOT / importer.ARCHIVE_SOURCE["char_shard"]).exists())

	def test_shard_identity_used_by_an_earlier_import_is_reserved(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			text_root = output / importer.TEXT_ROOT
			text_root.mkdir(parents=True)
			# An earlier import already placed identity 9001 in the tree.
			(text_root / "characterdata_7777.txt").write_bytes(importer._encode([char_row("COS_P_CAT", 9001)]))
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")]))
			self._run(zip_path, pin_path, native, output)
			marker = json.loads((output / importer.ARCHIVE_SOURCE["report"]).read_text(encoding="utf-8"))
			self.assertNotEqual(marker["idMap"]["COS_P_DOG"]["target"], 9001)

	def test_scroll_name_text_is_merged_with_the_kind_name(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			texts = [["1", "SN_COS_P_DOG", "k", "", "", "Dog", "Dog", "a", "b", "c"],
				["1", "SN_ITEM_PET_DOG", "k", "", "", "Dog scroll", "Dog scroll", "a", "b", "c"]]
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")], texts))
			report = self._run(zip_path, pin_path, native, output)
			self.assertEqual(report["missingNameText"], [])
			name_rows = importer.characters.translation_rows((output / importer.TEXT_ROOT / "textdataname.txt").read_bytes())
			self.assertTrue({"SN_COS_P_DOG", "SN_ITEM_PET_DOG"} <= {row[1] for row in name_rows if len(row) > 1})

	def test_a_pass_that_selects_nothing_drops_its_manifest_lines(self):
		with tempfile.TemporaryDirectory() as folder:
			base = Path(folder)
			native, output = base / "native", base / "out"
			self._tree(native, *native_wolf())
			zip_path, pin_path = self._archive(base, self._entries(
				[char_row("COS_P_DOG", 9001)], [scroll("ITEM_PET_DOG", 9002, "COS_P_DOG")]))
			self._run(zip_path, pin_path, native, output)
			self._archive(base, self._entries([], []))
			self._run(zip_path, pin_path, native, output)
			text_root = output / importer.TEXT_ROOT
			for manifest_name, shard in (("characterdata.txt", importer.ARCHIVE_SOURCE["char_shard"]),
					("itemdata.txt", importer.ARCHIVE_SOURCE["item_shard"])):
				names = [row[0] for row in importer.tables.table_rows((text_root / manifest_name).read_bytes()) if row]
				self.assertNotIn(shard, names)
				self.assertFalse((text_root / shard).exists())


if __name__ == "__main__":
	unittest.main()
