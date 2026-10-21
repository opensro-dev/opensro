import { test } from "node:test";
import assert from "node:assert/strict";
import { collectEntityParticleReferences } from "../../../../scripts/build/effects/buildEffectPrograms.mjs";
import { defined } from "../helpers/defined.mjs";
import { PORT_PET_DROP_MODEL } from "../../../../scripts/build/shared/portItemRows.mjs";

test("BSR dependency census discovers all selectors even when every manifest discarded the particle payload", () => {
	const reads = [],
		modifiers = [
			{ kind: 2, stateId: -1, animationSetName: "ambient", entries: [ { effectPath: "SYSTEM\\GLOW.EFP" } ] },
			{
				kind: 1,
				stateId: 5,
				animationSetName: "default",
				entries: [ { effectPath: "monster/hit.efp" }, { effectPath: "system/glow.efp" } ]
			},
			{ kind: 0, stateId: -1, animationSetName: "appear", entries: [ { effectPath: "monster/appear.efp" } ] }
		];
	const rows = collectEntityParticleReferences(
		domain => ({ models: { a: { bsr: "res/" + domain + ".bsr" }, b: { bsr: "res/" + domain + ".bsr" } } }),
		path => {
			reads.push( path );
			return { particleModifiers: modifiers };
		}
	);
	assert.deepEqual(
		reads,
		[ "res/itemdrop.bsr", "res/npc.bsr", "res/skillfx.bsr" ],
		"each original model is parsed once despite duplicate references"
	);
	assert.equal( rows.length, 9 );
	for ( const domain of [ "itemdrop", "npc", "skillfx" ] ) {
		assert.deepEqual( rows.filter( r => r.domain === domain ).map( r => r.effectPath ), [
			"monster/appear.efp",
			"monster/hit.efp",
			"system/glow.efp"
		] );
		assert.deepEqual(
			defined( rows.find( r => r.domain === domain && r.effectPath === "system/glow.efp" ) ).selectors.map( s =>
				s.kind
			),
			[ 2, 1 ]
		);
	}
});

test("new malformed BSR dependencies fail instead of vanishing from the build", () => {
	assert.throws(
		() =>
			collectEntityParticleReferences(
				() => ({ models: { a: { bsr: "res/a.bsr" } } }),
				() => ({ particleModifiers: [ { entries: [ { effectPath: "not-an-efp.txt" } ] } ] })
			),
		/Invalid itemdrop particle reference/
	);
});

test("the owned pet drop model's borrowed modifiers come from its row, never a retail BSR", () => {
	const reads = [];
	const glow = [ {
		kind: 2,
		stateId: -1,
		animationSetName: "ambient",
		entries: [ { effectPath: "system\\item_drop_equip.efp" } ]
	} ];
	const rows = collectEntityParticleReferences(
		domain =>
			domain === "itemdrop" ?
				{ models: { [PORT_PET_DROP_MODEL]: { bsr: `res/${PORT_PET_DROP_MODEL}`, particleModifiers: glow } } } :
				{ models: {} },
		path => {
			reads.push( path );
			return { particleModifiers: [] };
		}
	);
	assert.deepEqual( reads, [], "no retail file exists for the owned model" );
	assert.deepEqual( rows.map( r => [ r.domain, r.modelPath, r.effectPath ] ), [
		[ "itemdrop", `res/${PORT_PET_DROP_MODEL}`, "system/item_drop_equip.efp" ]
	] );
});
