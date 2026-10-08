/*
===========================================================================
mission-loading.ts - shared scene and travel loading artwork

Native destination selection and desktop geometry stay together. Compact
browser presentation contains the illustration above a readable footer.
===========================================================================
*/
import type { UiQuad, UiRect } from "@/engine/contracts/ui";
const DESKTOP_WIDTH = 800;
const DESKTOP_HEIGHT = 600;
const COMPACT_FOOTER = 120;
const COMPACT_MARGIN = 16;
const COMPACT_FRAME_WIDTH = 560;
const COMPACT_FRAME_BOTTOM = 100;
const COMPACT_LABEL_BOTTOM = 64;
const COMPACT_LABEL_WIDTH = 108;
const COMPACT_LABEL_HEIGHT = 15;
const root = "/assets/images/Media_extracted/interface/loading/";
/*
================
missionLoadingAssets
================
*/
export function missionLoadingAssets() {
	return [
		"loading_europe_1",
		"loading_europe_2",
		"loading_form",
		"gauge_loading",
		"nowloading",
		"loading_charactercustom",
		"loading_charactercustom_europe",
		"loading_rebirth",
		"loading_thief2"
	].map( name => root + name + ".png" );
}
/*
================
missionLoadingQuads

CPSMission 7292E0 supplies the authored 1600x1200 rectangles. Desktop keeps
the centered 4:3 background and bottom-centered control group.
================
*/
export function missionLoadingQuads( width: number, height: number, variant: number, progress: number ): UiQuad[] {
	return loadingScreenQuads( width, height, root + `loading_europe_${variant === 2 ? 2 : 1}.png`, progress );
}
/*
================
loadingScreenQuads

Compact footer placement is paired with the native status row in loading.css.
================
*/
export function loadingScreenQuads( width: number, height: number, background: string, progress: number ): UiQuad[] {
	const assets = missionLoadingAssets();
	const full: UiRect = [ 0, 0, width, height ], white = [ 1, 1, 1, 1 ] as const;
	/*
	================
	quad
	================
	*/
	const quad = ( rect: UiRect, texture: string, uv: UiRect = [ 0, 0, 1, 1 ] ): UiQuad => ({
		rect,
		texture,
		uv,
		color: white,
		clip: full
	});
	if ( width < DESKTOP_WIDTH || height < DESKTOP_HEIGHT ) {
		// Port-only, owner-approved. Keep all of the 4:3 art above a readable footer.
		const areaHeight = Math.max( 0, height - COMPACT_FOOTER );
		const artScale = Math.min( width / 1600, areaHeight / 1200 );
		const art: UiRect = [
			(width - 1600 * artScale) / 2,
			(areaHeight - 1200 * artScale) / 2,
			1600 * artScale,
			1200 * artScale
		];
		const frameScale = Math.max( 0, Math.min( COMPACT_FRAME_WIDTH, width - COMPACT_MARGIN * 2 ) ) / 1121;
		const frame: UiRect = [
			(width - 1121 * frameScale) / 2,
			height - COMPACT_FRAME_BOTTOM,
			1121 * frameScale,
			64 * frameScale
		];
		const gauge: UiRect = [
			frame[0] + 27 * frameScale,
			frame[1] + 12 * frameScale,
			1064 * frameScale,
			20 * frameScale
		];
		const fraction = Math.max( 0, Math.min( 1, progress ) );
		return [
			{ ...quad( full, "" ), color: [ 0, 0, 0, 1 ] },
			quad( art, background ),
			quad( frame, assets[2]! ),
			quad( [ gauge[0], gauge[1], gauge[2] * fraction, gauge[3] ], assets[3]!, [ 0, 0, fraction, 1 ] ),
			quad( [
				(width - COMPACT_LABEL_WIDTH) / 2,
				height - COMPACT_LABEL_BOTTOM,
				COMPACT_LABEL_WIDTH,
				COMPACT_LABEL_HEIGHT
			], assets[4]! )
		];
	}
	const factor = Math.min( width / 1600, height / 1200 ),
		left = (width - 1600 * factor) / 2,
		top = height - 1200 * factor;
	/*
	================
	scale
	================
	*/
	const scale = (
		r: UiRect
	): UiRect => [
		Math.round( left + r[0] * factor ),
		Math.round( top + r[1] * factor ),
		r[2] * factor,
		r[3] * factor
	];
	const gauge = scale( [ 268, 985, 1064, 20 ] ), fraction = Math.max( 0, Math.min( 1, progress ) );
	return [
		{ ...quad( full, "" ), color: [ 0, 0, 0, 1 ] },
		quad(
			[ Math.trunc( (width - height * 4 / 3) / 2 + .5 ), 0, Math.trunc( height * 4 / 3 + .5 ), height ],
			background
		),
		quad( scale( [ 241, 973, 1121, 64 ] ), assets[2]! ),
		quad( [ gauge[0], gauge[1], gauge[2] * fraction, gauge[3] ], assets[3]!, [ 0, 0, fraction, 1 ] ),
		quad( scale( [ 268, 1025, 252, 35 ] ), assets[4]! )
	];
}

// CPSMission 0x7292E0 modes 2/3: destination region dispatch.
/*
================
regionLoadingBackground
================
*/
export function regionLoadingBackground( region: number ): string {
	let name = "china_1";
	if ( region === 0x694f ) name = "constantinople";
	else if ( region === 0x6a6c ) name = "samarkand";
	else if ( region === 0x6699 ) name = "dunwhang";
	else if ( region === 0x61a8 ) name = "zangan";
	else if ( region === 0x5c87 ) name = "hotan";
	else if ( region === 0x60b6 ) name = "thief";
	else if ( [ 0x624b, 0x6559, 0x6759 ].includes( region ) ) name = "port2";
	else if ( [ 0x629c, 0x64a1, 0x5b8c, 0x5a8c, 0x5b8f, 0x5a8f, 0x61a1, 0x609e ].includes( region ) ) name = "river";
	else if ( region === 0x8001 ) name = "dungeons_donwhang";
	// Royal mausoleum is named by retail for 8002..8007, but that artwork is
	// absent from this Legend III publication. Retain the published default.
	return root + "loading_" + name + ".png";
}

/*
================
travelLoadingQuads
================
*/
export function travelLoadingQuads(
	width: number,
	height: number,
	travel: import("@/engine/contracts/world").WorldTravel,
	variant: number,
	progress: number
): UiQuad[] {
	const background = travelLoadingBackground( travel, variant );
	const quads = loadingScreenQuads( width, height, background ?? "", progress );
	return background === null ? quads.filter( ( _, index ) => index !== 1 ) : quads;
}
/*
================
travelLoadingBackground
================
*/
export function travelLoadingBackground(
	travel: import("@/engine/contracts/world").WorldTravel,
	variant: number
): string | null {
	return travel.mode === 6 ?
		null :
		travel.mode === 1 ?
		root + "loading_rebirth.png" :
		travel.mode === 4 ?
		root + "loading_thief2.png" :
		travel.mode === 2 || travel.mode === 3 ?
		regionLoadingBackground( travel.region ) :
		travel.mode === 5 ?
		root + "loading_charactercustom.png" :
		root + `loading_europe_${variant === 2 ? 2 : 1}.png`;
}
