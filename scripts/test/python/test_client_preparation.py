"""
===========================================================================

test_client_preparation.py - the PK2 reader and the client preparation tool

Builds small synthetic PK2 archives with the client's directory cipher, so
the reader (scripts/sro_pk2.py) and the extractor
(scripts/prepare_client_resources.py) are tested without game data:
nested folders, CP949 names with ASCII-only case folding, directory guards,
path containment, and repair of a damaged extraction. The doctor runs as a
process against scratch game roots.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import os
import struct
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import prepare_client_resources as prepare  # noqa: E402
import sro_pk2  # noqa: E402

HEADER_BYTES = 256


def _swap(value):
	return b"".join(value[i:i + 4][::-1] for i in range(0, len(value), 4))


class ArchiveBuilder:
	"""A PK2 writer for tests: one directory block per folder."""

	def __init__(self):
		self.blocks = {}
		self.payloads = bytearray()
		self.next_block = HEADER_BYTES

	def folder(self, entries):
		"""
		entries: (kind, raw name, start, size) rows; returns the block offset.
		A chain of 0 ends the folder.
		"""
		offset = self.next_block
		self.next_block += sro_pk2.BLOCK_BYTES
		self.blocks[offset] = entries
		return offset

	def build(self, chains=None):
		chains = chains or {}
		payload_base = self.next_block
		data = bytearray(sro_pk2.SIGNATURE.ljust(HEADER_BYTES, b"\0"))
		cipher = sro_pk2.directory_cipher()
		for offset in sorted(self.blocks):
			block = bytearray(sro_pk2.BLOCK_BYTES)
			for index, (kind, name, start, size) in enumerate(self.blocks[offset]):
				row = bytearray(sro_pk2.ENTRY_BYTES)
				row[0] = kind
				row[1:1 + len(name)] = name
				resolved = start if kind == sro_pk2.ENTRY_FOLDER else payload_base + start
				struct.pack_into("<QIQ", row, sro_pk2.ENTRY_EXTENT_OFFSET, resolved, size, 0)
				block[index * sro_pk2.ENTRY_BYTES:(index + 1) * sro_pk2.ENTRY_BYTES] = row
			if offset in chains:
				last = (sro_pk2.ENTRIES_PER_BLOCK - 1) * sro_pk2.ENTRY_BYTES
				struct.pack_into("<Q", block, last + sro_pk2.ENTRY_CHAIN_OFFSET, chains[offset])
			data += _swap(cipher.encrypt(_swap(bytes(block))))
		data += self.payloads
		return bytes(data)

	def add_payload(self, value):
		start = len(self.payloads)
		self.payloads += value
		return start, len(value)


def sample_archive():
	"""root: Readme.TXT, 텍스쳐/검.ddj (CP949), nested/deep.bin."""
	builder = ArchiveBuilder()
	root = builder.folder([])
	korean = builder.folder([])
	nested = builder.folder([])
	readme = builder.add_payload(b"hello")
	sword = builder.add_payload(b"\x01\x02\x03")
	deep = builder.add_payload(b"deep bytes")
	builder.blocks[root] = [
		(sro_pk2.ENTRY_FILE, b"Readme.TXT", *readme),
		(sro_pk2.ENTRY_FOLDER, "텍스쳐".encode("cp949"), korean, 0),
		(sro_pk2.ENTRY_FOLDER, b"nested", nested, 0),
	]
	builder.blocks[korean] = [
		(sro_pk2.ENTRY_FOLDER, b".", korean, 0),
		(sro_pk2.ENTRY_FILE, "검.ddj".encode("cp949"), *sword),
	]
	builder.blocks[nested] = [(sro_pk2.ENTRY_FILE, b"deep.bin", *deep)]
	return builder.build()


class ReaderTests(unittest.TestCase):
	def test_walks_nested_folders_with_original_names(self):
		data = sample_archive()
		entries = {e.path: sro_pk2.payload(data, e) for e in sro_pk2.read_directory(data)}
		self.assertEqual(entries, {
			"Readme.TXT": b"hello",
			"텍스쳐/검.ddj": b"\x01\x02\x03",
			"nested/deep.bin": b"deep bytes",
		})

	def test_folds_ascii_only(self):
		# str.lower() would also fold non-ASCII letters: on Latin-1-decoded
		# CP949 bytes that corrupts Korean lead bytes (0xC0-0xDE).
		self.assertEqual(sro_pk2.fold_ascii("Dunhuang/돈황.OGG"), "dunhuang/돈황.ogg")
		self.assertEqual(sro_pk2.fold_ascii("\xc5\xc0"), "\xc5\xc0")

	def test_refuses_two_names_that_fold_together(self):
		builder = ArchiveBuilder()
		root = builder.folder([])
		first, second = builder.add_payload(b"a"), builder.add_payload(b"b")
		builder.blocks[root] = [(sro_pk2.ENTRY_FILE, b"Same.txt", *first), (sro_pk2.ENTRY_FILE, b"same.TXT", *second)]
		with self.assertRaisesRegex(ValueError, "duplicate"):
			sro_pk2.read_directory(builder.build())

	def test_refuses_a_cyclic_chain(self):
		builder = ArchiveBuilder()
		root = builder.folder([(sro_pk2.ENTRY_FILE, b"x", 0, 0)])
		with self.assertRaisesRegex(ValueError, "cyclic"):
			sro_pk2.read_directory(builder.build(chains={root: root}))

	def test_refuses_another_signature(self):
		with self.assertRaisesRegex(ValueError, "signature"):
			sro_pk2.read_directory(b"not an archive".ljust(4096, b"\0"))


class ExtractionTests(unittest.TestCase):
	def test_extracts_then_repairs_only_damaged_files(self):
		with tempfile.TemporaryDirectory() as scratch:
			archive = Path(scratch) / "Sample.pk2"
			archive.write_bytes(sample_archive())
			output = Path(scratch) / "Sample_extracted"
			files, written, _ = prepare.extract_archive(archive, output)
			self.assertEqual((files, written), (3, 3))
			self.assertEqual((output / "텍스쳐" / "검.ddj").read_bytes(), b"\x01\x02\x03")
			self.assertEqual(prepare.extract_archive(archive, output)[1], 0)
			(output / "Readme.TXT").write_bytes(b"damaged")
			(output / "nested" / "deep.bin").unlink()
			self.assertEqual(prepare.extract_archive(archive, output)[1], 2)
			self.assertEqual((output / "Readme.TXT").read_bytes(), b"hello")

	def test_refuses_names_that_leave_the_output_folder(self):
		with tempfile.TemporaryDirectory() as scratch:
			root = Path(scratch)
			for name in ("../escape.txt", "a/../../b", "stream:alternate", "c:/x"):
				with self.assertRaises(ValueError, msg=name):
					prepare.safe_target(root, name)
			self.assertEqual(prepare.safe_target(root, "a/b.txt"), root / "a" / "b.txt")


REPO_ROOT = Path(__file__).resolve().parents[3]
EXTRACTED_FOLDERS = ("Media_extracted", "Data_extracted", "Map_extracted", "Particles_extracted", "Music_mp3")


def run_doctor(game_root):
	env = dict(os.environ, SRO_GAME_ROOT=str(game_root))
	return subprocess.run(["node", "scripts/assets_doctor.mjs"], cwd=REPO_ROOT, env=env, capture_output=True, text=True)


class DoctorTests(unittest.TestCase):
	"""The doctor and the builds' preflight (scripts/build/shared/clientInputs.mjs)."""

	def test_an_empty_root_lists_every_missing_input(self):
		with tempfile.TemporaryDirectory() as root:
			result = run_doctor(root)
			self.assertEqual(result.returncode, 1)
			for name in ("SRO_Client.exe", "Particles.pk2", *EXTRACTED_FOLDERS):
				self.assertIn(name, result.stdout)

	def test_a_complete_root_passes_the_build_inputs(self):
		with tempfile.TemporaryDirectory() as root:
			for name in ("SRO_Client.exe", "Particles.pk2"):
				(Path(root) / name).write_bytes(b"x")
			for folder in EXTRACTED_FOLDERS:
				(Path(root) / "extracted" / folder).mkdir(parents=True)
				(Path(root) / "extracted" / folder / "file").write_bytes(b"x")
			result = run_doctor(root)
			self.assertRegex(result.stdout, r"ok\s+build inputs")
			self.assertNotIn("build input  ", result.stdout.replace("build inputs", ""))

	def test_an_empty_folder_counts_as_missing(self):
		with tempfile.TemporaryDirectory() as root:
			(Path(root) / "extracted" / "Music_mp3").mkdir(parents=True)
			self.assertIn("extracted/Music_mp3 is missing or empty", run_doctor(root).stdout)


if __name__ == "__main__":
	unittest.main()
