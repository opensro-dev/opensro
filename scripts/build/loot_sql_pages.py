"""
===========================================================================
loot_sql_pages.py - read-only SQL backup reference-table extraction

Owns page/row decoding for the loot snapshot importer. This is not a SQL
restore: allocation metadata, primary rows, and conflicting identities are
checked explicitly before content can enter the normalized catalog.
===========================================================================
"""

import collections
import mmap
import struct

PAGE_SIZE = 8192
SECTOR_SIZE = 512
PAGE_HEADER_SIZE = 96


# ================
# decode_page
# ================
def decode_page(raw):
	if len(raw) != PAGE_SIZE:
		raise ValueError("Truncated SQL page")
	flags = struct.unpack_from("<H", raw, 4)[0]
	if not flags & 0x100:
		return raw
	if flags & 0x200:
		raise ValueError("Conflicting SQL page protection flags")
	saved = struct.unpack_from("<I", raw, 60)[0]
	signature = saved & 3
	if signature not in (1, 2):
		raise ValueError("Invalid torn-page signature")
	page = bytearray(raw)
	for sector in range(1, PAGE_SIZE // SECTOR_SIZE):
		end = (sector + 1) * SECTOR_SIZE - 1
		if page[end] & 3 != signature:
			raise ValueError("Torn SQL page")
		page[end] = (page[end] & ~3) | ((saved >> (2 * sector)) & 3)
	return bytes(page)


# ================
# records
# ================
def records(page):
	count = struct.unpack_from("<H", page, 22)[0]
	if not 0 < count < 2000:
		return
	for index in range(count):
		at = struct.unpack_from("<H", page, PAGE_SIZE - 2 - 2 * index)[0]
		if PAGE_HEADER_SIZE <= at < PAGE_SIZE - 2 - 2 * count and (page[at] >> 1) & 7 == 0:
			yield at


# ================
# variable_fields
# ================
def variable_fields(page, at):
	end = struct.unpack_from("<H", page, at + 2)[0]
	if not 4 <= end < 8000 or at + end + 2 >= PAGE_SIZE - 2:
		return []
	columns = struct.unpack_from("<H", page, at + end)[0]
	current = at + end + 2 + (columns + 7) // 8
	if not 1 <= columns <= 300 or current + 2 >= PAGE_SIZE - 2:
		return []
	count = struct.unpack_from("<H", page, current)[0]
	current += 2
	if not 1 <= count <= 64 or current + count * 2 >= PAGE_SIZE - 2:
		return []
	start = current + count * 2
	values = []
	for index in range(count):
		stop = at + (struct.unpack_from("<H", page, current + 2 * index)[0] & 32767)
		if not start <= stop <= PAGE_SIZE:
			return []
		values.append(page[start:stop])
		start = stop
	return values


# ================
# Backup
# ================
class Backup:
	# ================
	# __init__
	# ================
	def __init__(self, path):
		self.file = path.open("rb")
		self.raw = mmap.mmap(self.file.fileno(), 0, access=mmap.ACCESS_READ)
		self.pages = collections.defaultdict(list)
		for offset in range(0, len(self.raw) - PAGE_SIZE + 1, SECTOR_SIZE):
			if self.raw[offset:offset + 2] == b"\x01\x01":
				allocation = struct.unpack_from("<I", self.raw, offset + 24)[0]
				self.pages[allocation].append(offset)
		objects = {}
		for _, at, page in self.rows(34):
			fields = variable_fields(page, at)
			if fields:
				name = fields[0].decode("utf-16le")
				if name.startswith(("_Ref", "Tab_Ref")):
					objects[struct.unpack_from("<I", page, at + 4)[0]] = name
		rowsets = {}
		for _, at, page in self.rows(5):
			if struct.unpack_from("<H", page, at + 2)[0] != 57:
				continue
			identity = struct.unpack_from("<I", page, at + 13)[0]
			if identity in objects and struct.unpack_from("<I", page, at + 17)[0] <= 1:
				rowsets[struct.unpack_from("<Q", page, at + 4)[0]] = objects[identity]
		self.tables = collections.defaultdict(list)
		for _, at, page in self.rows(7):
			if struct.unpack_from("<H", page, at + 2)[0] != 73 or page[at + 12] != 1:
				continue
			owner = struct.unpack_from("<Q", page, at + 13)[0]
			if owner in rowsets:
				allocation = (struct.unpack_from("<Q", page, at + 4)[0] >> 16) & 0xffffffff
				self.tables[rowsets[owner]].append(allocation)

	# ================
	# rows
	# ================
	def rows(self, allocation):
		for offset in self.pages[allocation]:
			page = decode_page(self.raw[offset:offset + PAGE_SIZE])
			for at in records(page):
				yield offset, at, page

	# ================
	# named_rows
	# ================
	def named_rows(self, name):
		if name not in self.tables:
			raise ValueError("Missing reference table " + name)
		for allocation in self.tables[name]:
			yield from self.rows(allocation)

	# ================
	# close
	# ================
	def close(self):
		self.raw.close()
		self.file.close()
