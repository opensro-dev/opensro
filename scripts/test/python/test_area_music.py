"""
===========================================================================

test_area_music.py - which music areas a variant takes, and in what layout

The area list follows the regions the variant holds (#3546: the Storm and
Cloud Desert had terrain but no area, so it was silent). Synthetic tables:
the VSRO areas are appended verbatim when one of their regions is held, the
v1.696 per-region tables are converted to the v1.150 layout, a region keeps
the first area that claims it, and the regions left without an area are
reported.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import hashlib
import tempfile
import unittest
from pathlib import Path

import area_music as music  # noqa: E402


def cp949(rows):
	return ("\r\n".join(rows) + "\r\n").encode("cp949")


class MusicTests(unittest.TestCase):
	TOWN = "알렉"
	FIELD = "델타"

	def tables(self):
		native = {
			"regioninfo.txt": cp949(["#TOWN\t도적\t\t\t\t\t", "182\t96\tALL\t\t\t\t"]),
			"effectenvsnd.txt": cp949(["<1>\t도적\t\t\t\t\t", '\t"EasternEurope_Field.ogg"\t\t\t\t\t'])
		}
		vsro = {
			"regioninfo.txt": cp949([
				"#TOWN\t" + self.TOWN + "\t\t\t\t\t", "48\t95\tALL\t\t\t\t", "\t\t\t\t\t\t", "\t\t\t\t\t\t",
				"#FIELD\t" + self.FIELD + "\t\t\t\t\t", "50\t93\tALL\t\t\t\t", "#FIELD\tother\t\t\t\t\t", "1\t2\tALL\t\t\t\t"
			]),
			"effectenvsnd.txt": cp949([
				"<1>\t" + self.TOWN + "\t\t\t\t", '\t"Egypt_Town_Alexandria1.ogg"\t\t\t', "\t<2>\t낮\t\t\t",
				'\t\t\t<3>\t"night_wind.wav"\t0~0', "<1>\t" + self.FIELD + "\t\t\t\t", '\t"Egypt_Field_Delta.ogg"\t\t\t',
				"<1>\tother\t\t\t\t", '\t"Other.ogg"\t\t\t'
			])
		}
		return native, vsro

	def test_the_areas_are_appended_in_the_native_layout_and_name_their_tracks(self):
		native, vsro = self.tables()
		merged, tracks = music.merge_music_tables(native, vsro, areas=(self.TOWN, self.FIELD))
		self.assertEqual(tracks, ["Egypt_Field_Delta.ogg", "Egypt_Town_Alexandria1.ogg"])
		regions = merged["regioninfo.txt"].decode("cp949").split("\r\n")
		self.assertEqual(regions[:2], ["#TOWN\t도적\t\t\t\t\t", "182\t96\tALL\t\t\t\t"])
		self.assertIn("48\t95\tALL\t\t\t\t", regions)
		self.assertIn("50\t93\tALL\t\t\t\t", regions)
		self.assertNotIn("1\t2\tALL\t\t\t\t", regions)
		sounds = merged["effectenvsnd.txt"].decode("cp949").split("\r\n")
		self.assertTrue(all(len(row.split("\t")) == music.EFFECTENVSND_COLUMNS for row in sounds if row))
		self.assertIn('\t\t\t<3>\t"night_wind.wav"\t0~0\t', sounds)
		self.assertNotIn("<1>\tother\t\t\t\t\t", sounds)

	def test_an_area_the_native_tables_have_is_left_alone(self):
		native, vsro = self.tables()
		merged, _ = music.merge_music_tables(native, vsro, areas=(self.TOWN,))
		again, tracks = music.merge_music_tables(merged, vsro, areas=(self.TOWN,))
		self.assertEqual(tracks, [])
		self.assertEqual(again, merged)

	def test_a_missing_area_is_refused(self):
		native, vsro = self.tables()
		with self.assertRaises(SystemExit):
			music.merge_music_tables(native, vsro, areas=("nowhere",))




class AreaSelectionTests(unittest.TestCase):
	VSRO = [
		"#TOWN\t알렉\t\t\t\t\t", "48\t95\tALL\t\t\t\t",
		"#FIELD\t사막\t\t\t\t\t", "73\t84\tALL\t\t\t\t", "74\t84\tALL\t\t\t\t",
		"#FIELD\t신전\tegypt\t\t\t\t", "16\t128\tALL\t\t\t\t",
		"#FIELD\t아레나\t\t\t\t\t", "215\t126\tALL\t\t\t\t"
	]

	# ================
	# test_an_area_is_wanted_when_the_variant_holds_any_of_its_regions
	#
	# The reported desert (73,84) is wanted with the city; the arena, whose
	# regions the variant lacks, is not; a known area is skipped.
	# ================
	def test_an_area_is_wanted_when_the_variant_holds_any_of_its_regions(self):
		regions = {(48, 95), (73, 84)}
		self.assertEqual(music.wanted_areas(self.VSRO, regions), ["알렉", "사막"])
		self.assertEqual(music.wanted_areas(self.VSRO, regions, known={"알렉": []}), ["사막"])

	def test_a_dungeon_area_is_wanted_by_its_dungeon_id(self):
		regions = {(16, music.DUNGEON_SECTOR_Y)}
		self.assertEqual(music.wanted_areas(self.VSRO, regions), ["신전"])
		info = '1\t"Dungeon\\a.dof"\r\n16\t"Dungeon\\b.dof"\r\n'.encode("cp949")
		self.assertEqual(music.dungeon_ids(info), [1, 16])

	def test_the_variant_regions_are_its_terrain_files_and_dungeons(self):
		with tempfile.TemporaryDirectory() as scratch:
			out = Path(scratch)
			(out / "Map_extracted" / "84").mkdir(parents=True)
			(out / "Map_extracted" / "84" / "73.m").write_bytes(b"")
			(out / "Map_extracted" / "84" / "73.o").write_bytes(b"")
			(out / "Map_extracted" / "tile2d").mkdir()
			self.assertEqual(music.variant_regions(out, [16]), {(73, 84), (16, music.DUNGEON_SECTOR_Y)})

	def test_imported_regions_without_an_area_are_reported(self):
		regions = {(73, 84), (90, 90), (20, music.DUNGEON_SECTOR_Y)}
		self.assertEqual(music.silent_regions(self.VSRO, regions), [(90, 90)])


class LaterAreaTests(unittest.TestCase):
	# The v1.696 Data regioninfo.txt: one row per region, the area on each.
	REGIONS = [
		"#TOWN\t바그다드\t88\t89\tALL\t0\t0\t0\t0",
		"#TOWN\t바그다드\t89\t89\tRECT\t0\t220\t1920\t220",
		"#FIELD\t사막\t73\t84\tALL\t0\t0\t0\t0",
		"#FIELD\t바그다드던젼\t40\t128\tarabia\t0\t0\t0\t0",
		"#FIELD\t카크\t95\t95\tALL\t0\t0\t0\t0",
		"#FIELD\t카크\t88\t89\tALL\t0\t0\t0\t0"
	]
	# The v1.696 effectenvsnd.txt: area, kind (0 track, 1 day, 2 night), file, range.
	SOUNDS = [
		'바그다드\t0\t"ARABIA_TOWN.ogg"\t0~0', '바그다드\t1\t"day_wind.wav"\t0~0',
		'바그다드\t2\t"night_wind.wav"\t10~20', '바그다드던젼\t0\t"ARABIA_DUNGEON.ogg"\t0~0',
		'카크\t0\t"ARABIA_FIELD.ogg"\t0~0'
	]
	PRESENT = ["#FIELD\t사막\t\t\t\t\t", "73\t84\tALL\t\t\t\t"]

	# ================
	# test_a_later_area_is_converted_to_the_v1150_layout
	#
	# Header, track, a "<2>" period per ambience kind with its "<3>" sounds;
	# the region rows keep their RECT numbers and a dungeon its page.
	# ================
	def test_a_later_area_is_converted_to_the_v1150_layout(self):
		regions = {(88, 89), (89, 89), (40, music.DUNGEON_SECTOR_Y)}
		region_rows, sound_rows, names = music.later_music_rows(
			self.PRESENT, self.REGIONS, self.SOUNDS, regions, music.EFFECTENVSND_COLUMNS
		)
		self.assertEqual(names, ["바그다드", "바그다드던젼"])
		self.assertEqual(region_rows, [
			"#TOWN\t바그다드\t\t\t\t\t", "88\t89\tALL\t\t\t\t", "89\t89\tRECT\t0\t220\t1920\t220",
			"#FIELD\t바그다드던젼\tarabia\t\t\t\t", "40\t128\tALL\t\t\t\t"
		])
		self.assertEqual(sound_rows[:5], [
			"<1>\t바그다드\t\t\t\t\t", '\t"ARABIA_TOWN.ogg"\t\t\t\t\t', "\t<2>\t낮\t\t\t\t",
			'\t\t\t<3>\t"day_wind.wav"\t0~0\t', "\t\t\t\t\t\t"
		])
		self.assertIn('\t\t\t<3>\t"night_wind.wav"\t10~20\t', sound_rows)
		self.assertEqual(music.music_tracks(sound_rows), ["ARABIA_DUNGEON.ogg", "ARABIA_TOWN.ogg"])

	# ================
	# test_a_region_keeps_the_first_area_that_claims_it
	#
	# The desert is already present, so its later twin adds nothing; 88,89
	# goes to Baghdad first, so Kakh keeps only the region nobody claimed,
	# and an area left with no held region is dropped.
	# ================
	def test_a_region_keeps_the_first_area_that_claims_it(self):
		regions = {(73, 84), (88, 89)}
		region_rows, _, names = music.later_music_rows(
			self.PRESENT, self.REGIONS, self.SOUNDS, regions, music.EFFECTENVSND_COLUMNS
		)
		self.assertEqual(names, ["바그다드"])
		self.assertNotIn("73\t84\tALL\t\t\t\t", region_rows)
		_, _, names = music.later_music_rows(
			self.PRESENT, self.REGIONS, self.SOUNDS, regions | {(95, 95)}, music.EFFECTENVSND_COLUMNS
		)
		self.assertEqual(names, ["바그다드", "카크"])

	# ================
	# test_an_area_with_no_sound_section_is_left_out
	#
	# Without its sounds Baghdad is skipped and claims nothing, so Kakh,
	# the next area on 88,89, takes the region.
	# ================
	def test_an_area_with_no_sound_section_is_left_out(self):
		regions = {(88, 89)}
		region_rows, _, names = music.later_music_rows(
			[], self.REGIONS, self.SOUNDS[3:], regions, music.EFFECTENVSND_COLUMNS
		)
		self.assertEqual(names, ["카크"])
		self.assertEqual(region_rows[1:], ["88\t89\tALL\t\t\t\t"])


class LaterTrackTests(unittest.TestCase):
	# ================
	# pinned_root
	#
	# A folder with a stand-in Music.pk2 and the pins that admit it.
	# ================
	def pinned_root(self, folder):
		archive = Path(folder) / "Music.pk2"
		archive.write_bytes(b"later music archive")
		return folder, {"Music": hashlib.sha256(archive.read_bytes()).hexdigest()}

	# ================
	# test_a_later_track_the_vsro_archive_lacks_comes_from_the_v1696_archive
	#
	# Only the later areas' tracks are asked of the v1.696 Music.pk2; a VSRO
	# area's absent track stays reported as before.
	# ================
	def test_a_later_track_the_vsro_archive_lacks_comes_from_the_v1696_archive(self):
		asked = []

		def convert(root, tracks, out):
			asked.append(list(tracks))
			return [Path(track).stem + ".mp3" for track in tracks], []

		with tempfile.TemporaryDirectory() as folder:
			root, pins = self.pinned_root(folder)
			converted, absent = music.source_later_tracks(
				["ARABIA_TOWN.ogg", "VSRO_ONLY.ogg"], ["ARABIA_TOWN.ogg", "JUPITER.ogg"], Path(folder), root,
				convert, pins
			)
		self.assertEqual(asked, [["ARABIA_TOWN.ogg"]])
		self.assertEqual(converted, ["ARABIA_TOWN.mp3"])
		self.assertEqual(absent, ["VSRO_ONLY.ogg"])

	# ================
	# test_a_later_track_no_archive_holds_stops_the_build
	# ================
	def test_a_later_track_no_archive_holds_stops_the_build(self):
		with tempfile.TemporaryDirectory() as folder:
			root, pins = self.pinned_root(folder)
			with self.assertRaisesRegex(SystemExit, "ARABIA_TOWN.ogg"):
				music.source_later_tracks(
					["ARABIA_TOWN.ogg"], ["ARABIA_TOWN.ogg"], Path(folder), root,
					lambda root, tracks, out: ([], list(tracks)), pins
				)

	# ================
	# test_an_unpinned_later_archive_is_refused_before_it_is_read
	# ================
	def test_an_unpinned_later_archive_is_refused_before_it_is_read(self):
		with tempfile.TemporaryDirectory() as folder:
			root, _ = self.pinned_root(folder)
			with self.assertRaises(SystemExit):
				music.source_later_tracks(
					["ARABIA_TOWN.ogg"], ["ARABIA_TOWN.ogg"], Path(folder), root,
					lambda *args: self.fail("read before the pin check"), {"Music": "0" * 64}
				)


if __name__ == "__main__":
	unittest.main()
