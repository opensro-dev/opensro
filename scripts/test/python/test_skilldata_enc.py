"""
===========================================================================

test_skilldata_enc.py - the later clients' encrypted skill tables

A known-answer vector from the pinned VSRO client (the first 16 bytes of
SkillData_5000ENC.txt and the plain text they decode to), a round trip
through the inverse, plain pass-through, and the manifest the client
reads: the encrypted one when the archive has it.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)

import sys
import unittest
from pathlib import Path

import skilldata_enc as enc  # noqa: E402

# The pinned VSRO Media.pk2's SkillData_5000ENC.txt opens with these bytes;
# they decode to the BOM and "1\t1\t172" of SKILL_PUNCH_01's row.
VSRO_HEAD = bytes.fromhex("e2b0223022304684a0aafd0ddc548020")
VSRO_HEAD_PLAIN = bytes.fromhex("fffe3100090031000900310037003200")


# ================
# encode
#
# The inverse the tests need: subtract the same key stream.
# ================
def encode(plain):
	out = bytearray(len(plain))
	key = enc.KEY_SEED
	for i, value in enumerate(plain):
		out[i] = (value - enc.KEY_TABLE_A[key % len(enc.KEY_TABLE_A)] + enc.KEY_TABLE_B[key % len(enc.KEY_TABLE_B)]) & 0xFF
		key += 1
	return bytes(out)


def utf16(rows):
	return ("﻿" + "\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


class Decode(unittest.TestCase):
	def test_the_pinned_client_vector_decodes(self):
		self.assertEqual(enc.decode(VSRO_HEAD), VSRO_HEAD_PLAIN)

	def test_a_table_round_trips(self):
		plain = utf16([["1", "31659", "0", "MSKILL_SD_HIKE_ATTACK03"]])
		encrypted = encode(plain)
		self.assertTrue(encrypted.startswith(enc.ENCRYPTED_MAGIC))
		self.assertEqual(enc.decode(encrypted), plain)

	def test_a_plain_table_passes_through(self):
		plain = utf16([["1", "1", "0", "SKILL_PUNCH_01"]])
		self.assertEqual(enc.decode(plain), plain)


class ClientSkillRows(unittest.TestCase):
	def archive(self, files):
		return (lambda name: files[name.lower()]), (lambda name: name.lower() in files)

	def test_the_encrypted_manifest_wins(self):
		read, has = self.archive({
			"skilldataenc.txt": utf16([["SkillData_5000ENC.txt"]]),
			"skilldata_5000enc.txt": encode(utf16([["1", "31659", "0", "MSKILL_NEW"]])),
			"skilldata.txt": utf16([["SkillData_5000.txt"]]),
			"skilldata_5000.txt": utf16([["1", "1", "0", "SKILL_OLD"]]),
		})
		self.assertEqual([row[3] for row in enc.client_skill_rows(read, has)], ["MSKILL_NEW"])

	def test_without_one_the_plain_manifest_is_read(self):
		read, has = self.archive({
			"skilldata.txt": utf16([["SkillData_5000.txt"]]),
			"skilldata_5000.txt": utf16([["1", "1", "0", "SKILL_OLD"]]),
		})
		self.assertEqual([row[3] for row in enc.client_skill_rows(read, has)], ["SKILL_OLD"])


if __name__ == "__main__":
	unittest.main()
