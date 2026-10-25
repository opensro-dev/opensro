/*
===========================================================================

combat-spacing.ts - the server's combat spacing, mirrored for prediction

The server casts a skill where its combat spacing is met: the center
distance at which the caster's body radius plus the target's body radius
plus the skill's reach admits the action (apps/server
internal/game/world/simulation/combatspacing.go). Anything that predicts
where a caster stops - the skill run-up today, the attack chase next -
aims at that admission boundary, never at the authored range alone: a
walk that runs past it waits beyond where the server casts, and the cast
pulls it back.

A monster's body is graded: trunc(float32(base) * grade) with grade 1.5
(champion), 3 (giant), 1.7 (elite) on the rarity byte's low nibble and 1
for every other grade (the server's monster gradescale.go). A character's
body is its model radius, ungraded. The radii ride the bootstrap's
refObjSnapshot rows (bodyRadius); what the snapshot does not carry, this
module does not invent.

The server's own walk aims closer in (its stand-off radius), but the cast
pre-empts the walk the moment the boundary is crossed and halts the
caster there, so the boundary is the stop a prediction waits at. What
still differs - a movement tick of penetration, the whole-unit rounding
below - is small and glides; it never snaps.

===========================================================================
*/

// The rarity byte's low-nibble grades (the server's gradescale.go).
const GRADE_CHAMPION = 1;
const GRADE_GIANT = 4;
const GRADE_ELITE = 6;
const CHAMPION_GRADE_SCALE = 1.5;
const GIANT_GRADE_SCALE = 3;
// float32(1.7) widened to a double: the server's elite scale (its float32
// 1.70000005) at the precision every spacing sum reads it.
const ELITE_GRADE_SCALE = 1.7000000476837158;

/*
================
monsterBodyRadius

A monster instance's body: its ref row's radius narrowed to float32,
scaled by its grade, truncated. An unknown grade keeps the bare body.
================
*/
export function monsterBodyRadius( baseRadius: number, rarity: number ): number {
	const grade = rarity & 0x0f;
	const scale = grade === GRADE_CHAMPION ?
		CHAMPION_GRADE_SCALE :
		grade === GRADE_GIANT ?
		GIANT_GRADE_SCALE :
		grade === GRADE_ELITE ?
		ELITE_GRADE_SCALE :
		1;
	return Math.trunc( Math.fround( baseRadius ) * scale );
}

/*
================
refBodyRadius

The body one ref row contributes to spacing: graded for a monster, the
bare model radius for anything else. Undefined while the snapshot carries
no radius for the row: the server resolves that body from data the client
does not hold, so no spacing is computed from it.
================
*/
export function refBodyRadius(
	kind: string,
	baseRadius: number | undefined,
	rarity: number
): number | undefined {
	if ( baseRadius === undefined ) return undefined;
	return kind === "monster" ? monsterBodyRadius( baseRadius, rarity ) : baseRadius;
}

/*
================
combatAdmissionRadius

The center distance at which the action is legal: both bodies plus the
reach. Undefined on data the server itself would not validate (a body
must be positive and finite, the reach finite): its spacing then holds
the cast, and a prediction must not invent a stop of its own.
================
*/
export function combatAdmissionRadius(
	actorRadius: number,
	targetRadius: number,
	reach: number
): number | undefined {
	if (
		!Number.isFinite( actorRadius ) || actorRadius <= 0 || !Number.isFinite( targetRadius ) ||
		targetRadius <= 0 || !Number.isFinite( reach ) || reach < 0
	) return undefined;
	return actorRadius + targetRadius + reach;
}

/*
================
roundWhole

Whole units, half away from zero - Go's math.Round, which the server's
walk goal rounds with. Math.round alone rounds half toward positive
infinity (-2.5 to -2, where Go gives -3).
================
*/
function roundWhole( value: number ): number {
	return Math.sign( value ) * Math.round( Math.abs( value ) );
}

/*
================
combatStopPoint

Where a caster walking from (fromX, fromZ) toward (targetX, targetZ)
stands when the spacing is met: on the line, one admission radius from
the target, rounded the way the server rounds its walk goal. Callers ask
only beyond the boundary (at or inside it there is no walk).
================
*/
export function combatStopPoint(
	fromX: number,
	fromZ: number,
	targetX: number,
	targetZ: number,
	admission: number
): { x: number; z: number; } {
	const dx = targetX - fromX, dz = targetZ - fromZ, distance = Math.hypot( dx, dz );
	if ( !(distance > 0) ) return { x: roundWhole( targetX ), z: roundWhole( targetZ ) };
	const travel = Math.max( 0, distance - admission );
	return {
		x: roundWhole( fromX + dx / distance * travel ),
		z: roundWhole( fromZ + dz / distance * travel )
	};
}
