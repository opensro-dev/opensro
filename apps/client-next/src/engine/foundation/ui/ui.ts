/*
===========================================================================

ui.ts - renderer admission of the published UI command product

Validates and copies each published UiScene into renderer-owned commands,
retaining the previous product's commands where they compare equal by
value, and projects portraits, equipment dolls and character anchors out
of it. Text runs are validated once per run object and kept by reference.

===========================================================================
*/

import { sameUiQuad } from "./ui-equality";
import type { UiScene, UiQuad, UiRect, UiTextRun } from "@/engine/contracts/ui";
import { uiRecordCount, validTextRun, UI_RECORD_LIMIT } from "@/engine/foundation/rendering/text-run";
/*
================
copyUi

Renderer admission copies the immutable command product; caller mutation cannot
alter retained commands. Invalid replacements leave the previous product intact.
================
*/
export function copyUi( scene: UiScene ): UiScene {
	return copyUiProduct( scene, undefined, new WeakSet() );
}
/*
================
copyUiProduct

Text runs are deeply frozen and validated once per run object (the owner's
set), then retained by reference: a run cannot change after admission.
================
*/
function copyUiProduct( scene: UiScene, previous: UiScene | undefined, validated: WeakSet<UiTextRun> ): UiScene {
	if (
		!Number.isSafeInteger( scene.revision ) || !Number.isFinite( scene.width ) ||
		!Number.isFinite( scene.height ) || scene.width <= 0 || scene.height <= 0 || scene.width > 32768 ||
		scene.height > 32768 || scene.quads.length > 8192 || uiRecordCount( scene.quads ) > UI_RECORD_LIMIT
	) throw new Error( "Invalid UI scene" );
	/*
	================
	vector
	================
	*/
	function vector( value: readonly number[] ): UiRect {
		if ( value.length !== 4 || !value.every( Number.isFinite ) ) throw new Error( "Invalid UI vector" );
		return [ value[0]!, value[1]!, value[2]!, value[3]! ];
	}
	const quads: UiQuad[] = scene.quads.map( ( quad, index ) => {
		const retained = previous?.quads[index];
		if ( retained && sameUiQuad( retained, quad ) ) return retained;
		if ( typeof quad.texture !== "string" || quad.texture.length > 1024 ) {
			throw new Error( "Invalid UI texture reference" );
		}
		const rect = vector( quad.rect ),
			clip = vector( quad.clip ),
			color = vector( quad.color ),
			rightColor = quad.rightColor === undefined ? undefined : vector( quad.rightColor ),
			uv = vector( quad.uv );
		if (
			rect[2] < 0 || rect[3] < 0 || clip[2] < 0 || clip[3] < 0 || color.some( v => v < 0 || v > 1 ) ||
			rightColor?.some( v => v < 0 || v > 1 )
		) throw new Error( "Invalid UI extent or color" );
		if ( quad.layer !== undefined && quad.layer !== "background" ) throw Error( "Invalid UI layer" );
		if ( quad.portraitGid !== undefined && (!Number.isSafeInteger( quad.portraitGid ) || quad.portraitGid <= 0) ) {
			throw Error( "Invalid portrait identity" );
		}
		if (
			quad.doll &&
			(!Number.isSafeInteger( quad.doll.gid ) || quad.doll.gid <= 0 || !Number.isFinite( quad.doll.yaw ))
		) throw Error( "Invalid equipment doll" );
		if ( quad.occlusion !== undefined && quad.occlusion !== "scene" && quad.occlusion !== "none" ) {
			throw Error( "Invalid UI occlusion" );
		}
		if ( quad.depth !== undefined && (!Number.isFinite( quad.depth ) || quad.depth < 0 || quad.depth > 1) ) {
			throw Error( "Invalid projected UI depth" );
		}
		if ( quad.rotation !== undefined && !Number.isFinite( quad.rotation ) ) throw Error( "Invalid UI rotation" );
		if (
			quad.characterAnchor !== undefined &&
			(!Number.isSafeInteger( quad.characterAnchor ) || quad.characterAnchor <= 0)
		) throw Error( "Invalid character anchor" );
		if ( quad.sampling !== undefined && quad.sampling !== "linear" && quad.sampling !== "nearest" ) {
			throw Error( "Invalid UI sampling" );
		}
		if (
			quad.uvTurn !== undefined && ![ 0, 1, 2, 3 ].includes( quad.uvTurn ) ||
			quad.alphaCutoff !== undefined &&
				(!Number.isFinite( quad.alphaCutoff ) || quad.alphaCutoff < 0 || quad.alphaCutoff > 1)
		) throw Error( "Invalid UI sampling" );
		let worldAnchor: UiQuad["worldAnchor"];
		if ( quad.worldAnchor ) {
			const a = quad.worldAnchor;
			if (
				!Number.isInteger( a.regionId ) || a.regionId < 0 || a.regionId > 65535 ||
				![ a.x, a.y, a.z ].every( Number.isFinite )
			) throw Error( "Invalid world anchor" );
			worldAnchor = { regionId: a.regionId, x: a.x, y: a.y, z: a.z };
		}
		let run: UiTextRun | undefined;
		if ( quad.run ) {
			if ( !validated.has( quad.run ) ) {
				if ( !validTextRun( quad.run ) ) throw Error( "Invalid UI text run" );
				validated.add( quad.run );
			}
			run = quad.run;
		}
		let mask: UiQuad["mask"];
		if ( quad.mask ) {
			const bounds = vector( quad.mask.rect );
			if (
				bounds[2] <= 0 || bounds[3] <= 0 || typeof quad.mask.texture !== "string" || !quad.mask.texture ||
				quad.mask.texture.length > 1024
			) throw Error( "Invalid UI mask" );
			mask = { rect: bounds, texture: quad.mask.texture };
		}
		return {
			...(quad.occlusion !== undefined ? { occlusion: quad.occlusion } : {}),
			rect,
			clip,
			color,
			...(rightColor ? { rightColor } : {}),
			uv,
			texture: quad.texture,
			...(quad.sampling !== undefined ? { sampling: quad.sampling } : {}),
			...(quad.depth !== undefined ? { depth: quad.depth } : {}),
			...(quad.doll ? { doll: { gid: quad.doll.gid, yaw: quad.doll.yaw } } : {}),
			...(worldAnchor ? { worldAnchor } : {}),
			...(quad.portraitGid !== undefined ? { portraitGid: quad.portraitGid } : {}),
			...(quad.rotation !== undefined ? { rotation: quad.rotation } : {}),
			...(quad.uvTurn !== undefined ? { uvTurn: quad.uvTurn } : {}),
			...(quad.alphaCutoff !== undefined ? { alphaCutoff: quad.alphaCutoff } : {}),
			...(quad.characterAnchor !== undefined ? { characterAnchor: quad.characterAnchor } : {}),
			...(quad.layer ? { layer: quad.layer } : {}),
			...(mask ? { mask } : {}),
			...(run ? { run } : {})
		};
	} );
	return {
		revision: scene.revision,
		width: scene.width,
		height: scene.height,
		quads,
		...(scene.damageText ? { damageText: true } : {})
	};
}

/*
================
prepareUi

These memberships change only when a new UI product is admitted. Projection
still samples current character poses and camera matrices on every frame.
================
*/
export function prepareUi( scene: UiScene ) {
	return prepareCopiedUi( copyUi( scene ) );
}
/*
================
prepareCopiedUi
================
*/
function prepareCopiedUi( copied: UiScene ) {
	const anchors = new Set<number>(), portraits: number[] = [];
	let worldAnchors = false, portraitGid: number | undefined, doll: UiQuad["doll"];
	for ( const quad of copied.quads ) {
		if ( quad.characterAnchor !== undefined ) anchors.add( quad.characterAnchor );
		if ( quad.worldAnchor ) worldAnchors = true;
		if ( !doll && quad.doll ) doll = quad.doll;
		if ( quad.portraitGid !== undefined && !portraits.includes( quad.portraitGid ) ) {
			portraits.push( quad.portraitGid );
		}
		if ( portraitGid === undefined && quad.portraitGid !== undefined ) portraitGid = quad.portraitGid;
	}
	if ( portraits.length > 8 ) throw Error( "Portrait capacity exceeded" );
	const projected = {
		...copied,
		quads: copied.quads.map( q =>
			q.portraitGid === undefined ?
				q :
				{
					...q,
					texture: portraits.indexOf( q.portraitGid ) === 0 ?
						"__portrait" :
						"__portrait" + portraits.indexOf( q.portraitGid )
				}
		)
	};
	return { scene: projected, portraits, anchors: anchors as ReadonlySet<number>, worldAnchors, portraitGid, doll };
}

/*
================
createUiPreparation

One renderer owns one preceding validated product. Equal commands retain their
owned storage; dynamic commands are copied and validated. Comparison is by
value, never caller identity. A failed admission cannot advance this cache.
Resize disables reuse. Null/disposal clears it; world replacement drops absent
commands and compares remaining values. Texture completion
changes command values or texture resources, not validated command ownership;
device recovery keeps CPU commands and recreates GPU resources independently.
================
*/
export function createUiPreparation() {
	let previous: UiScene | undefined;
	const validated = new WeakSet<UiTextRun>();
	return {
		/*
		================
		prepare
		================
		*/
		prepare( scene: UiScene | null ) {
			if ( !scene ) {
				previous = undefined;
				return null;
			}
			const copied = copyUiProduct(
				scene,
				previous?.width === scene.width && previous.height === scene.height ? previous : undefined,
				validated
			);
			const product = prepareCopiedUi( copied );
			previous = copied;
			return product;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			previous = undefined;
		}
	};
}
