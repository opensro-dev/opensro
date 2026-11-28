"""
===========================================================================

later_fixtures.py - textdata fixtures in the shapes the clients ship

A test that feeds an importer a later client's tables builds them here,
so every such test meets the shapes later_tables must handle (#4762),
not a flat table it happens to read correctly:

	index                     one shard index's bytes, shards in archive case
	sharded / write_sharded   an index naming its shards in the archive's
	                          own mixed case (SkillData_5000.txt), the
	                          shards stored under folded names, as an
	                          extraction stores them
	name_index                VSRO's textdataname.txt: an index over files
	                          other table names also stand for
	wrap_last                 a text row the archive wraps over two lines
	                          at the end of a shard (#4730)

	utf16                     one table's bytes: BOM, tabs, CRLF

===========================================================================
"""
import re

INDEX_NAME_PART = re.compile(r"[a-z]+|[0-9]+|[^a-z0-9]+")
# translation_rows joins a wrapped line onto a row of fewer cells than
# this whose second cell is a name symbol.
TEXT_ROW_CELLS = 10


# ================
# utf16
# ================
def utf16(rows, newline="\r\n"):
	return b"\xff\xfe" + (newline.join("\t".join(row) for row in rows) + newline).encode("utf-16-le")


# ================
# archive_case
#
# A shard name in mixed case, as the clients' indexes spell theirs
# (SkillData_5000.txt): each word of the stem capitalized, the extension
# kept (skilldata_5000.txt -> Skilldata_5000.txt). Only a reader that
# folds names resolves it to the stored file.
# ================
def archive_case(name):
	stem, dot, extension = name.rpartition(".")
	return "".join(part.capitalize() for part in INDEX_NAME_PART.findall(stem)) + dot + extension


# ================
# index
#
# One shard index's bytes: each shard named in archive case, in the
# order given. For a fixture whose shards are written apart from it.
# ================
def index(*shards):
	return utf16([[archive_case(name)] for name in shards])


# ================
# sharded
#
# {file name: bytes} for one sharded table: the index (table) listing
# each shard in archive case, in the order given, and each shard's rows
# under its folded name.
# ================
def sharded(table, shards):
	files = {table: index(*shards)}
	for name, rows in shards.items():
		files[name.lower()] = rows if isinstance(rows, bytes) else utf16(rows)
	return files


# ================
# write_sharded
#
# sharded written into a textdata folder.
# ================
def write_sharded(folder, table, shards):
	for name, raw in sharded(table, shards).items():
		(folder / name).write_bytes(raw)


# ================
# name_index
#
# VSRO's textdataname.txt: an index listing other text tables' files
# (textdata_equip&skill.txt, textdata_object.txt ...), which a reader of
# those names also reaches. tables maps each listed file to its rows.
# ================
def name_index(tables, index="textdataname.txt"):
	return sharded(index, tables)


# ================
# wrap_last
#
# A text table's rows with the last row wrapped as the archive wraps
# it: its first spaced cell after the symbol breaks at the space and the
# rest of the row continues on the next line. alexandria_npcs.
# translation_rows joins it back. Refuses a row it cannot wrap.
# ================
def wrap_last(rows):
	last = rows[-1]
	for at in range(2, min(len(last), TEXT_ROW_CELLS - 1)):
		head, space, tail = last[at].partition(" ")
		if space and head and tail:
			return rows[:-1] + [last[:at] + [head], [" " + tail] + last[at + 1:]]
	raise ValueError(f"no cell of {last[:2]} wraps")
