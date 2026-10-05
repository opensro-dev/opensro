/*
===========================================================================

overlays.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
await mkdir( "temp/artifacts/overlay-tests", { recursive: true } );
/*
================
load
================
*/
async function load( path, name ) {
	return import( sourceFileUrl( "src/engine/" + path ).href );
}
const { overheadLayout } = await load( "foundation/ui/overhead-layout.ts", "overhead" );
const { partyDistanceShade, partyLocalPose, partyOverlay, partyRosterPose, partyShadeImage } = await load(
	"foundation/ui/party-overlay.ts",
	"party"
);
const { buffViewerIcons, collectActiveBuffs, partyBuffViewer, rebuildBuffViewer, skillLookup } = await load(
	"foundation/ui/buff-viewer.ts",
	"buff-viewer"
);
const { fortressBootstrap, fortressPacket, fortressStatus } = await load(
	"foundation/gameplay/fortress.ts",
	"fortress"
);
const { defaultGameOptions } = await load( "foundation/gameplay/game-options.ts", "options" );
const { prepareUi } = await load( "foundation/ui/ui.ts", "ui" );
const { socialPacket, emptySocial } = await load( "foundation/gameplay/social.ts", "social" );
const local = { gid: 1, kind: "local-player", name: "Self", regionId: 257, x: 0, y: 0, z: 0 };
const peer = {
	...local,
	gid: 2,
	kind: "player",
	name: "Peer",
	guildName: "Guild",
	guildGrantName: "Officer",
	guildId: 10,
	guildWarTeam: 1
};
const game = {
	localGid: 1,
	vitals: [],
	social: {
		...emptySocial( "Self" ),
		leader: 11,
		members: [ { id: 11, name: "Self", status: 0xaa }, { id: 12, name: "Peer", status: 0x85 } ],
		guild: { id: 10, name: "Guild", members: [ { name: "Self", grant: "Master" } ] }
	}
};
test("guild/status/speech share native stack independently of hidden base names", () => {
	const options = { ...defaultGameOptions(), playerNames: false, ownName: false, partyStatus: true };
	const row = overheadLayout( peer, local, game, options );
	assert.equal( row.guildText, "[Guild * Officer]" );
	assert.deepEqual( row.guildColor, [ 254 / 255, 173 / 255, 46 / 255, 1 ] );
	assert.equal( row.guildY, -20 );
	assert.equal( row.statusY, -45 );
	assert.equal( row.speechY, -45 );
	assert.equal( overheadLayout( peer, local, game, { ...options, guildNames: false } ).statusY, -25 );
	assert.equal(
		overheadLayout( { ...peer, equipment: [ { slot: 8, typeFlags: 0x1000 } ] }, local, game, options ).guildText,
		""
	);
	assert.equal( overheadLayout( local, local, game, options ).guildText, "[Guild * Master]" );
	assert.equal(
		overheadLayout( peer, local, { ...game, social: { ...game.social, members: [] } }, options ).speechY,
		-20
	);
});
test("party slot viewer: bbuf and phase-1 effects, one row per list, abnormal bits appended", () => {
	const skillCatalog = [ { id: 1, icon: "skill/china/sword_smash_a.ddj", buffSecondary: true }, {
		id: 2,
		icon: "skill/china/sword_smash_a.ddj",
		buffSecondary: false
	} ];
	// 776450 / 85FB20: the status byte is only a flag, so phase 1 still displays.
	const attachedEffects = Array.from(
		{ length: 10 },
		( _, i ) => ({ gid: 2, skill: i < 8 ? 2 : 1, token: i, phase: i === 9 ? 1 : 2 })
	);
	const lookup = skillLookup( skillCatalog ),
		state = rebuildBuffViewer( partyBuffViewer(), collectActiveBuffs( attachedEffects, 2, lookup ), 0x11, lookup );
	const icons = buffViewerIcons( partyBuffViewer(), state, 2, lookup, { unlevelled: true } );
	assert.equal( icons.length, 10 );
	assert.deepEqual( icons.map( i => i.y ), [ 0, 0, 0, 0, 0, 0, 17, 17, 17, 17 ] );
	assert.match( icons[8].path, /s_freeze_icon/ );
	assert.match( icons[9].path, /s_poisoning_icon/ );
	assert.deepEqual( icons[9].helpSource, { kind: "abnormal", gid: 2, bit: 4, unlevelled: true, viewer: true } );
	const row = partyOverlay( { ...game, vitals: [] }, [ local, peer ], 900, 4, 137, true )[0];
	assert.equal( row.entity, peer );
	assert.equal( partyOverlay( game, [ local ], 900, 4, 137, true )[0].entity, undefined );
	assert.deepEqual( [ row.hp, row.mp ], [ .5, .8 ] );
});
test("party wraps with the native bottom reserve and option-specific row pitch", () => {
	const state = {
		...game,
		social: {
			...game.social,
			members: [
				game.social.members[0],
				...Array.from( { length: 7 }, ( _, i ) => ({ id: i + 20, name: "P" + i, status: 0xaa }) )
			]
		}
	};
	assert.deepEqual( partyOverlay( state, [], 600, 4, 137, true ).map( r => r.position ), [
		[ 17, 137 ],
		[ 17, 210 ],
		[ 17, 283 ],
		[ 150, 137 ],
		[ 150, 210 ],
		[ 150, 283 ],
		[ 283, 137 ]
	] );
	assert.deepEqual( partyOverlay( state, [], 600, 4, 137, false )[1].position, [ 17, 193 ] );
});
test("party rows list the member's trained masteries left of the buff row", () => {
	const member = ( id, primaryMastery, secondaryMastery ) => ({
		id,
		name: "M" + id,
		status: 0xaa,
		primaryMastery,
		secondaryMastery
	});
	const state = {
		...game,
		social: {
			...game.social,
			members: [ member( 20, 258, 273 ), member( 21, 513, 0 ), member( 22, 0, 0 ), {
				id: 23,
				name: "M23",
				status: 0xaa
			} ]
		}
	};
	const rows = partyOverlay( state, [], 900, 4, 137, true );
	assert.deepEqual( rows[0].masteries, [
		{ id: 258, rect: [ rows[0].position[0] + 2, 137 + 39, 12, 12 ] },
		{ id: 273, rect: [ rows[0].position[0] + 17, 137 + 39, 12, 12 ] }
	] );
	assert.deepEqual( rows[1].masteries.map( m => m.id ), [ 513 ] );
	assert.deepEqual( rows[2].masteries, [] );
	assert.deepEqual( rows[3].masteries, [] );
});
test("the distance shade measures from the live pose, not the lagging local entity", () => {
	const at = ( x, regionId = 0x6a48 ) => ({ regionId, x, y: 0, z: 0, angle: 0 });
	const stale = { ...local, regionId: 0x6a48, x: 0, y: 0, z: 0 };
	const walked = { ...game, pose: at( 900 ) };
	assert.equal( partyLocalPose( walked, [ stale ] ), walked.pose );
	assert.equal( partyDistanceShade( partyLocalPose( walked, [ stale ] ), at( 0 ) ), 4 );
	assert.equal( partyLocalPose( { ...game, pose: null }, [ stale ] ), stale );
	assert.equal( partyLocalPose( { ...game, pose: null }, [] ), null );
});
test("party portraits shade by the member's squared distance (5BD0A0)", () => {
	const at = ( regionId, x, z, y = 0 ) => ({ regionId, x, y, z, angle: 0 });
	const local = at( 0x6a48, 100, 100 );
	// Inclusive steps at 600, 700, 800, 900 and 1000 units.
	assert.deepEqual(
		[ 599, 600, 699, 700, 800, 900, 999, 1000, 5000 ].map( d =>
			partyDistanceShade( local, at( 0x6a48, 100 + d, 100 ) )
		),
		[ 0, 1, 1, 2, 3, 4, 4, 5, 5 ]
	);
	// Distance crosses region borders and counts height.
	assert.equal( partyDistanceShade( local, at( 0x6a49, 100 - 1920 + 650, 100 ) ), 1 );
	assert.equal( partyDistanceShade( local, at( 0x6a48, 100, 100, 650 ) ), 1 );
	// A member out of view is measured at its roster record with no height.
	assert.deepEqual( partyRosterPose( { region: 0x6a48, x: 5, y: 77, z: 6 } ), at( 0x6a48, 5, 6 ) );
	assert.equal( partyDistanceShade( at( 0x6a48, 0, 0, 650 ), partyRosterPose( { region: 0x6a48, x: 0, z: 0 } ) ), 1 );
	// 888140: an indoor region adds no sector offset; local coordinates compare.
	assert.equal( partyDistanceShade( local, at( 0x8001, 100, 100 ) ), 0 );
	assert.equal( partyDistanceShade( at( 0x8001, 0, 0 ), at( 0x8002, 700, 0 ) ), 2 );
	assert.deepEqual( [ 0, 1, 5 ].map( partyShadeImage ), [
		null,
		"interface/quickparty/qpt_face_faraway_60.png",
		"interface/quickparty/qpt_face_faraway_100.png"
	] );
});
test("fortress marks cover every status/team and native XOR end arm", () => {
	const base = {
		...fortressBootstrap( {
			localPlayerEntry: { fortressWorld: 2 },
			gameWorldData: [ { gameWorldId: 2, warName: "F" } ],
			siegeFortressData: [ { fortressId: 1, codeName: "F" } ]
		} ),
		worldId: 2,
		wars: [ { id: 1, name: "F", flags: 1 } ],
		registered: [ 10 ]
	};
	assert.equal( fortressStatus( base, 10, 10, [] ), 0xc9 );
	assert.equal( fortressStatus( base, 10, 20, [] ), 0xcc );
	assert.equal( fortressStatus( base, 20, 10, [] ), 0xca );
	assert.equal( fortressStatus( { ...base, registered: [] }, 20, 20, [] ), 0xcb );
	assert.equal( fortressStatus( { ...base, registered: [] }, 20, 30, [ 30 ] ), 0xcb );
	assert.equal( fortressStatus( base, 0, 10, [] ), 0xc8 );
	assert.equal( fortressStatus( { ...base, worldId: 0x10001 }, 10, 10, [] ), 0xcd );
	for ( const team of [ 1, 2 ] ) {
		const row = overheadLayout(
			{ ...peer, guildWarTeam: team },
			local,
			{ ...game, fortress: base },
			defaultGameOptions()
		);
		assert.match( row.fortressMark.path, new RegExp( "mark_fortress" + (team + 1) ) );
	}
	assert.equal(
		overheadLayout( { ...peer, guildWarTeam: 0 }, local, { ...game, fortress: base }, defaultGameOptions() )
			.fortressMark,
		null
	);
	const ended = fortressPacket( base, { opcode: 0x3887, payload: Uint8Array.of( 6 ) } );
	assert.equal( ended.wars[0].flags, 0 );
	assert.equal( fortressPacket( ended, { opcode: 0x3887, payload: Uint8Array.of( 6 ) } ).wars[0].flags, 1 );
	assert.throws(
		() => fortressPacket( base, { opcode: 0x3887, payload: Uint8Array.of( 0x10, 0, 0, 0, 0, 2, 1, 0, 0, 0 ) } ),
		/Truncated/
	);
	assert.deepEqual( base.registered, [ 10 ] );
});
test("alliance bulk decoding retains header and upserts atomically", () => {
	const bytes = [];
	const u32 = n => bytes.push( n & 255, n >>> 8 & 255, n >>> 16 & 255, n >>> 24 ),
		str = s => bytes.push( s.length, 0, ...new TextEncoder().encode( s ) );
	u32( 21 );
	u32( 22 );
	u32( 10 );
	bytes.push( 1 );
	u32( 11 );
	str( "Ally" );
	bytes.push( 5 );
	str( "Master" );
	u32( 1907 );
	bytes.push( 1 );
	const state = socialPacket( emptySocial(), { opcode: 0x341e, payload: Uint8Array.from( bytes ) } );
	assert.deepEqual( state.allianceCrests, [ 21, 22 ] );
	assert.equal( state.alliances[0].name, "Ally" );
	assert.equal( state.allianceMaster, 10 );
	assert.throws(
		() => socialPacket( state, { opcode: 0x341e, payload: Uint8Array.from( bytes.slice( 0, -1 ) ) } ),
		/Truncated/
	);
	assert.equal( state.alliances.length, 1 );
});
test("multiple portraits get separate bounded render targets without mutating source quads", () => {
	const quad = {
		rect: [ 0, 0, 28, 28 ],
		clip: [ 0, 0, 100, 100 ],
		uv: [ 0, 0, 1, 1 ],
		color: [ 1, 1, 1, 1 ],
		texture: "__portrait"
	};
	const scene = {
		revision: 1,
		width: 100,
		height: 100,
		quads: [ 1, 2, 1, 3 ].map( portraitGid => ({ ...quad, portraitGid }) )
	};
	const product = prepareUi( scene );
	assert.deepEqual( product.portraits, [ 1, 2, 3 ] );
	assert.deepEqual( product.scene.quads.map( q => q.texture ), [
		"__portrait",
		"__portrait1",
		"__portrait",
		"__portrait2"
	] );
	assert.equal( scene.quads[1].texture, "__portrait" );
	assert.throws(
		() =>
			prepareUi( {
				...scene,
				quads: Array.from( { length: 9 }, ( _, i ) => ({ ...quad, portraitGid: i + 1 }) )
			} ),
		/capacity/
	);
});
