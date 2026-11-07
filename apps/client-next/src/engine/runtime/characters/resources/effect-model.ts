/*
===========================================================================
effect-model.ts - shared owned effect geometry and native texture admission.
===========================================================================
*/
import type { CharacterModel } from "@/engine/contracts/character";
import type { NativeTexture } from "@/engine/contracts/texture";
/*
================
createIdentityMatrix

Return a fresh 4x4 column-major identity matrix for procedural effect bindings.
================
*/
export function createIdentityMatrix(): Float32Array {
	const out = new Float32Array( 16 );
	out[0] =
		out[5] =
		out[10] =
		out[15] =
			1;
	return out;
}

/*
================
buildProceduralEffectModel

Wrap vertex streams and a procedural BGRA native texture into an admitted CharacterModel.
================
*/
export function buildProceduralEffectModel(
	positions: number[],
	normals: number[],
	uvs: number[],
	indices: number[],
	texture: NativeTexture
): { model: CharacterModel; images: NativeTexture[]; } {
	const vertexCount = positions.length / 3;
	const weights = new Float32Array( vertexCount * 4 );
	for ( let i = 0; i < vertexCount; i++ ) {
		weights[i * 4] = 1;
	}
	return {
		model: {
			nodes: [
				{
					name: "root",
					parent: -1,
					translation: [ 0, 0, 0 ],
					rotation: [ 0, 0, 0, 1 ],
					scale: [ 1, 1, 1 ]
				}
			],
			clips: [ { name: "effect", duration: 1, channels: [] } ],
			images: [ { width: texture.width, height: texture.height } ],
			primitives: [
				{
					name: "effect",
					node: 0,
					joints: [ 0 ],
					inverseBind: createIdentityMatrix(),
					image: 0,
					geometry: {
						positions: Float32Array.from( positions ),
						normals: Float32Array.from( normals ),
						uvs: Float32Array.from( uvs ),
						joints: new Uint32Array( vertexCount * 4 ),
						weights,
						indices: Uint32Array.from( indices ),
						transform: createIdentityMatrix(),
						material: {
							color: [ 1, 1, 1, 1 ],
							alphaCutoff: 0,
							blend: true,
							blendPair: { source: 5, destination: 2 },
							depthWrite: false,
							textureAlpha: true,
							fadeAlphaOnly: true,
							doubleSided: true,
							unlit: true,
							fogDisabled: true
						}
					}
				}
			]
		},
		images: [ texture ]
	};
}
