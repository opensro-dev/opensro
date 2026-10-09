/*
===========================================================================

hud-readouts.ts - the underbar and minimap text readouts

Pure formatting for the HUD's numeric readouts: the experience percent and
the skill point count on the underbar (CIFUnderBar_OnUpdate 571F60), and
the minimap's coordinates and arrow rotation.

===========================================================================
*/
import { headingRadians } from "@/engine/foundation/math/angles";

import type { Pose } from "@/engine/contracts/gameplay";

// Port-only shortened skill point units (skillPointReadouts).
const SKILL_POINT_THOUSAND = 1e3;
const SKILL_POINT_MILLION = 1e6;
const SKILL_POINT_BILLION = 1e9;

/*
================
experienceReadout

CIFUnderBar::Update 571f60; float32 store precedes printf rounding.
================
*/
export function experienceReadout(
	experience: string,
	level: number,
	levels: ReadonlyMap<number, readonly [string, number]>
): string {
	const required = levels.get( level )?.[0];
	if ( !required ) return "";
	const percent = Math.min(
		Math.fround( 99.989997863769531 ),
		Math.fround( Number( BigInt( experience ) ) * 100 / Number( required ) )
	);
	return Math.max( 0, percent ).toFixed( 2 ) + " %";
}

/*
================
skillPointReadouts

CIFUnderBar_OnUpdate (571F60) prints the skill points with "%d" into
GDR_STATIC_SP, a 48-pixel static, which holds about eight digits. Beta
growth rates push the count past that, and the full number then ran over
the SP gauge. Port-only, not native: the HUD draws the first of these
that fits, so a count that fits is the native "%d" and a longer one is
shown in whole thousands, millions or billions (123456K) with no leading
digit lost. The last is drawn when none fits.
================
*/
export function skillPointReadouts( points: number ): readonly string[] {
	return [
		String( points ),
		Math.floor( points / SKILL_POINT_THOUSAND ) + "K",
		Math.floor( points / SKILL_POINT_MILLION ) + "M",
		Math.floor( points / SKILL_POINT_BILLION ) + "B"
	];
}

/*
================
minimapCoordinates

CIFMinimap's outdoor coordinate conversion truncates toward zero.
================
*/
export function minimapCoordinates( pose: Pick<Pose, "regionId" | "x" | "z"> ): readonly [string, string] {
	if ( pose.regionId & 0x8000 ) return [ "", "" ];
	const x = (((pose.regionId & 255) * 3 - 0x195) << 6) - Math.trunc( pose.x / -10 ),
		y = (((pose.regionId >>> 8) * 3 - 0x114) << 6) - Math.trunc( pose.z / -10 );
	return [ "X:" + String( x ).padStart( 3, " " ), "Y:" + String( y ).padStart( 3, " " ) ];
}

/*
================
minimapRotation

The authored arrow points right (+X). Map +Z projects upward, while UI
quad rotation is clockwise in screen coordinates. Wire zero already faces
+X; model yaw's pi/2 offset must not be added to this sprite.
================
*/
export function minimapRotation( angle: number ): number {
	return -headingRadians( angle );
}
