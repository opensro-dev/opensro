/*
===========================================================================

mall-preview.ts - Item Mall mannequin lifetime and shared equipment assembly

The mannequin borrows character resources and never changes the world actor
or authoritative inventory. Cold replacements keep the last complete outfit.
Native 6BBEA0 clones the player; 6BB0A0 replaces the matching avatar family.

===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";
import type { Renderer } from "@/engine/contracts/runtime";
import type { MallPreviewState } from "@/engine/contracts/item-mall";
import {
	assembleEquipmentAppearance,
	bodyPrefix,
	type AppearanceAssembly,
	type AppearanceResource,
	type DressCatalog,
	type WornItem
} from "@/engine/foundation/animation/equipment-appearance";
import { radians } from "@/engine/foundation/math/angles";

// Above both the unsigned wire GID range and the party portrait range.
export const MALL_PREVIEW_GID = 0x200000000;
const APPEARANCE_EQUIPMENT_SLOTS = 9;

/*
================
MallPreviewFrame
================
*/
interface MallPreviewFrame {
	readonly resource: AppearanceResource & { readonly glb: string; };
	readonly dress: DressCatalog;
	readonly equipment: readonly WornItem[];
	readonly avatars: readonly { readonly refObjId: number; }[];
	readonly seconds: number;
	readonly source?: CharacterActor;
}

/*
================
createMallPreview
================
*/
export function createMallPreview() {
	let request: readonly number[] | null = null;
	let catalog: DressCatalog | undefined;
	let body = "";
	let state: MallPreviewState = { wearable: [] };
	let planned: { signature: string; assembly: AppearanceAssembly; model: string; paths: string[]; } | null = null;
	let committed: { actors: CharacterActor[]; paths: string[]; } | null = null;

	/*
	================
	reset
	================
	*/
	function reset() {
		request = null;
		catalog = undefined;
		body = "";
		state = { wearable: [] };
		planned = null;
		committed = null;
	}

	return {
		/*
		================
		request
		================
		*/
		request( items: readonly number[] | null ) {
			if ( items === null ) {
				if ( request !== null ) reset();
				return;
			}
			request = [ ...items ];
		},
		/*
		================
		step

		Use the same body coverage, default garments and auxiliary sockets as
		world equipment. The render-only identities never enter simulation.
		================
		*/
		step(
			frame: MallPreviewFrame,
			resources: { ready( path: string ): boolean; plan( paths: readonly string[] ): boolean; },
			renderer: Pick<Renderer, "setCharacterAssembly">
		): readonly CharacterActor[] {
			if ( request === null ) return [];
			const prefix = bodyPrefix( frame.resource.codename );
			if ( catalog !== frame.dress || body !== prefix ) {
				const changedBody = body !== "" && body !== prefix;
				catalog = frame.dress;
				body = prefix;
				state = {
					wearable: Object.entries( frame.dress.equipment ?? {} ).filter( ( [, row] ) =>
						row.avatarSlot !== undefined && row.bodies[prefix] !== undefined
					).map( ( [id] ) => Number( id ) )
				};
				// A body change precedes the next UI publication; retire incompatible
				// local choices immediately instead of failing the character frame.
				if ( changedBody ) request = request.filter( id => state.wearable.includes( id ) );
				planned = null;
				committed = null;
			}
			if ( committed ) resources.plan( committed.paths );
			const avatars = new Map<number, { refObjId: number; }>();
			for ( const item of frame.avatars ) {
				const slot = frame.dress.equipment?.[String( item.refObjId )]?.avatarSlot;
				if ( slot === undefined ) throw Error( "Missing mannequin avatar slot " + item.refObjId );
				avatars.set( slot, item );
			}
			for ( const id of request ) {
				if ( !state.wearable.includes( id ) ) throw Error( "Invalid mannequin garment " + id );
				avatars.set( frame.dress.equipment![String( id )]!.avatarSlot!, { refObjId: id } );
			}
			const signature = JSON.stringify( [
				frame.resource.glb,
				frame.equipment.filter( item => item.slot >= 0 && item.slot < APPEARANCE_EQUIPMENT_SLOTS ).map(
					item => [ item.slot, item.refObjId, item.plus ]
				),
				[ ...avatars.values() ]
			] );
			if ( !planned || planned.signature !== signature ) {
				const assembly = assembleEquipmentAppearance( {
					resource: frame.resource,
					dress: frame.dress,
					equipment: frame.equipment,
					avatars: [ ...avatars.values() ],
					hwanHair: false,
					mounted: false,
					weaponHidden: false,
					attachmentsHidden: false,
					fortressIndex: -1,
					player: true,
					ownerless: true,
					committedWear: [],
					freezeWear: false
				} );
				planned = {
					signature,
					assembly,
					model: "mall:" + frame.resource.glb + ":" + JSON.stringify( assembly.parts ),
					paths: [
						frame.resource.glb,
						...assembly.parts.map( part => part.model ),
						...assembly.auxiliary.map( part => part.entry.glb )
					]
				};
			}
			// ready admits every missing source incrementally under the shared budget.
			if ( planned.paths.map( path => resources.ready( path ) ).every( Boolean ) ) {
				renderer.setCharacterAssembly( planned.model, frame.resource.glb, planned.assembly.parts );
				const actor: CharacterActor = {
					gid: MALL_PREVIEW_GID,
					model: planned.model,
					pose: { regionId: 0, x: 0, y: 0, z: 0, yaw: radians( 0 ) },
					clip: "stand",
					previewClip: frame.source?.previewClip,
					bodyVolume: frame.source?.bodyVolume,
					time: frame.seconds,
					loop: true,
					scale: 1,
					pickable: false
				};
				const children: CharacterActor[] = planned.assembly.auxiliary.map( ( { entry }, index ) => ({
					gid: MALL_PREVIEW_GID + index + 1,
					model: entry.glb,
					pose: actor.pose,
					clip: entry.clips.includes( "stand" ) ? "stand" : entry.clips[0]!,
					time: frame.seconds,
					loop: true,
					scale: 1,
					pickable: false,
					attachment: { gid: MALL_PREVIEW_GID, bone: entry.bone, offset: [ 0, 0, 0 ], basis: "compound" }
				}) );
				committed = { actors: [ actor, ...children ], paths: planned.paths };
				if ( state.gid === undefined ) state = { ...state, gid: MALL_PREVIEW_GID };
			}
			return committed?.actors ?? [];
		},
		/*
		================
		state
		================
		*/
		state: () => state,
		reset
	};
}
