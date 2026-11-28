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

A built v1.150 tree is read the same way (#4770): its characterdata,
itemdata and skilldata are indexes too, and the client loads only the
shards an index lists, in its order. Archive.tree reads one folder and
Archive.overlay a build's output over the native extraction; an
importer leaves its own shard out with rows(..., excluding=), never by
globbing <table>_*.txt (the gate refuses that too). A shard the index
does not list (skilldata_virtual.txt, a reap's leftover) is not read.

===========================================================================
"""
import mmap
from contextlib import contextmanager
from pathlib import Path

import sro_pk2
from import_vsro_alexandria import TEXTDATA, archive_index, decode_table, table_rows

CHARACTER_CODE_COLUMN = 2
# is_index reads a file's first line from this much of its head: a
# table's first line holds a tab well inside it, and a whole file is
# decoded only when the head reads like an index.
INDEX_HEAD_BYTES = 4096


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
# (read from the file's head) settles it before the whole file is split.
# ================
def is_index(raw):
	head = raw[:INDEX_HEAD_BYTES]
	if len(head) % 2 and head[:2] in (b"\xff\xfe", b"\xfe\xff"):
		head = head[:-1]
	for line in decode_table(head).splitlines():
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
# and has_file(name) itself, and list_files() when it can name its
# files. Names are folded to lower case, as the archive index keys them
# and a manifest's mixed-case shard names need.
# ================
class Archive:
	def __init__(self, read_file, has_file, label, list_files=None):
		self._read_file, self._has_file, self.label = read_file, has_file, label
		self._list_files = list_files

	# ================
	# names
	#
	# Every textdata file the source holds, folded and sorted: the
	# census index_matrix takes. A reader that cannot list refuses.
	# ================
	def names(self):
		if self._list_files is None:
			raise ValueError(f"{self.label} cannot list its files")
		return sorted(self._list_files())

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

		# ================
		# list_files
		#
		# The textdata folder's own files, not its subfolders'.
		# ================
		def list_files():
			return [key[len(TEXTDATA):] for key in index if key.startswith(TEXTDATA) and "/" not in key[len(TEXTDATA):]]
		return cls(read_file, lambda name: TEXTDATA + name in index, label, list_files)

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
		return cls(read_file, lambda name: name in files, str(root), files.keys)

	# ================
	# overlay
	#
	# A build's textdata: each file the output's copy when a step before
	# wrote one, else the native extraction's (native_root and
	# output_root are the two textdata folders). An index is followed
	# like any other, so the output's index, once a step rewrote it,
	# names the shards of both trees.
	# ================
	@classmethod
	def overlay(cls, native_root, output_root):
		native_root, output_root = Path(native_root), Path(output_root)
		native_paths = {sro_pk2.fold_ascii(path.name): path for path in native_root.iterdir() if path.is_file()} \
			if native_root.is_dir() else {}

		# ================
		# path_of
		# ================
		def path_of(name):
			written = output_root / name
			return written if written.is_file() else native_paths.get(name)

		# ================
		# read_file
		# ================
		def read_file(name):
			path = path_of(name)
			if path is None:
				raise ValueError(f"the tree has no {name}")
			return path.read_bytes()

		return cls(read_file, lambda name: path_of(name) is not None, "the built tree")

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
		return cls(read_file, lambda name: name in folded, label, folded.keys)

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
	# index, else the file itself. excluding names shards to leave out:
	# an importer's own, which its rebuild rewrites.
	# ================
	def files(self, name, excluding=()):
		name = sro_pk2.fold_ascii(name)
		left_out = {sro_pk2.fold_ascii(shard) for shard in excluding}
		listed = [sro_pk2.fold_ascii(shard) for shard in shards(table_rows(self._read_file(name)))] or [name]
		return [part for part in listed if part not in left_out]

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
	# excluding leaves the named shards out (files). width, when given,
	# is the fewest cells the caller's rows have: a file whose rows all
	# fall short is a wrong-shaped table (another client's layout, or an
	# index), and it raises instead of filtering down to nothing (#4762).
	# ================
	def rows(self, name, parse=table_rows, indexed_only=False, optional=False, excluding=(), width=None):
		if optional and not self.has(name):
			return []
		if indexed_only and self.files(name) == [sro_pk2.fold_ascii(name)]:
			raise ValueError(f"{name} is not a v1.696 shard index")
		return [row for part in self.files(name, excluding) for row in self._part_rows(name, part, parse, width)]

	# ================
	# _part_rows
	#
	# One listed file's rows. A file an index lists is a table: one that
	# is itself an index would hand its file names back as rows, and no
	# client nests (every v1.696 and VSRO textdata file, #4730), so it
	# refuses. width is rows' width.
	# ================
	def _part_rows(self, name, part, parse, width=None):
		raw = self._read_file(part)
		if part != sro_pk2.fold_ascii(name) and is_index(raw):
			raise ValueError(f"{self.label}: {part}, which {name} lists, is itself a shard index")
		rows = parse(raw)
		if width is not None and rows and not any(len(row) >= width for row in rows):
			raise ValueError(f"{self.label}: no row of {part} ({name}) has {width} cells: a wrong-shaped table")
		return rows

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
		files = list(dict.fromkeys((part, name) for name in names for part in self.files(name)))
		seen, rows = set(), []
		for part, name in files:
			if part not in seen:
				seen.add(part)
				rows += self._part_rows(name, part, parse)
		return rows

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
# index_matrix
#
# Which of a source's .txt files are shard indexes, and the shards each
# lists: {name: [shard, ...]} for every index (#4762). The checked-in
# census (scripts/data/later-table-indexes.json) is this, per client.
# ================
def index_matrix(archive):
	matrix = {}
	for name in archive.names():
		# is_index settles a table from its head, so the census never
		# splits a whole table.
		if name.endswith(".txt") and is_index(archive._read_file(name)):
			matrix[name] = archive.files(name)
	return matrix


# ================
# characters
#
# A later client's characterdata rows by codename (its third cell).
# ================
def characters(archive):
	return {row[CHARACTER_CODE_COLUMN]: row for row in archive.rows("characterdata.txt", width=CHARACTER_CODE_COLUMN + 1)
		if len(row) > CHARACTER_CODE_COLUMN}
