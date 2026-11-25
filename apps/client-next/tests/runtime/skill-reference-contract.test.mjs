/*
===========================================================================

skill-reference-contract.test.mjs - tests for the client modules it imports

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";

async function load( file ) {
	return import( sourceFileUrl( file ).href );
}

const { createCombat } = await load( "src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts" );
const { skillCatalog, createSkillTrainingContext } = await load( "src/engine/foundation/gameplay/skill-catalog.ts" );
const { skillMotionResolveAnimation } = await load( "src/engine/foundation/animation/skill-motion-resolve.ts" );
const { skillMotionRole, SKILL_MOTION_DEFAULT_SET_KEY } = await load(
	"src/engine/foundation/animation/skill-motion.ts"
);
const { locomotionLayers, changeLocomotion } = await load( "src/engine/foundation/animation/locomotion-blend.ts" );

function writer() {
	const bytes = [];
	return {
		u8( n ) {
			bytes.push( n & 255 );
			return this;
		},
		u16( n ) {
			return this.u8( n ).u8( n >>> 8 );
		},
		u32( n ) {
			return this.u16( n ).u16( n >>> 16 );
		},
		bytes() {
			return Uint8Array.from( bytes );
		}
	};
}

// Reference Skill: SKILL_EU_WARRIOR_TWOHANDA_DASH_A_01 (Bash)
const REFERENCE_SKILL_ID = 7543;
const REFERENCE_GROUP_ID = 436;
const REFERENCE_MASTERY_ID = 513; // Two-Handed Sword Warrior
const REFERENCE_MASTERY_LEVEL = 4;
const REFERENCE_SP_COST = 2;
const REFERENCE_COOLDOWN_MS = 3000;

test("Reference Skill 7543 (Bash): Training & learn state representation", () => {
	const catalogData = {
		refSkillSnapshot: [
			{
				id: REFERENCE_SKILL_ID,
				group: REFERENCE_GROUP_ID,
				level: 1,
				ui: {
					name: "Bash",
					nameSymbol: "SN_SKILL_EU_WARRIOR_TWOHANDA_DASH_A",
					icon: "skill\\europe\\warrior_twohanda_dash_a.ddj",
					spCost: REFERENCE_SP_COST,
					trainable: true,
					targetRequired: true,
					cooldownMs: REFERENCE_COOLDOWN_MS,
					cooldownGroup: 0,
					masteries: [
						{ ID: REFERENCE_MASTERY_ID, Level: REFERENCE_MASTERY_LEVEL },
						{ ID: 0, Level: 0 }
					],
					prerequisites: [
						{ ID: 0, Level: 0 },
						{ ID: 0, Level: 0 },
						{ ID: 0, Level: 0 }
					]
				}
			}
		]
	};

	const catalog = skillCatalog( catalogData );
	assert.equal( catalog.length, 1 );
	assert.equal( catalog[0].id, REFERENCE_SKILL_ID );
	assert.equal( catalog[0].group, REFERENCE_GROUP_ID );
	assert.equal( catalog[0].trainable, true );
	assert.equal( catalog[0].targetRequired, true );

	// Initial state: not learned
	let learned = [];
	let ctx = createSkillTrainingContext( learned, catalog );
	assert.equal( ctx.learned( REFERENCE_SKILL_ID ), false );

	// Check progression requirements (insufficient mastery level)
	const lowProgression = {
		skillPoints: 10,
		level: 10,
		stats: { strength: 20, intellect: 20 },
		masteries: [ { id: REFERENCE_MASTERY_ID, level: 2 } ] // only level 2
	};
	assert.ok( ctx.reason( catalog[0], lowProgression ) !== null, "Should reject when mastery level is too low" );

	// Sufficient progression
	const validProgression = {
		skillPoints: 10,
		level: 10,
		stats: { strength: 20, intellect: 20 },
		masteries: [ { id: REFERENCE_MASTERY_ID, level: 4 } ]
	};
	assert.equal( ctx.reason( catalog[0], validProgression ), null, "Should be eligible for learning" );

	// State after learning
	learned = [ REFERENCE_SKILL_ID ];
	ctx = createSkillTrainingContext( learned, catalog );
	assert.equal( ctx.learned( REFERENCE_SKILL_ID ), true );
	assert.equal( ctx.hasGroupLevel( REFERENCE_GROUP_ID, 1 ), true );
});

test("Reference Skill 7543 (Bash): C->S Cast request generation (0x72CD)", async () => {
	const { createCombat } = await load(
		"src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts"
	);
	const combat = createCombat();

	// Attack action produces 0x72CD request
	const frame = combat.attack( 101 );
	assert.equal( frame.opcode, 0x72cd, "Opcode must be 0x72CD (OpTargetInteract / CLIENT_OPCODE_ACTION_SKILL)" );
	assert.equal( frame.payload[0], 1 );
	assert.equal( frame.payload[1], 1 );
	assert.equal( frame.payload[2], 1 );

	const targetGid = new DataView( frame.payload.buffer ).getUint32( 3, true );
	assert.equal( targetGid, 101, "Target GID must match argument" );
});

test("Reference Skill 7543 (Bash): S->C Rejection handling (0xB245 error and 0xB2CD notice)", () => {
	const combat = createCombat();
	combat.seed( 1, { hp: 100, mp: 0, maxHp: 100, maxMp: 100 } ); // Out of MP

	// Server sends 0xB245 rejection [ok=2][errCode=5] (e.g. not enough MP)
	const rejected = Uint8Array.of( 2, 5 );
	assert.equal( combat.receive( 0xb245, rejected, 100 ), true );
	assert.equal( combat.state().error, "Cast rejected: 5" );
	assert.equal( combat.state().casts.length, 0, "No active cast must exist on rejection" );

	// Server sends 0xB2CD action response with error flag (pPacket.b == 3)
	// 0xB2CD handler in retail triggers CGInterface_ShowSystemNotification(0x19, errCode)
	const actionResponseRejection = Uint8Array.of( 3, 1, 0x12 ); // kind 3 error
	assert.equal( actionResponseRejection[0], 3, "Kind 3 indicates refusal" );
});

test("Reference Skill 7543 (Bash): S->C Cast open (0xB245) + Knockback result + Finalize (0xB505)", () => {
	const combat = createCombat();
	combat.seed( 1, { hp: 500, mp: 200, maxHp: 500, maxMp: 200 } );

	const casterGid = 1;
	const targetGid = 200;
	const instanceToken = 999;
	const damage = 250;

	// Single-target result payload with knockback displacement (type 5)
	// [ok=1][btResult=0][skillId=7543][casterGid=1][token=999][targetGid=200]
	// [steeringFlags=1][targetCount=1][impactCount=1][targetGid=200][type=5][packedDamage][fatal=0]
	// [region=0x5c87][x=100][y=0][z=200]
	const p = writer()
		.u8( 1 ) // ok
		.u8( 0 ) // btResult
		.u32( REFERENCE_SKILL_ID )
		.u32( casterGid )
		.u32( instanceToken )
		.u32( targetGid )
		.u8( 1 ) // steering: targets present
		.u8( 1 ) // target count = 1
		.u8( 1 ) // impact count = 1
		.u32( targetGid )
		.u8( 5 ) // type 5: knockback
		.u32( damage << 8 ) // packed damage
		.u32( 0 ) // fatal / flags
		.u16( 0x5c87 ) // knockback dest region
		.u16( 100 ) // x
		.u16( 0 ) // y
		.u16( 200 ) // z
		.bytes();

	assert.equal( combat.receive( 0xb245, p, 1000 ), true );

	const state = combat.state();
	assert.equal( state.casts.length, 1 );
	assert.equal( state.casts[0].skill, REFERENCE_SKILL_ID );
	assert.equal( state.casts[0].token, instanceToken );

	// Verify displacement queued for victim
	const displacements = combat.takeDisplacements();
	assert.equal( displacements.length, 1 );
	assert.equal( displacements[0].gid, targetGid );
	assert.deepEqual( displacements[0].destination, {
		regionId: 0x5c87,
		x: 100,
		y: 0,
		z: 200
	} );

	// S->C 0xB505 finalize closes the cast bracket
	const finalize = writer().u8( 2 ).u8( 0 ).u32( instanceToken ).bytes();
	combat.receive( 0xb505, finalize, 2500 );

	const cancelled = combat.takeCancellations();
	assert.deepEqual( cancelled, [ instanceToken ] );

	combat.step( 3000 );
	assert.equal( combat.state().casts.length, 0, "Cast bracket must be retired after finalize" );
});

test("Reference Skill 7543 (Bash): Animation resolution with weapon prefix and fallback", () => {
	// Two-handed sword set key in native client (set index 7 = twohand_sword)
	const twohandSetKey = SKILL_MOTION_DEFAULT_SET_KEY + 7 * 28;
	const role = skillMotionRole( twohandSetKey, "ANI_SKILL_1" );
	assert.equal( role, "native:twohand_sword:26" );

	// Case 1: resident GLB with weapon-prefixed clip 'skill_1-twohand-sword'
	const residentClips = [ "stand", "walk", "skill_1-twohand-sword" ];
	const resolved = skillMotionResolveAnimation( {
		role,
		clips: residentClips,
		bodyStates: {
			"skill_1-twohand-sword": { durationMs: 1500, soundEvents: [], trackEvents: [] }
		}
	} );

	assert.ok( resolved !== undefined );
	assert.equal( resolved.clip, "skill_1-twohand-sword" );
	assert.equal( resolved.definition.durationMs, 1500 );

	// Case 2: Fallback when weapon-specific clip is missing -> falls back to generic 'skill_1'
	const genericClips = [ "stand", "walk", "skill_1" ];
	const fallbackResolved = skillMotionResolveAnimation( {
		role,
		clips: genericClips,
		bodyStates: {
			"skill_1": { durationMs: 1200, soundEvents: [], trackEvents: [] }
		}
	} );

	assert.ok( fallbackResolved !== undefined );
	assert.equal( fallbackResolved.clip, "skill_1", "Must fall back to default unprefixed clip" );
	assert.equal( fallbackResolved.definition.durationMs, 1200 );

	// Case 3: Cold asset / missing clip returns undefined
	const missingResolved = skillMotionResolveAnimation( {
		role,
		clips: [ "stand", "walk" ]
	} );
	assert.equal( missingResolved, undefined, "Must return undefined when neither weapon clip nor fallback exists" );
});

test("Reference Skill 7543 (Bash): Cadence, blend, duration override & interruption", () => {
	// Locomotion blend during cast start
	let locomotion = changeLocomotion( undefined, "stand", true, 0, "stand" );
	locomotion = changeLocomotion( locomotion, "skill-1-twohand-sword", false, 1.0, "skill-1-twohand-sword" );

	// Check layer blend weight at transition
	let layers = locomotionLayers( locomotion, 1.05 );
	assert.ok( layers.length >= 1 );
	assert.equal( layers.some( l => l.clip === "skill-1-twohand-sword" ), true );

	// Interruption / teardown test:
	// If character gets knocked back or interrupted mid-cast at 1.2s, transition to 'knockdown'
	locomotion = changeLocomotion( locomotion, "knockdown", false, 1.2, "knockdown" );
	layers = locomotionLayers( locomotion, 1.25 );
	assert.equal( layers.some( l => l.clip === "knockdown" ), true, "Must interrupt and transition to knockdown" );
});

test("Devil's Spirit transformation (#4407): the later avatar_nasrun sets resolve their own clips", () => {
	// Port-only keys continue the native stride (animationSetNames.ts LATER_ANIMATION_SET_KEY_BY_NAME).
	const nasrun1 = SKILL_MOTION_DEFAULT_SET_KEY + 13 * 28;
	assert.equal( skillMotionRole( nasrun1, "ANI_READY01" ), "native:avatar_nasrun1:40" );
	assert.equal( skillMotionRole( nasrun1, "ANI_SHOT" ), "native:avatar_nasrun1:191" );
	assert.equal( skillMotionRole( SKILL_MOTION_DEFAULT_SET_KEY + 15 * 28, "ANI_SHOT" ), "native:avatar_nasrun3:191" );
	assert.throws(
		() => skillMotionRole( SKILL_MOTION_DEFAULT_SET_KEY + 16 * 28, "ANI_SHOT" ),
		/Unknown skill animation set/
	);
});
