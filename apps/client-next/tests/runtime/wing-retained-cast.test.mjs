/*
===========================================================================

wing-retained-cast.test.mjs - a wing dress runs with its owner after a cast

Issue #3564: gameplay.casts keeps a cast after its animation ends (a self
buff for its whole duration, a projectile still flying). The wing auxiliary
once treated any row there as "casting" and held "stand" while the body ran.
This drives the production character presentation with the published wing
dress, so it fails if the auxiliary reads table membership again instead of
castAnimating.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { entity, fixture } from "../helpers/presentation-fixture.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";

// The owner runs (movementMode 3) for this long before the wing is read.
const RUN_SECONDS = 3;
const STEP_SECONDS = .05;

/*
================
published

The published character roster and item presentation rows.
================
*/
function published() {
	const roster = JSON.parse( readFileSync( path.join( publicRoot, "assets/char/roster.json" ), "utf8" ) );
	const items = JSON.parse(
		readFileSync( path.join( publicRoot, "assets/data/missionPresentation.json" ), "utf8" )
	).itemsByRefObjId;
	return { roster, items };
}

/*
================
wingClipWhileRunning

Runs a remote CH_M player wearing a wing dress for RUN_SECONDS with the
given casts in gameplay, and returns the wing auxiliary's published clip.
================
*/
function wingClipWhileRunning( casts ) {
	const { roster, items } = published();
	const wingId = Number(
		Object.keys( roster.dress.avatarAuxiliary ).find( id => roster.dress.equipment[id].bodies.CH_M )
	);
	assert.ok( wingId > 0, "the roster carries a CH_M auxiliary avatar" );
	const f = fixture(
		{},
		2,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		undefined,
		undefined,
		undefined,
		{},
		{
			appearance: {
				items,
				roster: {
					models: [ {
						refObjId: 2,
						codename: "CHAR_CH_MAN_TEST",
						glb: "/assets/2.glb",
						clips: [ "stand", "walk", "run" ]
					} ],
					dress: roster.dress
				}
			}
		},
		{ cameraPitch: 0 }
	);
	const gameplay = { localGid: 1, inventory: [], vitals: [], casts, skills: [], moving: false };
	const entities = [
		entity( 1, { kind: "local-player" } ),
		entity( 2, { kind: "player", equipment: [], avatars: [ { refObjId: wingId } ], moving: true, movementMode: 3 } )
	];
	try {
		let wing;
		for ( let now = STEP_SECONDS; now <= RUN_SECONDS; now += STEP_SECONDS ) {
			f.step( entities, now, gameplay );
			assert.equal( f.presentation.error(), null );
			wing = f.actors.find( a => a.attachment?.gid === 2 && a.model.includes( "/avatar_aux_" ) );
		}
		assert.ok( wing, "the wing auxiliary publishes" );
		return wing.clip;
	} finally {
		f.dispose();
	}
}

test("a wing dress runs with its owner while a finished cast is still in gameplay.casts", () => {
	// Skill 7 has no action stages in this fixture, so the retained cast
	// plays no action layer: the body runs, and so must the wings.
	const retained = [ { token: 5, caster: 2, target: 0, skill: 7, damage: 0, fatal: false, receivedAtMs: 0 } ];
	assert.equal( wingClipWhileRunning( [] ), "run", "the wing runs with no cast" );
	assert.equal( wingClipWhileRunning( retained ), "run", "a retained cast does not freeze the wing" );
});
