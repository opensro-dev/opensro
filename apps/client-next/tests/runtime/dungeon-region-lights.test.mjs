/*
===========================================================================

dungeon-region-lights.test.mjs - every published dungeon region keeps
the authored block lights #2991 wired up, end to end

#3784 reported black item/monster models on the Qin-Shi floor the report
names "Jangan Cave B3" (region 32773, jinsi_floor03). The native source
is SWorld_RenderDungeonPass (0x8B17E0): every visible dungeon entity is
bound the point light of its block nearest the entity
(CRTDunBlock_FindNearestPointLight 0xAD3C30, squared distance, first
strictly smallest, no range gate) through RenderLight_ApplyLight1
(0xA5A7F0). These tests pin the port's half of that chain against the
published assets the client actually streams: the scene carries each
block's authored lights, navigation resolves the report's block, and
the renderer row for an actor standing at the reported pose comes out
lit, not black.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readPublishedAssetBytesSync as assetBytes } from "../../../../scripts/lib/publishedAsset.mjs";

const { createWorldDecoder } = await import(
	"../../src/engine/runtime/assets/worker/world/world.ts"
);
const { createNavigation } = await import(
	"../../src/engine/foundation/navigation/clipper.ts"
);
const { createNavigationResources } = await import( "../../src/engine/runtime/assets/worker/navigation/navigation.ts" );
const { dungeonPointLight } = await import(
	"@/engine/foundation/rendering/dungeon-lights"
);
const { radians } = await import( "@/engine/foundation/math/angles" );
const { fillRowPointLights } = await import(
	"../../src/engine/runtime/renderer/characters/characters.ts"
);

const bytes = p => assetBytes( p, CLIENT_PUBLIC_ROOT );
const DUNGEON_REGIONS = [ 32769, 32770, 32771, 32772, 32773, 32774, 32775, 32777 ];

function decodeRegion( regionId ) {
	return createWorldDecoder().decode(
		bytes( `/assets/world/dungeon/regions/0x${regionId.toString( 16 )}.json` ),
		false,
		{ origin: regionId, part: "all" }
	);
}

test("every dungeon block of every published region authors at least one light", () => {
	for ( const regionId of DUNGEON_REGIONS ) {
		const scene = decodeRegion( regionId );
		assert.ok( scene.dungeonLights?.length, `region ${regionId} publishes no block lights` );
		scene.dungeonLights.forEach( ( lights, block ) => {
			assert.ok( lights.length > 0, `region ${regionId} block ${block} authors no light` );
		} );
	}
});

test("the reported pose binds block 41's nearest light at a visible strength", async () => {
	// BR-261115-2239-66D5 (#3784): region 32773, pose below, nav block 41.
	const pose = { regionId: 32773, x: -6632, y: 723.69, z: -9830, angle: 0 },
		scene = decodeRegion( 32773 ),
		product = await createNavigationResources().resolve(
			bytes( "/assets/world/dungeon/dungeon-resources.json" ),
			32773,
			async p => bytes( p )
		),
		nav = createNavigation();
	nav.install( 32773, product );
	assert.equal( nav.block( pose ), 41, "navigation resolves the reported pose to block 41" );

	const bound = dungeonPointLight( scene.dungeonLights, 41, 32773, pose.x, pose.y, pose.z );
	assert.ok( bound, "block 41 binds a light for the reported pose" );
	// The shader adds the descriptor ambient unattenuated and the diffuse
	// scaled by 1/(constant + linear*distance); at the authored 422-unit
	// distance both terms must stay clearly above black.
	const distance = Math.hypot( bound.pose.x - pose.x, bound.pose.y - pose.y, bound.pose.z - pose.z ),
		falloff = 1 / ((bound.attenuationConstant ?? 0) + bound.attenuation * distance);
	assert.ok( Math.max( ...bound.ambient ) > 0.1, `bound ambient ${bound.ambient} reads black` );
	assert.ok(
		Math.max( ...bound.diffuse.map( c => c * falloff ) ) > 0.1,
		`bound diffuse at falloff ${falloff} reads black`
	);

	// The renderer row an actor at the pose actually uploads.
	const floats = fillRowPointLights(
		{},
		[ {
			gid: 1,
			model: "",
			pose: { regionId: 32773, x: pose.x, y: pose.y, z: pose.z, yaw: radians( 0 ) },
			clip: "",
			time: 0,
			loop: false,
			scale: 1,
			dungeonBlock: 41
		} ],
		0,
		scene.dungeonLights
	);
	assert.equal( floats[3], bound.attenuationConstant );
	assert.ok( floats[4] > 0 && floats[5] > 0 && floats[6] > 0, "row ambient floats are lit" );
	assert.ok( floats[8] > 0 && floats[9] > 0 && floats[10] > 0, "row diffuse floats are lit" );
	assert.ok( Math.abs( floats[7] - bound.attenuation ) < 1e-9, "row carries the linear attenuation" );
});
