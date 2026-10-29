/*
===========================================================================

entityCompoundParticles.test.mjs - effect references read a .cpd entity

The npc manifest carries a compound model once the tomb flamemaster is on
the roster (#2024): res/char/china/shinmu.cpd. The effect builder used to
read every npc model as a BSR and stopped the asset build on it. These
tests read the retail compound through its branches, with and without
the tomb posture that supplies the degree-9 stand-ins.

===========================================================================
*/
import assert from "node:assert/strict";
import { test } from "node:test";
import { collectEntityParticleReferences, readEntityModel } from "../../build/effects/buildEffectPrograms.mjs";

const SHINMU = "res/char/china/shinmu.cpd";
const HAIR_PARTICLES = 1;

/*
================
withTomb

Runs body with SRO_QINSHI_TOMB set to value, restoring it afterwards.
================
*/
function withTomb( value, body ) {
	const previous = process.env.SRO_QINSHI_TOMB;
	process.env.SRO_QINSHI_TOMB = value;
	try {
		return body();
	} finally {
		if ( previous === undefined ) delete process.env.SRO_QINSHI_TOMB;
		else process.env.SRO_QINSHI_TOMB = previous;
	}
}

/*
================
compound entity joins its branches' modifiers
================
*/
test("a compound entity joins its branches' particle modifiers", () => {
	const model = withTomb( "on", () => readEntityModel( SHINMU ) );
	// Only the hwan hair branch authors a particle; the body and the
	// degree-9 stand-ins author none.
	assert.equal( model.particleModifiers.length, HAIR_PARTICLES );
	for ( const modifier of model.particleModifiers ) {
		for ( const entry of modifier.entries ) {
			assert.match( entry.effectPath, /\.efp$/i );
		}
	}
});

/*
================
compound entity without the tomb refuses its absent branches
================
*/
test("a compound entity refuses a branch v1.150 lacks when the tomb is off", () => {
	assert.throws(
		() => withTomb( "off", () => readEntityModel( SHINMU ) ),
		/compound branch absent: res\/item\/china\/man_item\/heavy_10_aa\.bsr/
	);
});

/*
================
npc manifest with a compound collects its references
================
*/
test("the npc manifest's compound row yields references instead of stopping the build", () => {
	const manifests = {
		itemdrop: { models: {} },
		npc: { models: { "char/china/shinmu.cpd": { bsr: SHINMU } } },
		skillfx: { models: {} }
	};
	const references = withTomb( "on", () => collectEntityParticleReferences( ( domain ) => manifests[domain] ) );
	assert.ok( references.length > 0 );
	for ( const reference of references ) {
		assert.equal( reference.domain, "npc" );
		assert.equal( reference.modelPath, SHINMU );
	}
});
