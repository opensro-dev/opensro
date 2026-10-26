"""
===========================================================================

test_import_vsro_alexandria.py - the flagged Alexandria importer

Synthetic tables and terrain, no game data: the SRO_ALEXANDRIA gate, the
region and gate selection from the VSRO tables, the terrain tile scan and
the native-versus-VSRO catalog comparison.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import hashlib
import struct
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import import_vsro_alexandria as alex  # noqa: E402


def utf16_table(rows):
	text = "\r\n".join("\t".join(row) for row in rows) + "\r\n"
	return b"\xff\xfe" + text.encode("utf-16-le")


def terrain(texture_ids):
	"""A JMXVMAPM1000 file whose vertices cycle through texture_ids."""
	data = bytearray(alex.MAPM_SIGNATURE)
	data += bytes(alex.MAP_BLOCKS * alex.MAPM_BLOCK_BYTES)
	count = 0
	for block in range(alex.MAP_BLOCKS):
		first = alex.MAPM_SIGNATURE_BYTES + block * alex.MAPM_BLOCK_BYTES + alex.MAPM_BLOCK_HEAD_BYTES
		for vertex in range(alex.MAPM_VERTICES):
			at = first + vertex * alex.MAPM_VERTEX_BYTES + alex.MAPM_VERTEX_TEXTURE_OFFSET
			# The high bits are the texture scale, not part of the id.
			struct.pack_into("<H", data, at, texture_ids[count % len(texture_ids)] | 0x8400)
			count += 1
	return bytes(data)


class FlagTests(unittest.TestCase):
	def test_unset_and_off_values_are_native(self):
		for env in ({}, {alex.FLAG_ENV: ""}, {alex.FLAG_ENV: "0"}, {alex.FLAG_ENV: "off"}, {alex.FLAG_ENV: "no"}):
			self.assertFalse(alex.flag_enabled(env), env)

	def test_on_values_enable(self):
		for value in ("1", "on", "ON", "true", " True "):
			self.assertTrue(alex.flag_enabled({alex.FLAG_ENV: value}), value)


class RegionTests(unittest.TestCase):
	def test_city_regions_read_the_code_column_and_decode_the_id(self):
		raw = utf16_table([
			["//Service", "Region ID", "Code Name"],
			["1", str((95 << 8) | 48), "RN_SD_EGYPT01"],
			["1", str((89 << 8) | 46), "RN_SD_EGYPT01"],
			["1", str((95 << 8) | 48), "RN_SD_EGYPT01"],
			["1", str((90 << 8) | 48), "RN_SD_EGYPT02"],
			["0", str((90 << 8) | 49), "RN_SD_EGYPT01"]
		])
		# Duplicates fold, other codes stay out, order is row (Y) then column (X).
		self.assertEqual(alex.city_regions(raw), [(46, 89), (49, 90), (48, 95)])

	def test_a_cp949_table_reads_too(self):
		raw = "1\t24625\tRN_SD_EGYPT01\t알렉\n".encode("cp949")
		self.assertEqual(alex.city_regions(raw), [(49, 96)])

	def test_paths_follow_the_archive_layout(self):
		self.assertEqual(alex.region_files(48, 90), ["90/48.m", "90/48.o", "90/48.o2", "90/48.t"])
		self.assertEqual(alex.navmesh_path(0x2E, 0x59), "navmesh/nv_592e.nvm")


def availability(regions):
	"""A JMXVMFO availability file enabling the (sectorX, sectorY) regions."""
	data = bytearray(alex.MFO_SIGNATURE) + struct.pack("<HH", alex.MFO_WIDTH, alex.MFO_HEIGHT)
	data += bytes(alex.MFO_HEADER_BYTES - len(data)) + bytes(alex.MFO_WIDTH * alex.MFO_HEIGHT // 8)
	for x, y in regions:
		region_id = (y << 8) | x
		data[alex.MFO_HEADER_BYTES + (region_id >> 3)] |= 0x80 >> (region_id & 7)
	return bytes(data)


class AvailabilityTests(unittest.TestCase):
	def test_the_bitmap_round_trips(self):
		regions = {(0, 0), (48, 90), (255, 127), (7, 8)}
		self.assertEqual(alex.mfo_regions(availability(regions)), regions)

	def test_a_wrong_file_is_refused(self):
		with self.assertRaises(ValueError):
			alex.mfo_regions(b"nope" * 20)
		bad = bytearray(availability([]))
		bad[alex.MFO_SIGNATURE_BYTES] = 1
		with self.assertRaises(ValueError):
			alex.mfo_regions(bytes(bad))

	def test_connected_regions_follow_touching_candidates_only(self):
		seeds = [(10, 10)]
		candidates = {(11, 11), (12, 12), (10, 9), (30, 30), (12, 14)}
		# (12, 14) touches nothing the walk reaches; (30, 30) is far away.
		self.assertEqual(alex.connected_regions(seeds, candidates), [(10, 9), (10, 10), (11, 11), (12, 12)])

	# ================
	# test_field_seeds_include_empty_terrain_without_connecting_unrelated_areas
	# ================
	def test_field_seeds_include_empty_terrain_without_connecting_unrelated_areas(self):
		city, fields = {(50, 90)}, {(67, 73)}
		candidates = {(50, 90), (51, 90), (67, 73), (67, 74), (68, 75), (120, 80)}
		self.assertEqual(alex.connected_regions(city, candidates), [(50, 90), (51, 90)])
		self.assertEqual(alex.connected_regions(city, candidates, fields),
			[(67, 73), (67, 74), (68, 75), (50, 90), (51, 90)])

	def test_the_seeds_stay_even_with_no_candidates(self):
		self.assertEqual(alex.connected_regions([(5, 6), (4, 6)], set()), [(4, 6), (5, 6)])


class TerrainTests(unittest.TestCase):
	def test_tile_ids_drop_the_scale_bits(self):
		self.assertEqual(alex.terrain_tile_ids(terrain([5, 262, 487])), {5, 262, 487})

	def test_a_wrong_file_is_refused(self):
		with self.assertRaises(ValueError):
			alex.terrain_tile_ids(b"JMXVMAPM1000")
		with self.assertRaises(ValueError):
			alex.terrain_tile_ids(b"X" * (alex.MAPM_SIGNATURE_BYTES + alex.MAP_BLOCKS * alex.MAPM_BLOCK_BYTES))


class CatalogTests(unittest.TestCase):
	def test_tile_catalog_compares_placeholders_additions_and_conflicts(self):
		native = alex.tile_catalog(
			b'JMXV2DTI1001\r\n3\r\n'
			b'00000 0x00000000 "A" "a.ddj"\r\n'
			b'00001 0x00000000 "X" "xxx.ddj"\r\n'
			b'00002 0x00000000 "B" "b.ddj"\r\n'
		)
		vsro = alex.tile_catalog(
			b'JMXV2DTI1001\r\n4\r\n'
			b'00000 0x00000000 "A" "a.ddj"\r\n'
			b'00001 0x00000000 "Pharaoh" "pha_grass_01.ddj"\r\n'
			b'00002 0x00000000 "B" "other.ddj"\r\n'
			b'00003 0x00000000 "C" "c.ddj"\r\n'
		)
		report = alex.compare_catalogs(native, vsro, alex.tile_placeholder)
		self.assertEqual(report, {"added": [1, 3], "conflicts": [2], "native_only": []})

	def test_object_catalog_flags_difference_is_a_conflict(self):
		native = alex.object_catalog(b'JMXVOBJI1000\r\n1\r\n01548 0x00000001 "res\\bldg\\castle.bsr"\r\n')
		vsro = alex.object_catalog(b'JMXVOBJI1000\r\n1\r\n01548 0x00000000 "res\\bldg\\castle.bsr"\r\n')
		report = alex.compare_catalogs(native, vsro, alex.object_placeholder)
		self.assertEqual(report["conflicts"], [1548])


class PinTests(unittest.TestCase):
	def setUp(self):
		alex._verified_archives.clear()
		self.root = tempfile.TemporaryDirectory()
		self.addCleanup(self.root.cleanup)
		self.vsro = Path(self.root.name)

	def pin(self, name, payload):
		(self.vsro / f"{name}.pk2").write_bytes(payload)
		return {name: hashlib.sha256(payload).hexdigest()}

	def test_a_matching_archive_verifies(self):
		pins = self.pin("Media", b"the pinned client")
		alex.verify_vsro_archive(self.vsro, "Media", pins)

	def test_a_changed_archive_is_refused(self):
		pins = self.pin("Media", b"the pinned client")
		(self.vsro / "Media.pk2").write_bytes(b"the pinned client, patched")
		with self.assertRaises(SystemExit):
			alex.verify_vsro_archive(self.vsro, "Media", pins)

	def test_a_missing_archive_is_refused(self):
		with self.assertRaises(SystemExit):
			alex.verify_vsro_archive(self.vsro, "Media", {"Media": "0" * 64})

	def test_an_unpinned_archive_is_refused(self):
		self.pin("Music", b"unpinned")
		with self.assertRaises(SystemExit):
			alex.verify_vsro_archive(self.vsro, "Music", {})

	def test_require_checks_every_named_archive(self):
		pins = self.pin("Map", b"map") | self.pin("Media", b"media")
		alex.require_vsro_pin(self.vsro, ("Map", "Media"), pins)
		(self.vsro / "Media.pk2").write_bytes(b"other media")
		alex._verified_archives.clear()
		with self.assertRaises(SystemExit):
			alex.require_vsro_pin(self.vsro, ("Map", "Media"), pins)

	def test_the_checked_in_pin_names_every_archive_the_chain_reads(self):
		pins = alex.archive_pins()
		self.assertEqual(set(pins), set(alex.ARCHIVE_NAMES))
		for digest in pins.values():
			self.assertEqual(len(digest), 64)
			self.assertTrue(all(c in "0123456789abcdef" for c in digest), digest)


if __name__ == "__main__":
	unittest.main()
