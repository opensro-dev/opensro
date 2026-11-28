"""
===========================================================================

later_tables.py - the one reader of a later client's textdata tables

The later clients split their big tables into shards behind an index of
the old name: v1.696 splits 23 (characterdata, itemdata, skilldata,
questdata, textdata_object, Skillaniset, Skilleffectset, characterinfo
...), VSRO splits textdataname, textquest and skilldataenc, and the v1.150
tree splits characterdata, itemdata and skilldata. An index read as a
table is one-cell rows of file names, which the importers' width filters
then drop without a word: the shape of #4730 (Karkadann's missing
presentation, the fortresses' lost VSRO names).

So no importer chooses whether to follow an index. Archive is the only
way to read one of these tables: rows, rows_of and text always follow
the index, and file - the one-file read, also what calling an Archive
does, for helpers that take a read callable - refuses a file that is an
index. The payload reader beneath is private here, and the
check:later-table-reads gate refuses a textdata payload read anywhere
else in scripts/ (#4761).

===========================================================================
"""
import mmap
from contextlib import contextmanager
from pathlib import Path

import sro_pk2
from import_vsro_alexandria import TEXTDATA, archive_index, decode_table, table_rows

CHARACTER_CODE_COLUMN = 2


# ================
# shards
#
# The shard names an index file lists, or [] when the file is the table
# itself: every non-blank row of an index is one cell naming a .txt file.
# VSRO keeps some of v1.696's indexed names as whole tables, so the
# shape, not the name, decides.
# ================
def shards(rows):
	listed = [row[0].strip() for row in rows if row and row[0].strip()]
	if not listed or not all(len(row) == 1 and row[0].strip().lower().endswith(".txt") for row in rows if row and row[0].strip()):
		return []
	# A shard is a file beside its index, named once: a path or a
	# repeat would read outside the folder or count rows twice.
	folded = [sro_pk2.fold_ascii(shard) for shard in listed]
	if any("/" in shard or "\\" in shard or shard.startswith(".") for shard in folded) or len(set(folded)) != len(folded):
		raise ValueError(f"invalid shard index entries: {listed}")
	return listed


# ================
# is_index
#
# Whether one file's bytes are a shard index. Only a file whose first
# row is one cell naming a .txt file can be one, so a table's first line
# settles it before the whole file is split.
# ================
def is_index(raw):
	for line in decode_table(raw).splitlines():
		if not line.strip() or line.startswith("//"):
			continue
		if "\t" in line or not line.strip().lower().endswith(".txt"):
			return False
		return bool(shards(table_rows(raw)))
	return False


#============================================================================
# Archive
#============================================================================


# ================
# Archive
#
# One later client's textdata: a Media archive, an extracted textdata
# folder, or a dict of files (tests, staged bytes); a reader with its own
# rules (a build's overlay tree, a hashing reader) passes read_file(name)
# and has_file(name) itself. Names are folded to lower case, as the
# archive index keys them and a manifest's mixed-case shard names need.
# ================
class Archive:
	def __init__(self, read_file, has_file, label):
		self._read_file, self._has_file, self.label = read_file, has_file, label

	# ================
	# media
	#
	# The textdata of one mapped Media archive and its archive_index.
	# ================
	@classmethod
	def media(cls, media, index, label="Media.pk2"):
		def read_file(name):
			entry = index.get(TEXTDATA + name)
			if entry is None:
				raise SystemExit(f"{label} has no {TEXTDATA}{name}")
			return sro_pk2.payload(media, entry)
		return cls(read_file, lambda name: TEXTDATA + name in index, label)

	# ================
	# tree
	#
	# An extracted or built textdata folder.
	# ================
	@classmethod
	def tree(cls, root):
		root = Path(root)
		files = {path.name.lower(): path for path in root.iterdir() if path.is_file()} if root.is_dir() else {}

		def read_file(name):
			path = files.get(name)
			if path is None:
				raise SystemExit(f"{root} has no {name}")
			return path.read_bytes()
		return cls(read_file, lambda name: name in files, str(root))

	# ================
	# of
	#
	# A dict of {file name: bytes}.
	# ================
	@classmethod
	def of(cls, files, label="table files"):
		folded = {sro_pk2.fold_ascii(name): raw for name, raw in files.items()}

		def read_file(name):
			if name not in folded:
				raise SystemExit(f"{label} has no {name}")
			return folded[name]
		return cls(read_file, lambda name: name in folded, label)

	# ================
	# open
	#
	# The textdata of root/<archive>.pk2, mapped for the with block.
	# ================
	@staticmethod
	@contextmanager
	def open(root, archive="Media"):
		path = Path(root) / f"{archive}.pk2"
		if not path.is_file():
			raise SystemExit(f"{path} not found")
		with open(path, "rb") as handle, mmap.mmap(handle.fileno(), 0, access=mmap.ACCESS_READ) as data:
			yield Archive.media(data, archive_index(data), path.name)

	# ================
	# has
	# ================
	def has(self, name):
		return self._has_file(sro_pk2.fold_ascii(name))

	# ================
	# files
	#
	# The files a table's rows come from: its shards when the file is an
	# index, else the file itself.
	# ================
	def files(self, name):
		name = sro_pk2.fold_ascii(name)
		return [sro_pk2.fold_ascii(shard) for shard in shards(table_rows(self._read_file(name)))] or [name]

	# ================
	# rows
	#
	# A table's rows through its index. parse reads one file's bytes into
	# rows (table_rows, which ends a record at CRLF so a cell's own line
	# breaks stay in it, #4608) and runs per file, so a record never
	# crosses two shards. indexed_only refuses a whole
	# table: a reader of v1.696's layout (an id cell after the service
	# cell) must not misread VSRO's. optional reads a table the client
	# may not ship (v1.696 has no textdata_new.txt) as no rows.
	# ================
	def rows(self, name, parse=table_rows, indexed_only=False, optional=False):
		if optional and not self.has(name):
			return []
		files = self.files(name)
		if indexed_only and files == [sro_pk2.fold_ascii(name)]:
			raise ValueError(f"{name} is not a v1.696 shard index")
		return [row for part in files for row in parse(self._read_file(part))]

	# ================
	# rows_of
	#
	# Several tables read as one, each file once. An index may list a
	# table another name also stands for: VSRO's textdataname.txt indexes
	# textdata_equip&skill, textdata_object and textdata_new, so reading
	# it beside textdata_object would count every object row twice. Files
	# keep the order they are first named in.
	# ================
	def rows_of(self, names, parse=table_rows):
		files = list(dict.fromkeys(part for name in names for part in self.files(name)))
		return [row for part in files for row in parse(self._read_file(part))]

	# ================
	# text
	#
	# A table as one decoded text, its shards in index order.
	# ================
	def text(self, name):
		return "".join(decode_table(self._read_file(part)) for part in self.files(name))

	# ================
	# snapshot
	#
	# {file name: bytes} of every file the named tables come from, each
	# index with its shards, for a reader that outlives the archive's
	# mapping; Archive.of reads it back through the same index.
	# ================
	def snapshot(self, names):
		folded = [sro_pk2.fold_ascii(name) for name in names]
		parts = dict.fromkeys(part for name in folded for part in [name] + self.files(name))
		return {part: self._read_file(part) for part in parts}

	# ================
	# file
	#
	# One table's bytes, for a whole table only: an index read this way
	# would be its file names as rows, so it refuses and names the reader
	# that follows it. missing, when given, stands for a table the source
	# lacks (b"" for one a step may create).
	# ================
	def file(self, name, missing=None):
		if missing is not None and not self.has(name):
			return missing
		raw = self._read_file(sro_pk2.fold_ascii(name))
		if is_index(raw):
			raise ValueError(f"{self.label}: {name} is a shard index; read it with rows() or text()")
		return raw

	__call__ = file


# ================
# characters
#
# A later client's characterdata rows by codename (its third cell).
# ================
def characters(archive):
	return {row[CHARACTER_CODE_COLUMN]: row for row in archive.rows("characterdata.txt") if len(row) > CHARACTER_CODE_COLUMN}
