/*
===========================================================================

regionCodeCatalog.test.mjs - one region code per region id, the first row's

v1.696's RegionCode.txt names region 22478 twice (RN_JUPITER_GOD, then
RN_JUPITER_SEA). The client's region table insert (sub_721530, from the
loader sub_722460) keeps the entry it already holds, so the first row
names the region; the catalogue once kept the last.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { parseRegionCodes } from "../../build/shared/textResources.mjs";

test("a repeated region id keeps its first code, as the client's table does", () => {
	const raw = [
		"1\t22478\tRN_JUPITER_GOD\tgarden",
		"1\t22479\tRN_JUPITER_A\tfield",
		"1\t22478\tRN_JUPITER_SEA\tcoast",
		"0\t22480\tRN_DISABLED\toff",
		"1\t22481\txxx\tplaceholder"
	].join( "\r\n" );
	const { entries, rows } = parseRegionCodes( raw );
	assert.deepEqual( entries, { "22478": "RN_JUPITER_GOD", "22479": "RN_JUPITER_A" } );
	assert.deepEqual( rows.map( ( row ) => row.key ), [ "RN_JUPITER_GOD", "RN_JUPITER_A", "RN_JUPITER_SEA" ] );
});
