/*
===========================================================================

fortress-map-hud.ts - the battle aide's fortress map window state

The aide's structure row (5D8930 action 0x32 row 2) sends the 0x71E1
structure list; its 0xB1E1 answer opens CIFFortressMap (754A40 ->
CIFNPCTalk_ForwardFortressContextToControl12D 5D22B0 ->
CGInterface_ShowFortressMapWindow 69EC70), which keeps the aide and the
fortress it was opened for (CIFFortressMap_SetTalkContext 65E560). The
window belongs to the conversation: ending it or changing target closes
the map (5DBE00). This owner keeps the open window, its selected zone,
the map pan, the guard tower type popup and the open confirm box; the UI
draws from it every frame.

===========================================================================
*/
import type { Pose } from "@/engine/contracts/gameplay";
import type { FortressState } from "@/engine/foundation/gameplay/fortress";
import {
	FORTRESS_STRUCTURE_LIST,
	type FortressMapPage,
	type FortressMapZone
} from "@/engine/foundation/gameplay/fortress-map";
import { nativeLayout, type WorldMapLayout, type worldMapFrame } from "@/engine/foundation/ui/world-map";

// A region's side in map units (1920 local units / 10), as world-map.ts.
const MAP_REGION_UNITS = 192;

/*
================
FortressMapQuestion

The open confirm box (CGInterface_ShowConfirmationWithText kinds 0x28
construction, 0x29 upgrade, 0x27 repair), with the zone and the tower type
65CD80 sends when it is accepted.
================
*/
export interface FortressMapQuestion {
	readonly kind: "construct" | "upgrade" | "repair";
	readonly zone: number;
	readonly type: number;
}

/*
================
createFortressMapHud
================
*/
export function createFortressMapHud() {
	let npc: number | null = null, fortress = 0, seen = 0, pending = false, open = false;
	let zone = 0;
	let pan: [number, number] = [ 0, 0 ];
	// The pose the page was centred on when the window opened; the drag moves
	// the page from there (CIFWorldMap keeps its scroll position).
	let center: Pose | null = null;
	let question: FortressMapQuestion | null = null;
	// The guard tower type popup (CheckConfirmWnd 0x65): the zone it asks
	// for and the ticked row, -1 for none.
	let popup: { zone: number; choice: number; } | null = null;
	return {
		/*
		================
		request

		The structure list is in flight; the answer opens the window.
		================
		*/
		request( gid: number, id: number, sequence: number ) {
			this.close();
			npc = gid;
			fortress = id;
			seen = sequence;
			pending = true;
		},
		/*
		================
		observe

		Opens the window on the list's answer and closes it with the
		conversation. A refusal leaves it shut; the worker reports it.
		================
		*/
		observe( state: FortressState | undefined, target: number | undefined, talking: boolean ) {
			if ( npc === null ) return;
			if ( target !== npc || !talking ) {
				this.close();
				return;
			}
			const service = state?.service;
			if ( !pending || !service || state?.serviceSequence === seen ) return;
			seen = state?.serviceSequence ?? 0;
			if ( service.action !== FORTRESS_STRUCTURE_LIST ) return;
			pending = false;
			if ( service.result !== 1 ) {
				npc = null;
				return;
			}
			fortress = service.fortress ?? fortress;
			open = true;
		},
		/*
		================
		close
		================
		*/
		close() {
			npc = null;
			pending = open = false;
			zone = 0;
			pan = [ 0, 0 ];
			center = null;
			question = null;
			popup = null;
		},
		npc: () => open ? npc : null,
		fortress: () => fortress,
		zone: () => zone,
		pan: () => pan as readonly [number, number],
		/*
		================
		anchor

		The page's centre, taken from the first pose drawn after opening.
		================
		*/
		anchor( pose: Pose ) {
			center ??= pose;
			return center;
		},
		question: () => question,
		popup: () => popup,
		/*
		================
		select

		A marker click (CIFWorldMap_OnMouseMessage); the panel follows the
		selection (65E6B0).
		================
		*/
		select( id: number ) {
			zone = id;
		},
		/*
		================
		drag

		579920: the map moves by each drag delta; the frame clamps it.
		================
		*/
		drag( dx: number, dy: number ) {
			pan = [ pan[0] + dx, pan[1] + dy ];
		},
		/*
		================
		clampPan

		Keeps the stored pan where the page still covers the view.
		================
		*/
		clampPan( next: readonly [number, number] ) {
			pan = [ next[0], next[1] ];
		},
		/*
		================
		ask
		================
		*/
		ask( kind: FortressMapQuestion["kind"], type = 0 ) {
			if ( zone ) question = { kind, zone, type };
		},
		/*
		================
		takeQuestion
		================
		*/
		takeQuestion() {
			const asked = question;
			question = null;
			return asked;
		},
		/*
		================
		askType

		65D560 for a rank-1 guard tower: the type popup instead of the box.
		================
		*/
		askType() {
			if ( zone ) popup = { zone, choice: -1 };
		},
		/*
		================
		tick

		The popup's check boxes act as one choice.
		================
		*/
		tick( choice: number ) {
			if ( popup ) popup = { ...popup, choice };
		},
		/*
		================
		takeType

		65D7A0: the chosen type (0 when none is ticked, as +0x840 was reset
		by the status refresh) opens the upgrade box; cancel closes the popup.
		================
		*/
		takeType( accept: boolean ) {
			const asked = popup;
			popup = null;
			if ( accept && asked ) question = { kind: "upgrade", zone: asked.zone, type: Math.max( 0, asked.choice ) };
		}
	};
}

/*
================
fortressMapLayout

The world map layout with the fortress page as its only extra page, so the
map window's page geometry (worldMapFrame, worldMapQuads) draws it.
================
*/
export function fortressMapLayout( page: FortressMapPage ): WorldMapLayout {
	const image = page.image.split( "/" ).pop()!.replace( /^map_/, "" ).replace( /\.ddj$/, "" );
	return {
		overview: nativeLayout().overview,
		extra: [ {
			id: page.id,
			name: page.world,
			image,
			size: page.size,
			texture: page.texture,
			picture: page.picture,
			bounds: page.bounds
		} ]
	};
}

/*
================
fortressMarkerAt

A zone's marker centre in the view: the page projection CIFWorldMap uses
for every overlay (57BE10 offsets the region-local position by the
page's origin and scales it to the drawn size).
================
*/
export function fortressMarkerAt(
	zone: FortressMapZone,
	frame: Pick<
		ReturnType<typeof worldMapFrame>,
		"ox" | "oy" | "left" | "right" | "top" | "bottom" | "width" | "height"
	>
): readonly [number, number] {
	const u = (zone.regionId & 255) * MAP_REGION_UNITS + zone.x / 10,
		v = (zone.regionId >>> 8) * MAP_REGION_UNITS + zone.z / 10;
	return [
		frame.ox + (u - frame.left) / (frame.right - frame.left) * frame.width,
		frame.oy + (frame.top - v) / (frame.top - frame.bottom) * frame.height
	];
}
