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
import { defined } from "../helpers/defined.mjs";
const { createPetTraining, OP_PET_TRAINING_STATE, OP_PET_SKILL_COUNTERS } = await import(
	"../../src/engine/runtime/simulation/worker/session/world/gameplay/pet-training/pet-training.ts"
);
const {
	petSkillHelp,
	petStateAsk,
	petSkillTooltip,
	petSkillDesign,
	petReference,
	petAbilities,
	petAffixLine,
	petRows,
	petEffect,
	petRoarRadius,
	petCooldownQuads,
	petRowCooldown,
	PET_AFFIX_PARAM_BASE,
	PET_GEAR_SLOTS
} = await import(
	"../../src/engine/foundation/ui/pet-training.ts"
);
const { PET_DESIGN } = await import( "../../src/engine/foundation/ui/pet-design-data.ts" );
const { cosHpText, cosStatusRatios } = await import( "../../src/engine/foundation/ui/cos-command.ts" );
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
	assert.match( petEffect( design, 3.5 ), /Attack speed \+3.5%/ );
	const guardian = defined(
		PET_DESIGN.treeSkills.find( entry => entry.family === "Guardian's Bond" && entry.option === 1 )
	);
	assert.match( petEffect( guardian, 0, 1 ), /10% HP/ );
	assert.match( petEffect( guardian, 0, 10 ), /100% HP/ );
	assert.match( petEffect( guardian, 0, 1 ), /600 s/ );
	assert.match( petEffect( guardian, 0, 10 ), /300 s/ );
	for ( const [option, low, high] of [ [ 2, 10, 20 ], [ 3, 20, 30 ] ] ) {
		const book = defined(
			PET_DESIGN.treeSkills.find( entry => entry.family === "Guardian's Bond" && entry.option === option )
		);
		assert.match( petEffect( book, 0, 1 ), new RegExp( `Transfers ${low}%` ) );
		assert.match( petEffect( book, 0, 10 ), new RegExp( `Transfers ${high}%` ) );
	}
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
	assert.ok( help.startsWith( wolf.name + "\nActive\nEffect\n" ) );
	assert.doesNotMatch( help, /Tier|Proficiency|Now:|Click|Level 1/ );
	for ( const [level, points] of [ [ 12, 1 ], [ 15, 0 ], [ 15, 1 ] ] ) {
		const tooltip = petSkillTooltip( wolf, skill, level, points );
		assert.equal(
			tooltip.find( row => row.value === "Pet level 15" )?.color,
			level < 15 ? 0xffff5555 : 0xffffffff
		);
		assert.equal(
			tooltip.find( row => row.value === "Skill points 1" )?.color,
			points < 1 ? 0xffff5555 : 0xffffffff
		);
		assert.equal( tooltip.find( row => row.value === "Effect" )?.ornament, "diamond" );
	}
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
unasked state push drives the HP display

#1896: under the expansion the server lowers a pet's max (a level-40
wolf: 6858 catalog, 350 server) and pushes the state unasked at
summon, restore, potion and growth. A default client - which never
asks, the Experimental row only gates the ask - must then read a pet
healed to its server max as full, not as 350/6858 (5%).
================
*/
test("a pet at its pushed server max reads 350/350 (100%), not 5% of the catalog max", () => {
	const training = createPetTraining( () => null );
	const tail = new Uint8Array( 10 ), view = new DataView( tail.buffer );
	tail[0] = 1;
	view.setUint32( 1, 350, true );
	view.setUint16( 5, 100, true );
	view.setUint16( 7, 100, true );
	tail[9] = 1;
	training.receive(
		{ opcode: OP_PET_TRAINING_STATE, payload: Uint8Array.from( [ ...statePayload( [], [] ), ...tail ] ) },
		0
	);
	const catalog = {
		maxHp: 6858,
		physicalDefence: 100,
		magicalDefence: 100,
		icon: "",
		rideable: false,
		skills: [],
		hit: 100,
		parry: 100
	};
	const reference = petReference( catalog, training.state() );
	assert.equal( reference?.maxHp, 350 );
	const pet = {
		gid: 9,
		refObjId: 100,
		band: 3,
		hp: 350,
		mp: 0,
		status: 0,
		dead: false,
		satiety: 10000,
		commandMode: 1
	};
	assert.equal( cosHpText( pet, reference ), "350/350 (100%)" );
	assert.equal( cosStatusRatios( pet, reference ).hp, 1 );
	assert.equal( cosHpText( pet, catalog ), "350/6858 (5%)" );
});

/*
================
advanced book descriptions
================
*/
test("advanced tooltips include inherited proficiency in the current effect", () => {
	const mend = petSkillDesign( 47 ), mark = petSkillDesign( 50 ), howl = petSkillDesign( 2 );
	assert.ok( mend && mark && howl );
	assert.match( petEffect( mend, 20, 1 ), /20% maximum HP: half immediately, half over 5 seconds/ );
	assert.match( petEffect( mark, 20, 1 ), /hatches after 30 s.*Stores 3 casts/ );
	assert.match( petEffect( howl, 20, 1 ), /Attack speed \+20%.*Wolf and owner/ );
	const paw = petSkillDesign( 30 ), feathers = petSkillDesign( 42 ), murder = petSkillDesign( 60 );
	assert.ok( paw && feathers && murder );
	assert.match( petEffect( paw, 2, 1 ), /Every 10 landed physical or spell hits taken/ );
	assert.match( petEffect( paw, 2, 1 ), /8 m.*50% chance/ );
	assert.match( petEffect( paw, 2, 1 ), /owner has a separate 10-hit counter/ );
	const firstPaw = petSkillDesign( 28 );
	assert.ok( firstPaw );
	assert.doesNotMatch( petEffect( firstPaw, 10, 1 ), /owner has|50% chance/ );
	assert.doesNotMatch(
		petSkillHelp(
			firstPaw,
			{
				id: 28,
				level: 10,
				effectiveLevel: 12,
				nextPetLevel: 0,
				action: false,
				queued: false,
				learnable: false,
				live: true,
				slotted: true,
				value: 2.12,
				nextValue: 2.12
			},
			90,
			15
		),
		/Proficiency/
	);
	assert.match( petEffect( feathers, 10, 1 ), /Maximum stacks 5/ );
	assert.match( petEffect( murder, 200, 1 ), /Crow hits grant/ );
	assert.match( petEffect( murder, 200, 1 ), /Party members within 20 m/ );
});

/*
================
Roar range and cast rules
================
*/
test("Roar I/III gain one metre per effective level and explain cast restrictions", () => {
	assert.equal( petRoarRadius( 1, 1 ), 10 );
	assert.equal( petRoarRadius( 1, 10 ), 19 );
	assert.equal( petRoarRadius( 2, 1 ), 12 );
	assert.equal( petRoarRadius( 3, 1 ), 30 );
	assert.equal( petRoarRadius( 3, 3 ), 32 );
	assert.equal( petRoarRadius( 3, 10 ), 39 );
	const help = petEffect( defined( petSkillDesign( 18 ) ), 0, 1 );
	assert.match( help, /30 m/ );
	assert.match( help, /1 s cast/ );
	assert.match( help, /without targets/ );
	assert.match( help, /Cannot cast in town/ );
});

test("Iron Hide tooltips and shieldGauge scale health + shield like League of Legends", async () => {
	const { shieldGauge } = await import( "../../src/engine/foundation/ui/shield-gauge.ts" );
	const t1 = petEffect( defined( petSkillDesign( 19 ) ), 30, 1 );
	const t2 = petEffect( defined( petSkillDesign( 20 ) ), 55, 1 );
	const t3 = petEffect( defined( petSkillDesign( 21 ) ), 80, 1 );
	assert.match( t1, /Shields the bear and its owner for 30% of maximum HP for 5 s\./ );
	assert.doesNotMatch( t1, /party members/ );
	assert.match( t2, /Also shields party members within 20 m\./ );
	assert.doesNotMatch( t2, /explodes/ );
	assert.match( t3, /explodes within 8 m for 50% of its remaining value/ );
	for ( const id of [ 19, 20, 21 ] ) {
		const design = petSkillDesign( id );
		assert.ok( design );
		assert.equal( design.valueUnit, "% max HP" );
		const fullHelp = petSkillHelp(
			design,
			{
				id,
				level: 1,
				effectiveLevel: 1,
				nextPetLevel: 90,
				action: true,
				queued: false,
				learnable: false,
				live: true,
				slotted: false,
				value: 30,
				nextValue: 32
			},
			90,
			1
		);
		assert.doesNotMatch( fullHelp, /Proficiency:|Planned/ );
	}
	// When HP (50%) + Shield (30%) <= 100%, health stays 0.5 and shield appends 0.3 beside it.
	const partial = shieldGauge(
		{ shield: { amount: 300, maxHp: 1000, atMs: 0, untilMs: 5000, revision: 1, burst: false } },
		0.5,
		1000
	);
	assert.equal( partial.health, 0.5 );
	assert.equal( partial.start, 0.5 );
	assert.equal( partial.shield, 0.3 );
	assert.equal( partial.amount, 300 );
	// When HP (80%) + Shield (45%) > 100%, both compress proportionally to fit the bar.
	const overflow = shieldGauge(
		{ shield: { amount: 450, maxHp: 1000, atMs: 0, untilMs: 5000, revision: 1, burst: false } },
		0.8,
		1000
	);
	assert.ok( Math.abs( overflow.health + overflow.shield - 1 ) < 1e-9 );
	assert.equal( overflow.start, overflow.health );
});

/*
================
Authoritative counters and owned art
================
*/
test("counter pushes are bounded, atomic, pet-specific and do not restart cooldowns", () => {
	const t = createPetTraining( () => null );
	const p = Uint8Array.from( [ 1, 0, 0, 2, 39, 9, 8, 10 ] );
	const frame = { opcode: OP_PET_SKILL_COUNTERS, payload: p };
	t.receive( frame, 1 );
	assert.equal( t.state(), undefined );
	t.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( [], [] ) }, 100 );
	t.receive( frame, 200 );
	assert.deepEqual( t.state()?.counters, { attacks: 39, bearHits: 9, ownerHits: 8, resolve: 10 } );
	assert.equal( t.state()?.receivedAtMs, 100 );
	for ( const [index, invalid] of [ [ 4, 40 ], [ 5, 10 ], [ 6, 10 ], [ 7, 11 ] ] ) {
		const bad = p.slice();
		bad[index] = invalid;
		assert.throws( () => t.receive( { ...frame, payload: bad }, 300 ), /Invalid pet skill counters/ );
	}
	assert.throws( () => t.receive( { ...frame, payload: p.subarray( 0, 7 ) }, 300 ) );
	const previous = t.state();
	t.receive( { ...frame, payload: Uint8Array.from( [ 2, 0, 0, 2, 0, 0, 0, 0 ] ) }, 300 );
	assert.equal( t.state(), previous );
	t.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( [], [] ) }, 400 );
	assert.deepEqual( t.state()?.counters, previous?.counters );
	t.clear();
	t.receive( frame, 500 );
	assert.equal( t.state(), undefined );
});

test("each skill family uses its atlas cell and stack badges reuse native level digits", async () => {
	const { petSkillIcon, petSkillIconUV, petSkillStack, petStackQuads, petRowHelp } = await import(
		"../../src/engine/foundation/ui/pet-skill-icons.ts"
	);
	const state = {
		gid: 1,
		points: 0,
		attackPercent: 100,
		defensePercent: 100,
		rows: [ { timerMs: 0, left: 3, kind: "Attacks" } ],
		slotted: [],
		skills: [],
		gear: [],
		receivedAtMs: 100,
		counters: { attacks: 39, bearHits: 9, ownerHits: 8, resolve: 10 }
	};
	const skill = {
		id: 0,
		level: 10,
		effectiveLevel: 10,
		nextPetLevel: 0,
		action: false,
		queued: false,
		learnable: false,
		live: true,
		slotted: true,
		value: 1,
		nextValue: 1
	};
	const cells = new Set();
	for ( const d of PET_DESIGN.treeSkills ) {
		assert.ok( petSkillIcon( d ).endsWith( d.option === 3 ? "/pet-skills.png" : `/pet-skills-${d.option}.png` ) );
		const uv = petSkillIconUV( d );
		cells.add( uv.join( "," ) );
		assert.ok( uv[0] >= 0 && uv[1] >= 0 && uv[0] + uv[2] <= 1 && uv[1] + uv[3] <= 1 );
		if ( d.family === "Crushing Paw" ) assert.equal( petSkillStack( state, d, skill ), 9 );
		if ( d.family === "Unbroken Spirit" ) assert.equal( petSkillStack( state, d, skill ), 10 );
		if ( d.family === "Guiding Feathers" ) assert.equal( petSkillStack( state, d, skill ), 0 );
		assert.equal( petSkillStack( state, d, { ...skill, level: 0 } ), null );
	}
	assert.equal( cells.size, 20 );
	const r = /** @type {import('../../src/engine/contracts/ui.ts').UiRect} */ ([ 0, 0, 32, 32 ]);
	assert.equal( petStackQuads( 10, r, r ).length, 3 );
	assert.deepEqual( petStackQuads( null, r, r ), [] );
	const d = PET_DESIGN.treeSkills.find( entry => entry.family === "Crushing Paw" && entry.option === 3 );
	assert.ok( d );
	assert.match(
		petRowHelp( { ...state, skills: [ { ...skill, id: d.id } ] }, d.row, 100 ),
		/Bear hits: 9 \/ 10\nOwner hits: 8 \/ 10/
	);
});

/*
================
Locked-book requirements remain visible
================
*/
test("unlearned advanced skills retain effects and red individual requirements", () => {
	const d = defined( petSkillDesign( 24 ) );
	const skill = {
		id: d.id,
		level: 0,
		effectiveLevel: 0,
		nextPetLevel: 90,
		action: false,
		queued: false,
		learnable: false,
		live: true,
		slotted: false,
		value: 3,
		nextValue: 3
	};
	const rows = petSkillTooltip( d, skill, 40, 0, false );
	assert.ok( rows.some( r => r.value === "Maximum HP +3% per stack" ) );
	for ( const value of [ "Pet level 90", "Skill points 1", "Unbroken Spirit II 10" ] ) {
		assert.equal( rows.find( r => r.value === value )?.color, 0xffff5555 );
	}
	assert.ok( rows.every( r => !/^Tier|^Level|^Now:|Proficiency/.test( r.value ) ) );
	assert.match( petSkillHelp( d, skill, 40, 0, false ), /Unbroken Spirit II 10/ );
});

test("petSkillTooltip Next section omits unchanged trailing lines when rank2Level inserts a middle line", () => {
	const executeI = defined( petSkillDesign( 7 ) );
	const rows = petSkillTooltip(
		executeI,
		{
			id: 7,
			level: 4,
			effectiveLevel: 4,
			nextPetLevel: 20,
			action: false,
			queued: false,
			learnable: true,
			live: true,
			slotted: true,
			value: 58,
			nextValue: 66
		},
		20,
		1,
		true
	);
	const nextIdx = rows.findIndex( r => r.value === "Next" && r.heading );
	const reqIdx = rows.findIndex( r => r.value === "Required" && r.heading );
	assert.ok( nextIdx >= 0 && reqIdx > nextIdx );
	const nextLines = rows.slice( nextIdx + 1, reqIdx ).map( r => r.value );
	assert.ok( !nextLines.includes( "Never procs on blocked hits" ) );
});

test("petReference scales maxHp from active Resolve stacks and reverts on expiry", () => {
	const baseRef = {
		icon: "cos/bear.ddj",
		maxHp: 1000,
		rideable: false,
		physicalDefence: 100,
		magicalDefence: 80,
		parry: 20,
		hit: 30,
		skills: []
	};
	// Third-party targeted bear without local PetTraining: Bear hpPercent = 220 -> 2200 base, Tier 3 (3%/stack) * 10 = +30% -> 2860.
	const thirdParty = petReference( baseRef, undefined, { stacks: 10, tier: 3, untilMs: 5000 }, 1000 );
	assert.equal( thirdParty?.maxHp, 2860 );
	assert.equal( petReference( baseRef, undefined, { stacks: 10, tier: 3, untilMs: 5000 }, 5000 )?.maxHp, 1000 );

	// Owned bear with Unbroken Spirit III (value = 4%/stack) and unbuffed training.stats.maxHp = 2200: 10 stacks -> 3080.
	const training = {
		gid: 77,
		points: 0,
		attackPercent: 80,
		defensePercent: 120,
		rows: [],
		slotted: [ 24 ],
		skills: [ {
			id: 24,
			level: 10,
			effectiveLevel: 10,
			nextPetLevel: 0,
			action: false,
			queued: false,
			learnable: false,
			live: true,
			slotted: true,
			value: 4,
			nextValue: 4
		} ],
		gear: [],
		receivedAtMs: 0,
		counters: { attacks: 0, bearHits: 0, ownerHits: 0, resolve: 10 },
		stats: {
			maxHp: 2200,
			magicPercent: 80,
			magicDefensePercent: 110,
			magicPrimary: false,
			gearStats: [ 0, 0, 0, 0, 0, 0 ],
			cooldownPercent: 0
		}
	};
	assert.equal( petReference( baseRef, training, { stacks: 10, tier: 3, untilMs: 5000 }, 1000 )?.maxHp, 3080 );
	// When 0x3FB1 already carries the Resolve-buffed maxHp (3080), do not double-scale, and revert to 2200 after expiry.
	const preScaled = { ...training, stats: { ...training.stats, maxHp: 3080 } };
	assert.equal( petReference( baseRef, preScaled, { stacks: 10, tier: 3, untilMs: 5000 }, 1000 )?.maxHp, 3080 );
	assert.equal( petReference( baseRef, preScaled, { stacks: 10, tier: 3, untilMs: 5000 }, 6000 )?.maxHp, 2200 );
	const balanced = { ...training, stats: { ...training.stats, maxHp: 15400 } };
	assert.equal( petReference( baseRef, balanced, { stacks: 10, tier: 3, untilMs: 5000 }, 1000, 90 )?.maxHp, 15400 );
	assert.equal( petReference( baseRef, balanced, { stacks: 10, tier: 3, untilMs: 5000 }, 6000, 90 )?.maxHp, 11000 );
});

/*
================
Wolf execution progress
================
*/
test("wolf and owner seven-action progress decodes independently, with bounded extension", async () => {
	const t = createPetTraining( () => null );
	t.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( [], [] ) }, 100 );
	const payload = Uint8Array.from( [ 1, 0, 0, 2, 0, 0, 0, 0, 6, 5 ] );
	t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload }, 200 );
	assert.equal( t.state()?.counters?.wolfExecute, 6 );
	assert.equal( t.state()?.counters?.ownerExecute, 5 );
	assert.equal( t.state()?.receivedAtMs, 100 );
	for ( const index of [ 8, 9 ] ) {
		const bad = payload.slice();
		bad[index] = 7;
		assert.throws( () => t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload: bad }, 300 ) );
	}
	assert.throws( () => t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload: payload.subarray( 0, 9 ) }, 300 ) );
	const { petSkillStack, petRowHelp } = await import( "../../src/engine/foundation/ui/pet-skill-icons.ts" );
	const d = defined( petSkillDesign( 9 ) );
	const skill = {
		id: 9,
		level: 1,
		effectiveLevel: 1,
		nextPetLevel: 94,
		live: true,
		action: false,
		queued: false,
		learnable: false,
		slotted: true,
		value: 19,
		nextValue: 19.7
	};
	const state = defined( t.state() );
	assert.equal( petSkillStack( state, d, skill ), 6 );
	assert.match( petRowHelp( { ...state, skills: [ skill ] }, 2, 200 ), /Wolf attacks: 6 \/ 7/ );
	assert.match( petRowHelp( { ...state, skills: [ skill ] }, 2, 200 ), /Highest learned book is always active/ );
	assert.doesNotMatch( petRowHelp( { ...state, skills: [ skill ] }, 2, 200 ), /Click to select/ );
	assert.match( petSkillHelp( d, skill, 90, 0 ), /Sweep range 10 m/ );
});

/*
================
Wolf kit progress and descriptions
================
*/
test("wolf kit stacks decode independently and descriptions match each book", async () => {
	const t = createPetTraining( () => null );
	t.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( [], [] ) }, 100 );
	const payload = Uint8Array.from( [ 1, 0, 0, 2, 0, 0, 0, 0, 6, 5, 10, 8 ] );
	t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload }, 200 );
	assert.equal( t.state()?.counters?.wolfBlood, 10 );
	assert.equal( t.state()?.counters?.ownerBlood, 8 );
	for ( const index of [ 10, 11 ] ) {
		const bad = payload.slice();
		bad[index] = 11;
		assert.throws( () => t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload: bad }, 300 ) );
	}
	assert.throws( () => t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload: payload.subarray( 0, 11 ) }, 300 ) );
	for ( const tier of [ 1, 2, 3 ] ) {
		const howl = defined( petSkillDesign( tier ) );
		const rend = defined( petSkillDesign( 3 + tier ) );
		const blood = defined( petSkillDesign( 9 + tier ) );
		const hunt = defined( petSkillDesign( 12 + tier ) );
		assert.equal( howl.family, "Hunting Howl" );
		assert.match( petEffect( howl, 10 * tier ), /Duration 8 s/ );
		assert.match( petEffect( rend, 100 + 60 * tier ), /Bleeding duration 6 s/ );
		assert.doesNotMatch( petEffect( rend, 200 ), /30% HP|execut/ );
		assert.match( petEffect( blood, 1 ), new RegExp( `Maximum stacks ${[ 5, 8, 10 ][tier - 1]}` ) );
		assert.match( petEffect( hunt, 100 ), /Savage Rend bonus damage \+100%/ );
	}
	const { petSkillStack } = await import( "../../src/engine/foundation/ui/pet-skill-icons.ts" );
	assert.equal(
		petSkillStack( defined( t.state() ), defined( petSkillDesign( 12 ) ), {
			id: 12,
			live: true,
			level: 1,
			slotted: true,
			action: false,
			effectiveLevel: 1,
			nextPetLevel: 94,
			queued: false,
			learnable: false,
			value: 1,
			nextValue: 1.1
		} ),
		10
	);
});

/*
================
Owner HUD charge readiness
================
*/
test("owner passive HUD tracks independent charges, readiness and active books", async () => {
	const { petOwnerStacks } = await import( "../../src/engine/foundation/ui/pet-skill-icons.ts" );
	const { buffBoard } = await import( "../../src/engine/foundation/ui/buff-board.ts" );
	const t = createPetTraining( () => null );
	t.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( [], [] ) }, 100 );
	/** @param {number} id */
	const learned = id => ({
		id,
		live: true,
		level: 1,
		effectiveLevel: 1,
		slotted: true,
		action: false,
		queued: false,
		learnable: false,
		nextPetLevel: 94,
		value: 3,
		nextValue: 3.1
	});
	const state = {
		...defined( t.state() ),
		skills: [ learned( 9 ), learned( 12 ) ],
		counters: {
			attacks: 0,
			bearHits: 0,
			ownerHits: 0,
			resolve: 0,
			wolfExecute: 2,
			ownerExecute: 6,
			wolfBlood: 1,
			ownerBlood: 10
		}
	};
	const icons = petOwnerStacks( state );
	assert.deepEqual( icons.map( i => [ i.count, i.ready ] ), [ [ 6, true ], [ 10, true ] ] );
	assert.match( defined( icons[0] ).helpText, /Next damaging attack/ );
	const game = /** @type {import('../../src/engine/contracts/gameplay.ts').GameplayState} */ ({ localGid: 1 });
	const board = buffBoard( game, 0, new Set(), state );
	assert.equal( board.length, 2 );
	assert.ok( board.every( i => i.ready && i.uv && i.stack !== undefined ) );
	assert.equal( defined( board[1] ).x, 23 );
	const promoted = buffBoard( game, 0, new Set(), { ...state, skills: [ learned( 11 ) ] } );
	assert.equal( defined( promoted[0] ).fraction, 1, "a book change cannot overflow the charge bar" );
	assert.equal(
		buffBoard( { ...game, petTraining: state }, 0 ).length,
		0,
		"native/disabled board remains unchanged"
	);
	const reset = { ...state, counters: { ...state.counters, ownerExecute: 0, ownerBlood: 0 } };
	assert.ok( petOwnerStacks( reset ).every( i => i.count === 0 && !i.ready ) );
	assert.equal(
		petOwnerStacks( { ...state, skills: state.skills.map( s => ({ ...s, slotted: false }) ) } ).length,
		0
	);
	assert.equal( petOwnerStacks( { ...state, counters: undefined } ).length, 0 );
	const bear = { ...state, skills: [ learned( 30 ) ], counters: { ...state.counters, ownerHits: 9 } };
	assert.match( defined( petOwnerStacks( bear )[0] ).helpText, /Next qualifying hit/ );
});
/*
================
Blood Hunt shared prey snapshot
================
*/
test("Blood Hunt decodes target-bound charges and shows Rend readiness", async () => {
	const { petOwnerStacks, petSkillStack } = await import( "../../src/engine/foundation/ui/pet-skill-icons.ts" );
	const t = createPetTraining( () => null );
	t.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( [], [] ) }, 100 );
	const payload = Uint8Array.from( [ 1, 0, 0, 2, 0, 0, 0, 0, 6, 5, 10, 8, 4, 99, 0, 0, 0 ] );
	t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload }, 200 );
	const state = defined( t.state() );
	assert.equal( state.counters?.hunt, 4 );
	assert.equal( state.counters?.huntTarget, 99 );
	const skill = {
		id: 15,
		level: 1,
		effectiveLevel: 1,
		nextPetLevel: 94,
		live: true,
		action: false,
		queued: false,
		learnable: false,
		slotted: true,
		value: 100,
		nextValue: 105.6
	};
	const design = defined( petSkillDesign( 15 ) );
	const marked = { ...state, skills: [ skill ] };
	assert.equal( petSkillStack( marked, design, skill ), 4 );
	const hud = defined( petOwnerStacks( marked )[0] );
	assert.equal( hud.ready, true );
	assert.match( hud.helpText, /Rend the marked enemy/ );
	assert.match( petEffect( design, 100 ), /5 other bleeding enemies within 6 m/ );
	assert.equal(
		petOwnerStacks( { ...marked, skills: [ { ...skill, id: 13 } ] } ).length,
		0,
		"first book is wolf only"
	);
	for (
		const bad of [
			payload.subarray( 0, 16 ),
			Uint8Array.from( [ ...payload.slice( 0, 12 ), 5, 99, 0, 0, 0 ] ),
			Uint8Array.from( [ ...payload.slice( 0, 12 ), 4, 0, 0, 0, 0 ] ),
			Uint8Array.from( [ ...payload.slice( 0, 12 ), 0, 99, 0, 0, 0 ] )
		]
	) {
		assert.throws( () => t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload: bad }, 300 ) );
		assert.equal( t.state()?.counters?.huntTarget, 99, "invalid snapshots cannot partly overwrite state" );
	}
	payload.fill( 0, 12 );
	t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload }, 400 );
	assert.equal( petOwnerStacks( { ...defined( t.state() ), skills: [ skill ] } )[0]?.ready, false );
});

test("the quickslot bar asks again for a pet state lost to an early ask or a world re-entry", () => {
	const wolf = { refObjId: 6123, gid: 7 };
	let { ask, next } = petStateAsk( { ref: 0, asks: 0, atMs: 0 }, wolf, undefined, 1000 );
	assert.equal( ask, true, "a new pet is asked at once" );
	assert.equal( petStateAsk( next, wolf, undefined, 2000 ).ask, false, "retries are spaced out" );
	for ( const at of [ 4000, 7000 ] ) {
		({ ask, next } = petStateAsk( next, wolf, undefined, at ));
		assert.equal( ask, true, `retry at ${at}` );
	}
	assert.equal( petStateAsk( next, wolf, undefined, 60000 ).ask, false, "a silent server is not asked forever" );
	({ ask, next } = petStateAsk( next, wolf, 7, 61000 ));
	assert.equal( ask, false, "a received state is not asked again" );
	assert.equal( petStateAsk( next, wolf, undefined, 70000 ).ask, true, "a cleared state is asked again" );
	assert.equal( petStateAsk( next, { refObjId: 6124, gid: 7 }, 7, 70000 ).ask, true, "another pet on the same GID" );
	assert.deepEqual( petStateAsk( next, undefined, undefined, 70000 ), {
		ask: false,
		next: { ref: 0, asks: 0, atMs: 0 }
	} );
});

test("penguin counters reject overflow atomically and expose ready Hourglass", () => {
	const t = createPetTraining( () => null );
	t.receive( { opcode: OP_PET_TRAINING_STATE, payload: statePayload( [], [] ) }, 100 );
	const packet = new Uint8Array( 20 );
	new DataView( packet.buffer ).setUint32( 0, 0x2000001, true );
	packet.set( [ 4, 3, 5 ], 17 );
	t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload: packet }, 200 );
	assert.equal( t.state()?.counters?.penguinResonance, 4 );
	assert.equal( t.state()?.counters?.hourglass, 5 );
	for ( const offset of [ 17, 18, 19 ] ) {
		const bad = packet.slice();
		bad[offset] = 6;
		assert.throws( () => t.receive( { opcode: OP_PET_SKILL_COUNTERS, payload: bad }, 300 ) );
		assert.equal( t.state()?.counters?.hourglass, 5 );
	}
});

test("individual pet skill blues name the skill rather than a generic skill bonus", () => {
	const affixes = PET_DESIGN.affixes.filter( a => "skillId" in a );
	assert.equal( affixes.length, 20 );
	for ( const a of affixes ) {
		const encoded = (5n << 32n) | BigInt( 0x8000 + a.code );
		assert.equal( petAffixLine( encoded.toString() ), a.effect.replaceAll( "{v}", "5" ) );
		assert.deepEqual( a.slots, [ "Collar", "Harness", "Charm" ] );
	}
});
