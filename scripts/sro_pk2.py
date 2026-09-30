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

Requires pycryptodome (requirements.txt).

===========================================================================
"""
import hashlib
import struct

from Crypto.Cipher import Blowfish

SIGNATURE = b"JoyMax File Manager!\n"
# The directory key as the v1.150 client derives it.
DIRECTORY_KEY = bytes.fromhex("32cedd7cbca8")
ROOT_BLOCK = 256
BLOCK_BYTES = 2560
ENTRY_BYTES = 128
ENTRIES_PER_BLOCK = 20
NAME_BYTES = 81
ENTRY_EMPTY, ENTRY_FOLDER, ENTRY_FILE = 0, 1, 2


def fold_ascii(name):
	"""Lowercase ASCII letters only, leaving every other character."""
	return "".join(chr(ord(c) + 32) if "A" <= c <= "Z" else c for c in name)


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
	cipher = Blowfish.new(DIRECTORY_KEY, Blowfish.MODE_ECB)
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
				start, size, chain = struct.unpack_from("<QIQ", row, 106)
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
