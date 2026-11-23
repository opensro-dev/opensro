/*
===========================================================================

job-honor-tiers.test.mjs - the tier title roster and its nameplate

Port-only, not native (#3530): under SRO_JOB_HONOR the server pushes
the division's tier roster (0x3FC7); a rostered player carries the
tier's coloured title on the name board. No roster (the flag off)
draws exactly the untitled stack.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import assert from "node:assert/strict";
import test from "node:test";

const { createJobHonorTiers, OP_JOB_HONOR_TIER_ROSTER } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/job-honor/job-honor-tiers.ts"
);
const { overheadLayout, withoutBoardPass, jobTierColor } = await import(
	"../../src/engine/foundation/ui/overhead-layout.ts"
);
const { defaultGameOptions } = await import( "../../src/engine/foundation/gameplay/game-options.ts" );
const { emptySocial } = await import( "../../src/engine/foundation/gameplay/social.ts" );

const options = defaultGameOptions();

/*
================
rosterBody
================
*/
function rosterBody( entries ) {
	let length = 2;
	for ( const [name] of entries ) length += 2 + name.length + 1;
	const payload = new Uint8Array( length ), view = new DataView( payload.buffer );
	view.setUint16( 0, entries.length, true );
	let offset = 2;
	for ( const [name, tier] of entries ) {
		view.setUint16( offset, name.length, true );
		offset += 2;
		for ( let i = 0; i < name.length; i++ ) payload[offset++] = name.charCodeAt( i );
		payload[offset++] = tier;
	}
	return payload;
}

test("the roster frame decodes name and tier, and replaces wholesale", () => {
	const tiers = createJobHonorTiers();
	assert.equal( tiers.tiers().size, 0 );
	assert.equal(
		tiers.receive( { opcode: OP_JOB_HONOR_TIER_ROSTER, payload: rosterBody( [ [ "Hero", 3 ], [ "Sage", 1 ] ] ) } ),
		true
	);
	assert.equal( tiers.tiers().get( "Hero" ), 3 );
	assert.equal( tiers.tiers().get( "Sage" ), 1 );
	// A shorter roster replaces the old one: Hero's title is gone.
	assert.equal(
		tiers.receive( { opcode: OP_JOB_HONOR_TIER_ROSTER, payload: rosterBody( [ [ "Sage", 2 ] ] ) } ),
		true
	);
	assert.equal( tiers.tiers().get( "Hero" ), undefined );
	assert.equal( tiers.tiers().get( "Sage" ), 2 );
	// Other opcodes are not this lane's.
	assert.equal( tiers.receive( { opcode: 0x3fc6, payload: new Uint8Array( 0 ) } ), false );
	tiers.clear();
	assert.equal( tiers.tiers().size, 0 );
});

test("a malformed roster is a protocol error to receive, fenced by receiveContained", () => {
	const tiers = createJobHonorTiers();
	tiers.receive( { opcode: OP_JOB_HONOR_TIER_ROSTER, payload: rosterBody( [ [ "Hero", 3 ] ] ) } );
	for (
		const payload of [
			new Uint8Array( 1 ),
			rosterBody( [ [ "Hero", 0 ] ] ),
			rosterBody( [ [ "Hero", 4 ] ] ),
			Uint8Array.of( 1, 0, 2, 0, 65 ),
			Uint8Array.of( 0, 0, 9 )
		]
	) {
		assert.throws(
			() => tiers.receive( { opcode: OP_JOB_HONOR_TIER_ROSTER, payload } ),
			/Invalid job honor tier/
		);
		// Contained: consumed, and the last good roster stands.
		assert.equal( tiers.receiveContained( { opcode: OP_JOB_HONOR_TIER_ROSTER, payload } ), true );
		assert.equal( tiers.tiers().get( "Hero" ), 3 );
	}
	// The rejections are on record: the three distinct messages (bad
	// tier twice and the trailing-bytes frame repeat the first), each
	// once, under this lane's code; then the record is drained.
	const drained = tiers.takeDegradations();
	assert.equal( drained.length, 3 );
	assert.ok( drained.every( d => d.code === "job_honor_tier_roster_rejected" ) );
	assert.equal( tiers.takeDegradations().length, 0 );
	// A valid roster still lands after the bad ones.
	assert.equal(
		tiers.receiveContained( { opcode: OP_JOB_HONOR_TIER_ROSTER, payload: rosterBody( [ [ "Sage", 2 ] ] ) } ),
		true
	);
	assert.equal( tiers.tiers().get( "Sage" ), 2 );
});

const local = { gid: 1, refObjId: 1907, kind: "local-player", name: "Me", heading: 0, regionId: 257, x: 0, y: 0, z: 0 };
const hero = { ...local, gid: 5, kind: "player", name: "Hero", guildName: "Guild" };

/*
================
layoutFor

The layout reads the whole GameplayState type; the fixtures below carry
only the fields the overhead rows consult.
================
*/
function layoutFor( entity, game ) {
	return overheadLayout( entity, local, /** @type {any} */ (game), options );
}

test("a rostered player carries the tier's coloured title, adding no row", () => {
	const game = { localGid: 1, social: emptySocial( "Me" ), vitals: [], jobHonorTiers: new Map( [ [ "Hero", 3 ] ] ) };
	const layout = layoutFor( hero, game );
	assert.equal( layout.tierText, "King" );
	assert.deepEqual( layout.tierColor, jobTierColor( 3 ) );
	// The title rides the name's own line, so the rows above the name
	// stand exactly where an untitled player's do.
	const plain = layoutFor( hero, { ...game, jobHonorTiers: new Map() } );
	assert.equal( plain.tierText, "" );
	assert.equal( layout.guildY, plain.guildY );
	assert.equal( layout.statusY, plain.statusY );
	assert.equal( layout.speechY, plain.speechY );
	// The local player's own title shows too, in its own tier's word.
	const own = layoutFor( local, { ...game, jobHonorTiers: new Map( [ [ "Me", 1 ] ] ) } );
	assert.equal( own.tierText, "Silver" );
	// Only a player carries the title, whatever the roster names.
	const monster = { ...hero, kind: "monster" };
	assert.equal( layoutFor( monster, game ).tierText, "" );
	// The /id-off pass hides it with the rest of the kind-4 rows.
	assert.equal( withoutBoardPass( layout ).tierText, "" );
});

test("each tier speaks its own word and colour", () => {
	assert.deepEqual( jobTierColor( 1 ), [ 0.8, 0.85, 0.95, 1 ] );
	assert.deepEqual( jobTierColor( 2 ), [ 1, 0.83, 0.25, 1 ] );
	assert.deepEqual( jobTierColor( 3 ), [ 1, 0.6, 0.08, 1 ] );
	const game = { localGid: 1, social: emptySocial( "Me" ), vitals: [] };
	for ( const [tier, word] of [ [ 1, "Silver" ], [ 2, "Gold" ], [ 3, "King" ] ] ) {
		const layout = layoutFor( hero, { ...game, jobHonorTiers: new Map( [ [ "Hero", tier ] ] ) } );
		assert.equal( layout.tierText, word );
	}
});
