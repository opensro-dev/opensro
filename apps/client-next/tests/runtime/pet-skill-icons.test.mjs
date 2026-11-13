/*
===========================================================================
pet-skill-icons.test.mjs - Fox books reuse their matching Wolf family art.
===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
const { PET_DESIGN } = await import( "../../src/engine/foundation/ui/pet-design-data.ts" );
const { petSkillIcon, petSkillIconUV } = await import( "../../src/engine/foundation/ui/pet-skill-icons.ts" );

test("every Fox skill book uses its matching Wolf icon and bounded atlas cell", () => {
	const fox = PET_DESIGN.treeSkills.filter( skill => skill.pet === "Fox" );
	assert.equal( fox.length, 15 );
	for ( const skill of fox ) {
		const wolf = PET_DESIGN.treeSkills.find( candidate =>
			candidate.pet === "Wolf" && candidate.family === skill.family && candidate.option === skill.option
		);
		assert.ok( wolf );
		assert.equal( petSkillIcon( skill ), petSkillIcon( wolf ) );
		assert.deepEqual( petSkillIconUV( skill ), petSkillIconUV( wolf ) );
		const [u, v, width, height] = petSkillIconUV( skill );
		assert.ok( u >= 0 && v >= 0 && u + width <= 1 && v + height <= 1 );
	}
});
