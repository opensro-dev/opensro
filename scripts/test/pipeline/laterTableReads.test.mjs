/*
===========================================================================

laterTableReads.test.mjs - the later textdata gate and what it refuses

Each read that went around scripts/later_tables.py before #4761, as the
importers spelled it, is refused; the reads through the owner, archive
writes and prose are not.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { findLaterTableReads } from "../../checks/check_later_table_reads.mjs";

/*
================
refused

The rule one line of code breaks, or null.
================
*/
function refused( line ) {
	const hits = findLaterTableReads( line + "\n" );
	return hits.length ? hits[0].why : null;
}

test("the hand reads #4761 retired are refused", () => {
	const retired = [
		"\treturn sro_pk2.payload(media, media_index[TEXTDATA + LOCALINFO])",
		"\t\tentry = media_index.get((tables.TEXTDATA + name).lower())",
		'\tvsro = {name: variant.read_vsro_file(vsro_root, "Media", stage1.TEXTDATA + name) for name in names}',
		'\tif tables.TEXTDATA + "characterdata.txt" not in index:',
		"\t\treturn tables.TEXTDATA + name.lower() in index",
		"\tsound = tables.table_text(media, media_index, SOUND_TABLE)",
		'\tfor name in sorted(n for n in media_index if n.startswith(TEXTDATA + "textdata_object")):',
		'\t\t\tif not wanted_text or not name.startswith("textdata_") or not name.endswith(".txt"):',
		'REF_TITLE_PATH = "server_dep/silkroad/textdata/RefTitle.txt"',
		'\traw = source._read_file("textdata_object.txt")'
	];
	for ( const line of retired ) assert.notEqual( refused( line ), null, line );
});

test("reads through the owner, writes and the row serializer pass", () => {
	const allowed = [
		"\tsource = later_tables.Archive.media(media, index)",
		'\trows = source.rows("textdata_object.txt", characters.translation_rows)',
		"\tif not source.has(LOCALINFO):",
		'\t(root / "Media.pk2").write_bytes(pk2({stage1.TEXTDATA + name: raw for name, raw in FILES.items()}))',
		'\tentries["media/" + tables.TEXTDATA + SHARD_NAME] = table_bytes(final)',
		'\traw = b"\\xff\\xfe" + npcs.table_text(rows).encode("utf-16-le")',
		'TEXT_ROOT = Path("Media_extracted") / tables.TEXTDATA',
		'TEXTDATA = "server_dep/silkroad/textdata/"'
	];
	for ( const line of allowed ) assert.equal( refused( line ), null, line );
});

test("comments and docstrings are prose", () => {
	const text = [
		'"""',
		"The table is server_dep/silkroad/textdata/RefTitle.txt (id, codename).",
		'"""',
		"# media_index[TEXTDATA + name] read raw lost every v1.696 name",
		"def later(read):",
		'\t"""One line: sro_pk2.payload(media, media_index[TEXTDATA + name])."""',
		'\treturn read.rows("itemdata.txt")  # not payload(media, index[TEXTDATA + n])'
	].join( "\n" );
	assert.deepEqual( findLaterTableReads( text ), [] );
});

test("a finding names its line and its rule", () => {
	const text = "def read(name):\n\t# the owner reads it\n\treturn tables.table_text(media, index, name)\n";
	assert.deepEqual( findLaterTableReads( text ), [ {
		line: 3,
		text: "return tables.table_text(media, index, name)",
		why: "the retired owner form table_text(media, index, name)"
	} ] );
});
