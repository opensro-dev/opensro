/*
===========================================================================

bug-report-companions.test.mjs - a report carries owned COS without inventory

Issue #4222: state.json omitted fat cosRecords (inventories) under the
per-field size bound, so a pet-attack report had no pet. diagnosticCompanions
keeps the compact rows (gid, band, hp, status, command mode, assist target)
outside that omit.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { diagnosticCompanions } = await import( "../../src/engine/runtime/bug-report/bug-report.ts" );

test("companion rows drop inventory and keep assist target", () => {
	const rows = diagnosticCompanions( [ {
		gid: 42,
		refObjId: 1,
		band: 3,
		hp: 900,
		mp: 0,
		status: 0,
		dead: false,
		commandMode: 2,
		assistTarget: 99,
		inventory: [ {
			slot: 0,
			refObjId: 7,
			typeFlags: 0,
			quantity: 1,
			plus: 0,
			durability: 0,
			variance: "0",
			magic: []
		} ]
	} ] );
	assert.deepEqual( rows, [ {
		gid: 42,
		band: 3,
		hp: 900,
		status: 0,
		dead: false,
		commandMode: 2,
		assistTarget: 99
	} ] );
	assert.equal( "inventory" in rows[0], false );
});

test("empty or missing cosRecords yield no companions", () => {
	assert.deepEqual( diagnosticCompanions( undefined ), [] );
	assert.deepEqual( diagnosticCompanions( [] ), [] );
});
