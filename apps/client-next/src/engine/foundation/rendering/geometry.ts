/*
===========================================================================

geometry.ts - validation and ownership copies of renderer geometry

validateGeometry bounds every stream; copyGeometry takes the private copy
the renderer owns, handling every Geometry field explicitly.

===========================================================================
*/
import { validTextureStage } from "./texture-stage";
import { validBlend } from "./blend-state";
import { validateMaterialTimeline } from "./material-timeline";
import { validateTextureAtlas } from "./texture-atlas";
import type { Geometry } from "@/engine/contracts/geometry";
import type { WorldMaterial } from "@/engine/contracts/scene";
import { finiteGeometryValues, geometryIndicesInRange } from "./geometry-validation";

export function copyMaterial( material: WorldMaterial ): WorldMaterial {
	if (
		material.alphaCompare !== undefined &&
			(!Number.isInteger( material.alphaCompare ) || material.alphaCompare < 1 || material.alphaCompare > 8) ||
		material.textureAlphaSquared !== undefined && typeof material.textureAlphaSquared !== "boolean"
	) throw Error( "Invalid native alpha state" );
	if ( material.deferredParticle !== undefined && typeof material.deferredParticle !== "boolean" ) {
		throw Error( "Invalid deferred material" );
	}
	if ( material.uvAtlas ) validateTextureAtlas( material.uvAtlas );
	if ( material.colorTimeline ) validateMaterialTimeline( material.colorTimeline );
	if (
		material.surfaceAlpha !== undefined && typeof material.surfaceAlpha !== "boolean" ||
		material.depthWrite !== undefined && typeof material.depthWrite !== "boolean" ||
		material.uvVelocity && (material.uvVelocity.length !== 6 || !material.uvVelocity.every( Number.isFinite ))
	) throw new Error( "Invalid material modifier" );
	if (
		material.color.length !== 4 || !material.color.every( Number.isFinite ) ||
		!Number.isFinite( material.alphaCutoff ) || typeof material.blend !== "boolean" ||
		typeof material.doubleSided !== "boolean" ||
		material.texture !== undefined && (typeof material.texture !== "string" || !material.texture) ||
		material.ambient !== undefined &&
			(material.ambient.length !== 3 || !material.ambient.every( Number.isFinite )) ||
		material.objectLight !== undefined &&
			(!Number.isFinite( material.objectLight ) || material.objectLight < 0 || material.objectLight > 1) ||
		material.stageFactor !== undefined && !Number.isFinite( material.stageFactor ) ||
		material.textureStage !== undefined && !validTextureStage( material.textureStage ) ||
		material.order !== undefined && !Number.isFinite( material.order ) ||
		material.fogDisabled !== undefined && typeof material.fogDisabled !== "boolean" ||
		material.textureAlpha !== undefined && typeof material.textureAlpha !== "boolean" ||
		material.instanceFade !== undefined && typeof material.instanceFade !== "boolean" ||
		material.objectFade !== undefined && typeof material.objectFade !== "boolean" ||
		material.fadeAlphaOnly !== undefined && typeof material.fadeAlphaOnly !== "boolean" ||
		material.decal !== undefined && typeof material.decal !== "boolean" ||
		material.groundDecal !== undefined && typeof material.groundDecal !== "boolean" ||
		material.blendPair !== undefined && !validBlend( material.blendPair ) ||
		material.shaderDiffuse !== undefined && typeof material.shaderDiffuse !== "boolean" ||
		material.textureFactorPulse !== undefined &&
			(![ material.textureFactorPulse.low, material.textureFactorPulse.high ].every( v =>
				Number.isInteger( v ) && v >= 0 && v <= 255
			) || !Number.isFinite( material.textureFactorPulse.rate )) ||
		material.textureFactor !== undefined &&
			(material.textureFactor.length !== 4 ||
				!material.textureFactor.every( v => Number.isFinite( v ) && v >= 0 && v <= 1 )) ||
		material.unlit !== undefined && typeof material.unlit !== "boolean" ||
		material.terrain !== undefined && typeof material.terrain !== "boolean" ||
		material.sharedPose !== undefined && typeof material.sharedPose !== "boolean" ||
		material.sky !== undefined && (!Number.isInteger( material.sky ) || material.sky < 1 || material.sky > 6) ||
		material.lightmap !== undefined && typeof material.lightmap !== "boolean" ||
		material.water !== undefined && typeof material.water !== "boolean"
	) {
		throw new Error( "Invalid geometry material" );
	}
	if (
		material.fog &&
		(!Number.isInteger( material.fog.color ) || material.fog.color < 0 || material.fog.color > 0xffffffff ||
			![ material.fog.nearPlane, material.fog.farPlane, material.fog.intensity ].every( Number.isFinite ) ||
			material.fog.nearPlane >= material.fog.farPlane || material.fog.intensity < 0)
	) throw Error( "Invalid material fog" );
	if (
		material.frames !== undefined &&
		(!Array.isArray( material.frames ) || !material.frames.length || material.frames.length > 256 ||
			material.frames[0] !== material.texture ||
			material.frames.some( path => typeof path !== "string" || !path ))
	) {
		throw new Error( "Invalid material animation frames" );
	}
	return {
		...material,
		...(material.colorTimeline ? { colorTimeline: structuredClone( material.colorTimeline ) } : {}),
		...(material.uvAtlas ? { uvAtlas: { ...material.uvAtlas } } : {}),
		...(material.uvVelocity ?
			{ uvVelocity: [ ...material.uvVelocity ] as [number, number, number, number, number, number] } :
			{}),
		...(material.ambient ? { ambient: [ ...material.ambient ] as [number, number, number] } : {}),
		...(material.blendPair ? { blendPair: { ...material.blendPair } } : {}),
		...(material.textureFactorPulse ? { textureFactorPulse: { ...material.textureFactorPulse } } : {}),
		...(material.textureStage ? { textureStage: { ...material.textureStage } } : {}),
		...(material.textureFactor ?
			{ textureFactor: [ ...material.textureFactor ] as [number, number, number, number] } :
			{}),
		color: [ ...material.color ],
		...(material.fog ? { fog: { ...material.fog } } : {}),
		frames: material.frames?.slice()
	};
}

// Admission completes before the caller replaces any live GPU resource.
export function validateGeometry(
	data: Geometry,
	byteLimit = 64 << 20,
	instanceLimit = 4096
): WorldMaterial | undefined {
	const vertices = data.positions.length / 3;
	if (
		!(data.positions instanceof Float32Array) || !vertices || !Number.isInteger( vertices ) ||
		!(data.indices instanceof Uint32Array) || !data.indices.length || data.indices.length % 3 ||
		!geometryIndicesInRange( data.indices, vertices ) ||
		!(data.transform instanceof Float32Array) || data.transform.length !== 16 ||
		data.world !== undefined && typeof data.world !== "boolean"
	) {
		throw new Error( "Invalid geometry dimensions" );
	}
	let bytes = data.indices.byteLength;
	for (
		const [array, length] of [
			[ data.positions, vertices * 3 ],
			[ data.transform, 16 ],
			[ data.normals, vertices * 3 ],
			[ data.uvs, vertices * 2 ],
			[ data.colors, vertices * 4 ],
			[ data.maskUVs, vertices * 2 ],
			[ data.instances, data.instances?.length ],
			[ data.weights, vertices * 4 ],
			[ data.bones, data.bones?.length ]
		] as const
	) {
		if ( array === undefined ) continue;
		if ( !(array instanceof Float32Array) || array.length !== length || !finiteGeometryValues( array ) ) {
			throw new Error( "Invalid geometry attributes" );
		}
		bytes += array.byteLength;
	}
	if ( data.joints !== undefined || data.weights !== undefined || data.bones !== undefined ) {
		const instances = data.material?.sharedPose ? 1 : Math.max( 1, (data.instances?.length ?? 16) / 16 );
		const bonesPerInstance = (data.bones?.length ?? 0) / 16 / instances;
		if (
			!(data.joints instanceof Uint32Array) || data.joints.length !== vertices * 4 || !data.weights ||
			!data.bones ||
			!Number.isInteger( bonesPerInstance ) || bonesPerInstance < 1 || bonesPerInstance > 512 ||
			!geometryIndicesInRange( data.joints, bonesPerInstance ) || data.weights.some( weight => weight < 0 )
		) {
			throw new Error( "Invalid geometry skin" );
		}
		for ( let i = 0; i < data.weights.length; i += 4 ) {
			if (
				Math.abs( data.weights[i]! + data.weights[i + 1]! + data.weights[i + 2]! + data.weights[i + 3]! - 1 ) >
					0.001
			) {
				throw new Error( "Invalid geometry skin weights" );
			}
		}
		bytes += data.joints.byteLength;
	}
	if (
		bytes > byteLimit ||
		data.instances && (data.instances.length % 16 || data.instances.length > instanceLimit * 16)
	) {
		throw new Error( "Geometry exceeds budget" );
	}
	return data.material ? copyMaterial( data.material ) : undefined;
}

export function copyGeometry( data: Geometry, byteLimit = 64 << 20, instanceLimit = 4096 ): Geometry {
	const material = validateGeometry( data, byteLimit, instanceLimit );
	// A new Geometry field must be explicitly handled here, even when optional.
	const owned = {
		world: data.world,
		material,
		positions: data.positions.slice(),
		indices: data.indices.slice(),
		transform: data.transform.slice(),
		normals: data.normals?.slice(),
		uvs: data.uvs?.slice(),
		colors: data.colors?.slice(),
		maskUVs: data.maskUVs?.slice(),
		instances: data.instances?.slice(),
		joints: data.joints?.slice(),
		weights: data.weights?.slice(),
		bones: data.bones?.slice(),
		dynamicVertices: data.dynamicVertices
	} satisfies Record<keyof Geometry, unknown>;
	return owned;
}
