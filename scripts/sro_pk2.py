"""
===========================================================================

sro_pk2.py - read a v1.150 Joymax PK2 archive

The PK2 directory is a chain of 2560-byte blocks of twenty 128-byte entries,
Blowfish-encrypted (ECB, with each 32-bit word byte-swapped around the
cipher). File payloads are stored plainly. This module walks the complete
directory with range, cycle and duplicate guards and reads payloads; it never
writes into an archive.

Names are CP949 bytes. Only ASCII letters are case-folded, the convention of
every JMX reader here: Python's str.lower() on the raw bytes would also fold
0xC0-0xDE lead bytes and corrupt Korean names.

The directory key is not in this repository: it is read from the
operator's own client data, <source-data>/pk2-directory.key (hex), the
same key their installed client uses to read its archives.

Requires pycryptodome (requirements.txt).

===========================================================================
"""
import hashlib
import struct

from Crypto.Cipher import Blowfish

SIGNATURE = b"JoyMax File Manager!\n"
DIRECTORY_KEY_FILE = "pk2-directory.key"
_directory_key = None
ROOT_BLOCK = 256
BLOCK_BYTES = 2560
ENTRY_BYTES = 128
ENTRIES_PER_BLOCK = 20
NAME_BYTES = 81
# Entry fields after the name: <QIQ start, size, next-block chain. Only the
# last entry's chain links the folder's next directory block.
ENTRY_EXTENT_OFFSET = 106
ENTRY_CHAIN_OFFSET = ENTRY_EXTENT_OFFSET + 8 + 4
ENTRY_EMPTY, ENTRY_FOLDER, ENTRY_FILE = 0, 1, 2


def fold_ascii(name):
	"""Lowercase ASCII letters only, leaving every other character."""
	return "".join(chr(ord(c) + 32) if "A" <= c <= "Z" else c for c in name)


def set_directory_key(key):
	"""Use key (bytes) for the directory cipher; tests inject their own."""
	global _directory_key
	_directory_key = bytes(key)


def directory_key():
	"""
	The directory key from the operator's source-data, read once. Raises
	FileNotFoundError naming the file when it is missing.
	"""
	global _directory_key
	if _directory_key is None:
		import sro_paths
		path = sro_paths.SOURCE_DATA_ROOT / DIRECTORY_KEY_FILE
		if not path.is_file():
			raise FileNotFoundError(f"PK2 directory key not found: put your client's key (hex) in {path}")
		_directory_key = bytes.fromhex(path.read_text(encoding="ascii").strip())
	return _directory_key


def directory_cipher():
	"""
	The PK2 directory-block cipher. This is the retail archive format, not a
	security choice: the v1.150 client obfuscates directory blocks with
	Blowfish in ECB mode under the key it carries, and reading (or,
	in tests, writing) the user's own archive must match it byte for byte.
	"""
	return Blowfish.new(directory_key(), Blowfish.MODE_ECB)


def _swap_words(value):
	return b"".join(value[i:i + 4][::-1] for i in range(0, len(value), 4))


class Entry:
	"""One file: its archive path (CP949-decoded, original case) and extent."""

	__slots__ = ("path", "offset", "size")

	def __init__(self, path, offset, size):
		self.path = path
		self.offset = offset
		self.size = size


def read_directory(data, blocks=None):
	"""
	Every file entry of an archive, in directory order. data is bytes or an
	mmap of the archive. When blocks is a list, each directory block read is
	appended to it as {"offset", "sha256"} of its stored (encrypted) bytes,
	the archive evidence scripts/analysis/verify_particle_archive.py records.
	Raises ValueError on a malformed, cyclic or out-of-range directory, or
	when two entries fold to the same path.
	"""
	if data[:len(SIGNATURE)] != SIGNATURE:
		raise ValueError("Invalid PK2 signature")
	cipher = directory_cipher()
	seen, folded, files = set(), set(), []

	def walk(offset, prefix):
		while offset:
			if offset in seen or not ROOT_BLOCK <= offset <= len(data) - BLOCK_BYTES:
				raise ValueError(f"Invalid or cyclic directory block {offset}")
			seen.add(offset)
			raw = data[offset:offset + BLOCK_BYTES]
			if blocks is not None:
				blocks.append({"offset": offset, "sha256": digest(raw)})
			block = _swap_words(cipher.decrypt(_swap_words(raw)))
			following = 0
			for index in range(ENTRIES_PER_BLOCK):
				row = block[index * ENTRY_BYTES:(index + 1) * ENTRY_BYTES]
				kind = row[0]
				raw_name = row[1:1 + NAME_BYTES].split(b"\0")[0]
				start, size, chain = struct.unpack_from("<QIQ", row, ENTRY_EXTENT_OFFSET)
				if kind not in (ENTRY_EMPTY, ENTRY_FOLDER, ENTRY_FILE):
					raise ValueError(f"Invalid entry type {kind}")
				if index == ENTRIES_PER_BLOCK - 1:
					following = chain
				if kind == ENTRY_EMPTY or raw_name in (b".", b".."):
					continue
				name = raw_name.decode("cp949")
				if not name or "/" in name or "\\" in name or name in (".", ".."):
					raise ValueError(f"Invalid archive filename {raw_name!r}")
				path = prefix + name
				if kind == ENTRY_FOLDER:
					walk(start, path + "/")
					continue
				key = fold_ascii(path)
				if start + size > len(data) or key in folded:
					raise ValueError(f"Invalid or duplicate archive file {path}")
				folded.add(key)
				files.append(Entry(path, start, size))
			offset = following

	walk(ROOT_BLOCK, "")
	return files


def payload(data, entry):
	"""The stored bytes of one file entry."""
	return data[entry.offset:entry.offset + entry.size]


def digest(value):
	return hashlib.sha256(value).hexdigest()
