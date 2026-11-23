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
# The catalog allocations: syscolpars (columns) and sysrscols (where a
# row keeps each column).
SYSCOLPARS = 41
SYSRSCOLS = 3
# A record's status bit A: the row carries variable-length fields.
HAS_VARIABLE = 0x20
# sys.types ids the typed reader decodes: fixed (format, width) and text
# (codecs, in the order tried). The shard's varchar columns are declared
# SQL_Latin1_General_CP1 (code page 1252) but hold the Korean code page,
# as the game's tools wrote them: cp949 first, and the declared page for
# the few Western bytes (an apostrophe 0x92) that are not cp949.
FIXED_TYPES = {48: ("<B", 1), 52: ("<h", 2), 56: ("<i", 4), 59: ("<f", 4), 62: ("<d", 8), 127: ("<q", 8)}
TEXT_TYPES = {167: ("cp949", "cp1252"), 231: ("utf-16le",)}


# ================
# decode_text
# ================
def decode_text(raw, codecs):
	for codec in codecs[:-1]:
		try:
			return raw.decode(codec)
		except UnicodeDecodeError:
			continue
	return raw.decode(codecs[-1])


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
	def __init__(self, path, extra_tables=()):
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
				if name.startswith(("_Ref", "Tab_Ref")) or name in extra_tables:
					objects[struct.unpack_from("<I", page, at + 4)[0]] = name
		rowsets = {}
		for _, at, page in self.rows(5):
			if struct.unpack_from("<H", page, at + 2)[0] != 57:
				continue
			identity = struct.unpack_from("<I", page, at + 13)[0]
			if identity in objects and struct.unpack_from("<I", page, at + 17)[0] <= 1:
				rowsets[struct.unpack_from("<Q", page, at + 4)[0]] = objects[identity]
		self.identities = {name: identity for identity, name in objects.items()}
		self.rowsets = {name: rowset for rowset, name in rowsets.items()}
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
	# columns
	#
	# The table's columns in column order, from the backup's own catalog:
	# syscolpars (allocation 41) gives each name, type and length, and
	# sysrscols (allocation 3) where its row keeps it - a fixed column's
	# byte offset, or for a variable one its index among the variable
	# fields (stored as -1, -2, ...), and its null-bitmap bit. The fixed
	# columns are not in column order (the clustered key leads), so a
	# reader that assumes they are reads the wrong bytes.
	# ================
	def columns(self, name):
		if name not in self.identities or name not in self.rowsets:
			raise ValueError("Missing reference table " + name)
		identity, rowset = self.identities[name], self.rowsets[name]
		catalog = {}
		for _, at, page in self.rows(SYSCOLPARS):
			if struct.unpack_from("<I", page, at + 4)[0] != identity:
				continue
			fields = variable_fields(page, at)
			column = struct.unpack_from("<I", page, at + 10)[0]
			catalog[column] = (fields[0].decode("utf-16le"), page[at + 14], struct.unpack_from("<h", page, at + 19)[0])
		places = {}
		for _, at, page in self.rows(SYSRSCOLS):
			if struct.unpack_from("<Q", page, at + 4)[0] != rowset:
				continue
			column = struct.unpack_from("<I", page, at + 12)[0]
			places[column] = (struct.unpack_from("<h", page, at + 44)[0], struct.unpack_from("<h", page, at + 48)[0])
		if not catalog or sorted(catalog) != sorted(places):
			raise ValueError("Incomplete catalog for " + name)
		columns = []
		for column in sorted(catalog):
			label, kind, length = catalog[column]
			offset, nullbit = places[column]
			if kind not in FIXED_TYPES and kind not in TEXT_TYPES:
				raise ValueError(f"Unsupported column type {kind} in {name}.{label}")
			if (kind in FIXED_TYPES) != (offset > 0):
				raise ValueError(f"Misplaced column {name}.{label}")
			columns.append((label, kind, offset, nullbit))
		return columns

	# ================
	# table_rows
	#
	# Every row of the table as {column: value}, decoded by its catalog:
	# integers and floats from the fixed part, text from the variable
	# fields (a trailing variable column the row omits is empty), None
	# where the null bitmap says NULL.
	# ================
	def table_rows(self, name):
		columns = self.columns(name)
		end_of_fixed = 4 + sum(FIXED_TYPES[kind][1] for _, kind, offset, _ in columns if offset > 0)
		for _, at, page in self.named_rows(name):
			if struct.unpack_from("<H", page, at + 2)[0] != end_of_fixed:
				raise ValueError(f"Unsupported {name} row layout")
			count = struct.unpack_from("<H", page, at + end_of_fixed)[0]
			bitmap = page[at + end_of_fixed + 2:at + end_of_fixed + 2 + (count + 7) // 8]
			texts = variable_fields(page, at) if page[at] & HAS_VARIABLE else []
			row = {}
			for label, kind, offset, nullbit in columns:
				bit = nullbit - 1
				if 0 <= bit < count and bitmap[bit // 8] >> (bit % 8) & 1:
					row[label] = None
				elif offset > 0:
					row[label] = struct.unpack_from(FIXED_TYPES[kind][0], page, at + offset)[0]
				else:
					index = -offset - 1
					raw = bytes(texts[index]) if index < len(texts) else b""
					row[label] = decode_text(raw, TEXT_TYPES[kind])
			yield row

	# ================
	# close
	# ================
	def close(self):
		self.raw.close()
		self.file.close()
