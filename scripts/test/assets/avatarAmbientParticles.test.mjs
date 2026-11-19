/*
===========================================================================

avatarAmbientParticles.test.mjs - worn avatars publish their ambient sets

The effect catalogue's reach follows equipmentAmbientParticles. It used to
skip every record without a compound-model slot, so the avatars (worn, but
slotless) never published their ambient programs: the later client's
steampunk attachment asked for system/avatar_w_steampunk_wing.efp and the
catalogue had none (2,970 live asset_failed reports, 11-18/19). The models
are injected, so these run without the extracted tree.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { AMBIENT_MODIFIER_KIND, equipmentAmbientParticles } from "../../build/char/equipmentParticles.mjs";
import { isAvatarItem, visualSocket } from "../../build/char/equipmentVisualRecords.mjs";

const ATTACH = "res/item/avatar/avatar_w_steampunk_attach.bsr";
const ARROWS = "res/item/etc/arrow.bsr";
const WING = "system\\avatar_w_steampunk_wing.efp";
// A ModDataParticle kind that is not the always-on ambient set.
const STATE_KIND = 1;

/*
================
record

An equipment record as loadEquipmentRecords shapes it.
================
*/
function record( id, tid, model ) {
	return { id, tid, resolvedModel: model, slot: visualSocket( tid ), avatar: isAvatarItem( tid ) };
}

/*
================
model

A parsed BSR holding one ambient set and one state set.
================
*/
function model() {
	return {
		particleModifiers: [
			{ kind: AMBIENT_MODIFIER_KIND, entries: [ { effectPath: WING } ] },
			{ kind: STATE_KIND, entries: [ { effectPath: "system\\state.efp" } ] }
		]
	};
}

test("a worn avatar's ambient set is published although it has no slot", () => {
	const records = new Map( [
		[ 1, record( 1, [ 3, 1, 13, 4 ], ATTACH ) ],
		// Ammunition is equipped but drawn by no worn resource.
		[ 2, record( 2, [ 3, 1, 4, 3 ], ARROWS ) ]
	] );
	const read = [];
	const ambient = equipmentAmbientParticles( records, source => (read.push( source ), model()) );
	assert.equal( records.get( 1 ).slot, null );
	assert.deepEqual( [ ...ambient.keys() ], [ ATTACH ] );
	assert.deepEqual( ambient.get( ATTACH ).map( m => m.kind ), [ AMBIENT_MODIFIER_KIND ] );
	assert.ok( !read.includes( ARROWS ), "a slotless non-avatar row is not read" );
});

test("an absent resource publishes nothing", () => {
	const records = new Map( [ [ 1, record( 1, [ 3, 1, 13, 4 ], ATTACH ) ] ] );
	assert.equal( equipmentAmbientParticles( records, () => null ).size, 0 );
});

test("only TypeID 3.1.13 is an avatar", () => {
	assert.equal( isAvatarItem( [ 3, 1, 13, 1 ] ), true );
	assert.equal( isAvatarItem( [ 3, 1, 1, 1 ] ), false );
	assert.equal( isAvatarItem( [ 3, 3, 13, 1 ] ), false );
});
