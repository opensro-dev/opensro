/*
===========================================================================
crowEgg.mjs - original port-only Stormbrood egg, published as a COS resource.
===========================================================================
*/
import { avatarToGlb } from "./exportGlb.mjs";
import { petLootAtlas } from "./petLootAtlas.mjs";
import { resourceGlbOutput } from "./resourceGlbOutput.mjs";
import { writeIntoPublicTreeSync } from "../shared/publicWrite.mjs";

export const CROW_EGG_REF = 5000001;
export const CROW_EGG_BSR = "res/port/stormbrood/egg.bsr";

/*
================
createCrowEgg

A tapered, faceted shell, grounded at y=0. Uses the owned muted brass atlas
patch for mottling, rather than another licensed model or texture.
================
*/
export function createCrowEgg() {
	const positions = [], normals = [], uvs = [], indices = [];
	const rings = 12, segments = 20, height = 9;
	for ( let ring = 0; ring <= rings; ring++ ) {
		const angle = ring * Math.PI / rings;
		const y = (1 - Math.cos( angle )) * height / 2;
		const radius = Math.sin( angle ) * 3 * (1 - .18 * y / height);
		for ( let n = 0; n <= segments; n++ ) {
			const a = n * Math.PI * 2 / segments;
			positions.push( Math.cos( a ) * radius, y, Math.sin( a ) * radius );
			normals.push( Math.cos( a ) * Math.sin( angle ), -Math.cos( angle ), Math.sin( a ) * Math.sin( angle ) );
			uvs.push( 1 / 3 + (n / segments) * .3, .02 + ring / rings * .45 );
			if ( ring < rings && n < segments ) {
				const i = ring * (segments + 1) + n, j = i + segments + 1;
				if ( ring > 0 ) indices.push( i, j, i + 1 );
				if ( ring < rings - 1 ) indices.push( i + 1, j, j + 1 );
			}
		}
	}
	return avatarToGlb( {
		name: "Stormbrood egg",
		skeleton: { bones: [ { name: "root", parentIndex: -1, local: { q: [ 0, 0, 0, 1 ], t: [ 0, 0, 0 ] } } ] },
		parts: [ {
			mesh: {
				materialName: "shell",
				rigid: true,
				positions,
				normals,
				uvs,
				indices,
				vertexCount: positions.length / 3,
				triangleCount: indices.length / 3
			}
		} ],
		materials: new Map( [ [ "shell", {
			pngBytes: petLootAtlas(),
			flags: 0x140,
			colors: { diffuse: [ .588, .588, .588, 1 ], ambient: [ .588, .588, .588, 1 ] }
		} ] ] ),
		clips: []
	} );
}

/*
================
publishCrowEgg
================
*/
export function publishCrowEgg( publicAssetsRoot ) {
	const output = resourceGlbOutput( CROW_EGG_BSR, { namespace: "npc", publicAssetsRoot } );
	const glb = createCrowEgg();
	writeIntoPublicTreeSync( output.diskPath, glb );
	return {
		codename: "PORT_STORMBROOD_EGG",
		refObjId: CROW_EGG_REF,
		kind: "cos",
		bsr: CROW_EGG_BSR,
		glb: output.publicPath,
		bytes: glb.length,
		bones: 1,
		materials: 1,
		clips: [],
		allowedClips: [],
		staticPose: true
	};
}
