/*
===========================================================================

character-pick-volume.ts - the box a character is picked by, and its bind bounds

Native picking (CModelInstance_IntersectRayAggregateBox 8E7B80, reached from
the dock 73A220 and world 856540 picks) tests the compound's aggregate box at
+0xB0, never the animated triangles or their texture alpha. That box is the
base resource's authored box1 (CResObject +0x280): CCompound_AttachResource-
WithMaterialSet (A9E310) unions later attachments only when part 1 is not
kind 0, so a character body's box stands alone under its equipment and
weapons.

The bind-pose vertex bounds remain for the consumers that size things from
the visible body (label anchors, shadow projection).

===========================================================================
*/
import type { CharacterModel } from "@/engine/contracts/character";
import { createCharacterPose } from "./animation-pose";
import { geometryPickBounds, geometryVertex, type PickBounds } from "@/engine/foundation/rendering/picking";
import { identity } from "@/engine/foundation/rendering/world-math";

/*
================
characterPickVolume

The authored aggregate box. A model with no resource behind it (a test or
synthesized model) has no authored box and is picked by its bind bounds.
================
*/
export function characterPickVolume( model: CharacterModel ): PickBounds {
	return model.aggregateBox ?? characterBindBounds( model );
}

/*
================
characterBindBounds

Bounds of every non-emitter primitive in the clip-less rest pose, in model
space. Build once per admitted assembly.
================
*/
export function characterBindBounds( model: CharacterModel ): PickBounds {
	const pose = createCharacterPose( model );
	pose.evaluate( "", 0 );
	const bounds = [ Infinity, Infinity, Infinity, -Infinity, -Infinity, -Infinity ], matrix = identity();
	for ( const primitive of model.primitives ) {
		if ( primitive.emission ) continue;
		const palette = new Float32Array( primitive.joints.length * 16 );
		pose.palette( primitive, palette );
		const points = new Float32Array( primitive.geometry.positions.length );
		for ( let i = 0; i < points.length / 3; i++ ) {
			points.set( geometryVertex( primitive.geometry, matrix, i, palette.length ? palette : undefined ), i * 3 );
		}
		const b = geometryPickBounds( points );
		for ( let i = 0; i < 3; i++ ) {
			bounds[i] = Math.min( bounds[i]!, b[i]! );
			bounds[i + 3] = Math.max( bounds[i + 3]!, b[i + 3]! );
		}
	}
	return [ bounds[0]!, bounds[1]!, bounds[2]!, bounds[3]!, bounds[4]!, bounds[5]! ];
}
