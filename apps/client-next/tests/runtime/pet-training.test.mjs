/*
===========================================================================

pet-training.test.mjs - the attack-pet tree's 0x3FB1 state and its help text

The server's state push (item/wire/pettraining.go) decodes to skills, row
cooldowns and worn gear with its blue options; a malformed push is a
protocol error, never a partial state. Help and affix lines read the
design workbook's generated module.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { createPetTraining, OP_PET_TRAINING_STATE } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/pet-training/pet-training.ts"
);
const {
	petSkillHelp,
	petSkillTooltip,
	petSkillDesign,
	petReference,
	petAbilities,
	petAffixLine,
	petRows,
	petEffect,
	petCooldownQuads,
	petRowCooldown,
	PET_AFFIX_PARAM_BASE,
	PET_GEAR_SLOTS
} = await import(
	"../../src/engine/foundation/ui/pet-training.ts"
);
const { PET_DESIGN } = await import( "../../src/engine/foundation/ui/pet-design-data.ts" );
const { quickSlotDrag, quickSlotDrop, quickSlotCommand, PET_SKILL_SHORTCUT_BASE } = await import(
	"../../src/engine/foundation/gameplay/quickslots.ts"
);

/*
================
statePayload

The server's layout: gid, points, attack%, defense%, two rows, slotted
passives, skills, gear with options.
================
*/
function statePayload( skills, gear ) {
	const bytes = [];
	const u16 = v => bytes.push( v & 0xff, v >> 8 );
	const u32 = v => bytes.push( v & 0xff, (v >> 8) & 0xff, (v >> 16) & 0xff, v >>> 24 );
	const u64 = v => {
		for ( let i = 0n; i < 8n; i++ ) bytes.push( Number( (v >> (8n * i)) & 0xffn ) );
	};
	u32( 0x2000001 );
	bytes.push( 3 );
	u16( 109 );
	u16( 103 );
	u32( 4000 );
	u16( 0 );
	bytes.push( 5, ..."Timer".split( "" ).map( c => c.charCodeAt( 0 ) ) );
	u32( 0 );
	u16( 2 );
	bytes.push( 7, ..."Attacks".split( "" ).map( c => c.charCodeAt( 0 ) ) );
	bytes.push( 0, 0, 0 );
	bytes.push( skills.length );
	for ( const s of skills ) {
		bytes.push( s.id, s.level, s.effective, s.next, s.flags );
		u16( s.value );
		u16( s.nextValue );
	}
	bytes.push( gear.length );
	for ( const [ref, options] of gear ) {
		u32( ref );
		bytes.push( options.length );
		options.forEach( u64 );
	}
	return Uint8Array.from( bytes );
}

const wolf = PET_DESIGN.treeSkills.find( s => s.pet === "Wolf" && s.row === 0 && s.status === "Implemented" );
const affix = PET_DESIGN.affixes.find( a => a.status === "Implemented" );
if ( !wolf || !affix ) throw new Error( "Missing implemented pet fixtures" );
const blue = (7n << 32n) | BigInt( PET_AFFIX_PARAM_BASE + affix.code );
const collar = PET_DESIGN.gear[0].items[0].id;

test("the state decodes rows, skills and worn gear with its blues", () => {
	const asked = [];
	const blues = Array.from( { length: 6 }, ( _, i ) => blue + (BigInt( i ) << 32n) );
	const training = createPetTraining( ( ref, slot, magic ) => {
		asked.push( [ ref, slot, magic ] );
		return {
			slot,
			refObjId: ref,
			name: "Collar",
			typeFlags: 0,
			quantity: 1,
			plus: 0,
			durability: 0,
			variance: "0",
			magic
		};
	} );
	const payload = statePayload( [
		{ id: wolf.id, level: 1, effective: 2, next: 21, flags: 1 | 2 | 4 | 8, value: 114, nextValue: 118 }
	], [ [ collar, blues ], [ 0, [] ], [ 0, [] ] ] );
	assert.equal( training.receive( { opcode: 0x3fb0, payload }, 5 ), false );
	assert.equal( training.receive( { opcode: OP_PET_TRAINING_STATE, payload }, 5 ), true );
	const state = training.state();
	assert.ok( state );
	assert.equal( state.gid, 0x2000001 );
	assert.equal( state.points, 3 );
	assert.deepEqual( state.rows, [ { timerMs: 4000, left: 0, kind: "Timer" }, {
		timerMs: 0,
		left: 2,
		kind: "Attacks"
	} ] );
	assert.deepEqual( state.skills[0], {
		id: wolf.id,
		level: 1,
		effectiveLevel: 2,
		nextPetLevel: 21,
		action: true,
		queued: true,
		learnable: true,
		live: true,
		slotted: false,
		value: 11.4,
		nextValue: 11.8
	} );
	assert.deepEqual( asked, [ [ collar, 0, blues.map( String ) ] ] );
	assert.deepEqual( state.gear.map( g => g?.name ?? null ), [ "Collar", null, null ] );
	training.clear();
	assert.equal( training.state(), undefined );
});

test("pet timers reuse character sweep and expire; event counters do not expire with time", () => {
	const state = {
		gid: 1,
		points: 0,
		attackPercent: 100,
		defensePercent: 100,
		slotted: [],
		skills: [],
		gear: [],
		receivedAtMs: 1000,
		rows: [ { timerMs: 4000, left: 0, kind: "Timer" }, { timerMs: 0, left: 3, kind: "Attacks" } ]
	};
	/** @type {import('../../src/engine/contracts/ui.ts').UiRect} */
	const rect = [ 0, 0, 32, 32 ];
	const timer = petCooldownQuads( state, 0, 2000, rect, rect );
	assert.ok( timer[0].texture.endsWith( "skill_delay.png" ) );
	assert.ok( timer[1].texture.endsWith( "cool_time_3.png" ) );
	assert.equal( petRowCooldown( state, 0, 2000 ), "3s" );
	assert.equal( petRowCooldown( state, 0, 5000 ), "" );
	assert.deepEqual( petCooldownQuads( state, 0, 5500, rect, rect ), [] );
	assert.deepEqual( petCooldownQuads( state, 1, 100000, rect, rect ), [] );
	assert.equal( petRowCooldown( state, 1, 100000 ), "3 attacks" );
	const design = petSkillDesign( 1 );
	assert.ok( design );
	assert.equal( petEffect( design, 3.5 ).includes( "3.5" ), false );
	const lastStand = PET_DESIGN.treeSkills.find( entry => entry.family === "Last Stand" );
	assert.ok( lastStand );
	assert.match( petEffect( lastStand, 31, 8 ), /40% HP/ );
});

test("a truncated or padded state is refused", () => {
	const training = createPetTraining( () => null );
	const payload = statePayload( [], [] );
	assert.throws( () => training.receive( { opcode: OP_PET_TRAINING_STATE, payload: payload.subarray( 0, 20 ) }, 0 ) );
	assert.throws( () =>
		training.receive( { opcode: OP_PET_TRAINING_STATE, payload: Uint8Array.from( [ ...payload, 0 ] ) }, 0 )
	);
	assert.equal( training.state(), undefined );
});

test("help, rows and blue lines read the design", () => {
	const skill = {
		id: wolf.id,
		level: 1,
		effectiveLevel: 1,
		nextPetLevel: 15,
		action: true,
		queued: false,
		learnable: false,
		live: true,
		slotted: false,
		value: 5,
		nextValue: 6
	};
	const help = petSkillHelp( wolf, skill, 12, 0 );
	assert.match( help, new RegExp( `^${wolf.name} \\(action\\)\\nLevel 1 / ${wolf.maxLevel}\\n` ) );
	assert.match( help, /Needs pet level 15 \(now 12\) and 1 skill point \(0 free\)\./ );
	for ( const [level, points, color] of [ [ 12, 1, 0xffff5555 ], [ 15, 0, 0xffff5555 ], [ 15, 1, 0xffffffff ] ] ) {
		const requirement = petSkillTooltip( wolf, skill, level, points ).find( row =>
			row.value.startsWith( "Needs pet level" )
		);
		assert.equal( requirement?.color, color );
	}
	assert.match( help, /numbered quickslot to cast/ );
	const rows = petRows( {
		gid: 1,
		skills: [ skill ],
		points: 0,
		attackPercent: 100,
		defensePercent: 100,
		slotted: [],
		gear: [],
		rows: [],
		receivedAtMs: 0
	} );
	assert.equal( rows.length, 5 );
	assert.equal( rows[0][0].design.id, wolf.id );
	assert.equal( petAffixLine( blue.toString() ), affix.effect.replaceAll( "{v}", "7" ) );
	assert.equal( petAffixLine( String( (7n << 32n) | 3n ) ), null );
	assert.equal( PET_GEAR_SLOTS.length, 3 );
});

test("learned pet actives bind to normal numbered slots and resolve the current pet and target", () => {
	const training = createPetTraining( () => null );
	training.receive( {
		opcode: OP_PET_TRAINING_STATE,
		payload: statePayload( [ {
			id: wolf.id,
			level: 1,
			effective: 1,
			next: 15,
			flags: 9,
			value: 50,
			nextValue: 60
		} ], [] )
	}, 0 );
	const petTraining = training.state();
	/** @type {import('../../src/engine/contracts/gameplay.ts').GameplayState} */
	const state = /** @type {import('../../src/engine/contracts/gameplay.ts').GameplayState} */ (
		/** @type {unknown} */ ({
			inventory: [],
			petTraining,
			target: 123,
			cosRecords: [ { gid: 0x2000001, band: 3, hp: 100 } ]
		})
	);
	const binding = quickSlotDrag( `pet-skill:${wolf.id}`, 5, state );
	assert.deepEqual( binding, { slot: 5, kind: 0x4a, payload: PET_SKILL_SHORTCUT_BASE + wolf.id } );
	assert.ok( binding );
	assert.deepEqual( quickSlotDrop( `pet-skill:${wolf.id}`, 5, state ), [ binding ] );
	assert.deepEqual( quickSlotCommand( binding, state ), {
		kind: "cos-training",
		gid: 0x2000001,
		operation: 2,
		arg: wolf.id,
		target: 123
	} );
	assert.equal( quickSlotCommand( binding, { ...state, petTraining: undefined } ), null );
	assert.equal( quickSlotCommand( binding, { ...state, cosRecords: [] } ), null );
	assert.equal( quickSlotDrag( "pet-skill:255", 5, state ), null );
});

/*
================
combat profile state and display
================
*/
test("server combat profile updates HP, defenses and magical attack while legacy state stays native", () => {
	const training = createPetTraining( () => null );
	const base = statePayload( [], [] ), tail = new Uint8Array( 10 ), view = new DataView( tail.buffer );
	tail[0] = 1;
	view.setUint32( 1, 700, true );
	view.setUint16( 5, 180, true );
	view.setUint16( 7, 120, true );
	tail[9] = 1;
	training.receive( { opcode: OP_PET_TRAINING_STATE, payload: Uint8Array.from( [ ...base, ...tail ] ) }, 0 );
	const state = training.state();
	assert.ok( state?.stats );
	assert.equal( state.stats.maxHp, 700 );
	const reference = {
		maxHp: 1000,
		physicalDefence: 100,
		magicalDefence: 100,
		icon: "",
		rideable: false,
		skills: [],
		hit: 100,
		parry: 100
	};
	const projected = petReference( reference, state );
	assert.ok( projected );
	assert.equal( projected.maxHp, 700 );
	assert.equal( projected.physicalDefence, 103 );
	assert.equal( projected.magicalDefence, 120 );
	/** @type {ReturnType<typeof import("../../src/engine/foundation/ui/cos-command.ts").cosAbilities>} */
	const ability = {
		low: false,
		physical: [ 100, 200 ],
		magical: [ 0, 0 ],
		physicalDefence: 103,
		magicalDefence: 120,
		hit: 100,
		parry: 100
	};
	assert.deepEqual( petAbilities( ability, state ).physical, [ 0, 0 ] );
	assert.deepEqual( petAbilities( ability, state ).magical, [ 180, 360 ] );
	assert.equal( petReference( reference, undefined ), reference );
	assert.equal( petAbilities( ability, undefined ), ability );
	const equippedTail = new Uint8Array( 36 );
	equippedTail.set( tail );
	equippedTail[0] = 2;
	const equippedView = new DataView( equippedTail.buffer );
	[ 10, 20, 30, 40, 5, 6 ].forEach( ( value, i ) => equippedView.setUint32( 10 + i * 4, value, true ) );
	equippedView.setUint16( 34, 80, true );
	training.receive( { opcode: OP_PET_TRAINING_STATE, payload: Uint8Array.from( [ ...base, ...equippedTail ] ) }, 1 );
	const equipped = training.state();
	assert.equal( equipped?.stats?.cooldownPercent, 80 );
	assert.deepEqual( petAbilities( ability, equipped ).magical, [ 216, 396 ] );
	assert.equal( petReference( reference, equipped )?.physicalDefence, 133 );
	assert.equal( petReference( reference, equipped )?.hit, 105 );
	assert.equal( petReference( reference, equipped )?.parry, 106 );
	equippedView.setUint16( 34, 81, true );
	assert.throws( () =>
		training.receive(
			{ opcode: OP_PET_TRAINING_STATE, payload: Uint8Array.from( [ ...base, ...equippedTail ] ) },
			2
		)
	);
	assert.throws( () =>
		training.receive( { opcode: OP_PET_TRAINING_STATE, payload: Uint8Array.from( [ ...base, ...tail, 0 ] ) }, 0 )
	);
});

/*
================
advanced book descriptions
================
*/
test("advanced tooltips include inherited proficiency in the current effect", () => {
	const mend = petSkillDesign( 47 ), mark = petSkillDesign( 50 ), howl = petSkillDesign( 2 );
	assert.ok( mend && mark && howl );
	assert.match( petEffect( mend, 20, 1 ), /30% of maximum HP immediately/ );
	assert.match( petEffect( mark, 20, 1 ), /40% for 8 s/ );
	assert.match( petEffect( howl, 5, 1 ), /9 m for 4 s/ );
	const paw = petSkillDesign( 30 ), feathers = petSkillDesign( 42 ), murder = petSkillDesign( 60 );
	assert.ok( paw && feathers && murder );
	assert.match( petEffect( paw, 2, 1 ), /Every 5th damaging attack/ );
	assert.match( petEffect( paw, 2, 1 ), /5 targets within 3 m/ );
	assert.match( petEffect( feathers, 10, 1 ), /up to 5 targets/ );
	assert.match( petEffect( murder, 200, 1 ), /Every 8th damaging attack/ );
	assert.match( petEffect( murder, 200, 1 ), /2 s Myopia/ );
});
