"""
===========================================================================

skilldata_enc.py - the later clients' encrypted skill tables

The VSRO client reads its skills from skilldataenc.txt and the
SkillData_<n>ENC.txt shards it names. The plain skilldata.txt shards
that ship beside them are an older copy: in the pinned VSRO Media.pk2
they hold 29,334 rows to the encrypted set's 31,400, and they lack the
Egypt field monsters' own skills (MSKILL_SD_HIKE_ATTACK03, id 31659, and
112 more), so a client-only import that read them refused the field.

The cipher is a byte stream: a counter starting at KEY_SEED picks one
byte from each of two tables (modulo their lengths), and their difference
is added to each file byte, modulo 256. It is the community-documented
SkillDataEnc scheme (xBot's SkillData.Decrypt has the same tables);
checked here against the pinned client itself: every shard decodes to a
UTF-16 table (BOM ff fe) of 118-column rows, and 29,234 of the plain
copy's 29,334 rows decode byte-identical.

A file that does not start with ENCRYPTED_MAGIC is returned unchanged,
so a plain table passes through.

===========================================================================
"""

import re

import import_vsro_alexandria as tables

ENC_MANIFEST = "skilldataenc.txt"
PLAIN_MANIFEST = "skilldata.txt"
# A manifest names shards by basename only (SkillData_5000ENC.txt,
# skilldata_5000.txt); anything else would be a path into the archive.
SHARD_NAME = re.compile(r"skilldata_[0-9]+(enc)?\.txt", re.IGNORECASE)
ENCRYPTED_MAGIC = bytes.fromhex("e2b0")
# Each encrypted shard ends with this marker in plain UTF-16, outside the
# cipher stream: checked on all seven shards of the pinned VSRO client.
# Decoded with the rest it became a one-column row of noise; kept plain it
# is a comment line the table reader drops.
ENCRYPTED_TRAILER = "\r\n//encrypt".encode("utf-16-le")
KEY_SEED = 0x8C1F
# The two key tables, 167 and 495 bytes: the cipher indexes them
# by the counter modulo their lengths.
KEY_TABLE_A = bytes.fromhex("""
	07 83 bc ee 4b 79 19 b6 2a 53 4f 3a cf 71 e5 3c
	2d 18 14 cb b6 bc aa 9a 31 42 3a 13 42 c9 63 fc
	54 1d f2 c1 8a dd 1c b3 52 ea 9b d7 c4 ba f8 12
	74 92 30 c9 d6 56 15 52 53 60 11 33 c5 9d 30 9a
	e5 d2 93 99 eb cf aa 79 e3 78 6a b9 02 e0 ce 8e
	f3 63 5a 73 74 f3 72 aa 2c 9f bb 33 91 de 5f 91
	66 48 d1 7a fd 3f 91 3e 5d 22 ec ef 7c a5 43 c0
	1d 4f 60 7f 0b 4a 4b 2a 43 06 46 14 45 d0 c5 83
	92 e4 16 d0 a3 a1 13 da d1 51 07 eb 7d ce a5 db
	78 e0 c1 0b e5 8e 1c 7c b4 df ed b8 53 ba 2c b5
	bb 56 fb 68 95 6e 65
""")
KEY_TABLE_B = bytes.fromhex("""
	0d 05 90 41 f9 d0 65 bf f9 0b 15 93 80 fb 01 02
	b6 08 c4 3c c1 49 94 4d ce 1d fd 69 ea 19 c9 57
	9c 4d 84 62 e3 67 f9 87 f4 f9 93 da e5 15 f1 4c
	a4 ec bc cf dd b3 6f 04 3d 70 1c 74 21 6b 00 71
	31 7f 54 b3 72 6c aa 42 c1 78 61 3e d5 f2 e1 27
	36 71 3a 25 36 57 d1 f8 70 86 bd 0e 58 b3 76 6d
	c3 50 f6 6c a0 10 06 64 a2 d6 2c d4 27 30 a5 36
	1c 1e 3e 58 9d 59 76 9d a7 42 5a f0 00 bc 69 31
	40 1e fa 09 1d e7 ee e4 54 89 36 7c 67 c8 65 22
	7e a3 60 44 1e bc 68 6f 15 2a fd 9d 3f 36 6b 28
	06 67 fe c6 49 6b 9b 3f 80 2a d2 d4 d3 20 1b 96
	f4 d2 ca 8c 74 ee 0b 6a e1 e9 c6 d2 6e 33 63 c0
	e9 d0 37 a9 3c f7 18 f2 4a 74 ec 41 61 7a 19 47
	8f a0 bb 94 8f 3d 11 11 26 cf 69 18 1b 2c 87 6d
	b3 22 6c 78 41 cc c2 84 c5 cb 01 6a 37 00 01 65
	4f a7 85 85 15 59 05 67 f2 4f ab b7 88 fa 69 24
	9e c6 7b 3f d5 0e 4d 7b fb b1 21 3c b0 c0 cb 2c
	aa 26 8d cc dd da c1 f8 ca 7f 6a 3f 2a 61 e7 60
	5c ce d3 4c ac 45 40 62 ea 51 f1 66 5d 2c 45 d6
	8b 7d ce 9c f5 bb f7 52 24 1a 13 02 2b 00 bb a1
	8f 6e 7a 33 ad 5f f4 4a 82 76 ab de 80 98 8b 26
	4f 33 d8 68 1e d9 ae 06 6b 7e a9 95 67 60 eb e8
	d0 7d 07 4b f1 aa 9a c5 29 93 9d 5c 92 3f 15 de
	48 f1 ca ea c9 78 3c 28 7e b0 46 d3 71 6c d7 bd
	2c f7 25 2f c7 dd b4 6d 35 bb a7 da 3e 3d a7 ca
	bd 87 dd 9f 22 3d 50 d2 30 d5 14 5b 8f f4 af aa
	a0 fc 17 3d 33 10 99 dc 76 a9 40 1b 64 14 df 35
	68 66 5b 49 05 33 68 26 c8 ba d1 8d 39 2b fb 3e
	24 52 2f 9a 69 bc f2 b2 ac b8 ef a1 17 29 2d ee
	f5 23 21 ec 81 c7 5b c0 82 cc d2 91 9d 29 93 0c
	9d 5d 57 ad d4 c6 40 93 8d e9 d3 35 9d c6 d3
""")


# ================
# decode
#
# One SkillData_<n>ENC.txt payload in plain UTF-16. A payload without the
# encrypted magic is already plain and comes back as it is. The plain
# ENCRYPTED_TRAILER, when present, is passed through undecoded.
# ================
def decode(raw):
	raw = bytes(raw)
	if not raw.startswith(ENCRYPTED_MAGIC):
		return raw
	trailer = ENCRYPTED_TRAILER if raw.endswith(ENCRYPTED_TRAILER) else b""
	body = raw[:len(raw) - len(trailer)]
	out = bytearray(len(body))
	key = KEY_SEED
	for i, value in enumerate(body):
		out[i] = (value + KEY_TABLE_A[key % len(KEY_TABLE_A)] - KEY_TABLE_B[key % len(KEY_TABLE_B)]) & 0xFF
		key += 1
	return bytes(out) + trailer


# ================
# client_skill_rows
#
# Every skill row a later client carries, read the way the client reads
# them: the encrypted manifest and its shards when the archive has one,
# else the plain manifest. read(name) returns a textdata payload by
# lower-case name. has(name) says whether the archive holds it. Manifest
# entries are validated basenames, never arbitrary paths.
# ================
def client_skill_rows(read, has):
	manifest = ENC_MANIFEST if has(ENC_MANIFEST) else PLAIN_MANIFEST
	names = [row[0].strip() for row in tables.table_rows(read(manifest)) if row and row[0].strip()]
	if not names or any(not SHARD_NAME.fullmatch(name) for name in names):
		raise ValueError(f"invalid skill manifest {manifest}")
	shards = [name.lower() for name in names]
	if len(set(shards)) != len(shards):
		raise ValueError(f"duplicate entry in skill manifest {manifest}")
	return [row for shard in shards for row in tables.table_rows(decode(read(shard)))]
