/*
skill-motion-resolve.ts

8E7150 / CInterfaceModel_StartAnimationBySet on the admitted caster model.
Resolve (animationSet, stateId) on the resident GLB branch first, then load a
published BAN when the body owns one. Authored-set miss retries DEFAULT
(0xCCCCC0), matching the second 8E7100 call in retail.
================
*/
import type { AnimationMetadata } from "./animation-metadata";
import { skillMotionStateName } from "./skill-motion";

export interface SkillMotionResolveInput {
	readonly role: string;
	readonly fallbackSet?: string;
	readonly clips: readonly string[];
	readonly bodyStates?: Record<string, AnimationMetadata>;
	readonly catalogStates?: Record<string, AnimationMetadata>;
	readonly motionUrls?: ReadonlyMap<string, string>;
}

export interface SkillMotionResolveResult {
	readonly clip: string;
	readonly definition: AnimationMetadata;
	readonly banUrl?: string;
}

/*
================
SkillMotionResolve_ParseRole

Split a native phase role into its authored set and numeric state id.
================
*/
function SkillMotionResolve_ParseRole( role: string ): { set: string; stateId: number; } | undefined {
	if ( !role.startsWith( "native:" ) ) {
		return undefined;
	}
	const parts = role.split( ":" );
	if ( parts.length !== 3 ) {
		return undefined;
	}
	const stateId = Number( parts[2] );
	if ( !Number.isInteger( stateId ) ) {
		return undefined;
	}
	return { set: parts[1]!, stateId };
}

/*
================
SkillMotionResolve_SetSuffix

Map BBDB70 set names to the clip suffix convention used by resident GLBs.
================
*/
function SkillMotionResolve_SetSuffix( set: string ): string {
	return set.replaceAll( "_", "-" );
}

/*
================
SkillMotionResolve_MergeStates

Catalog publication overlays the resident body metadata without discarding
states that only exist on the admitted GLB.
================
*/
function SkillMotionResolve_MergeStates(
	bodyStates: Record<string, AnimationMetadata> | undefined,
	catalogStates: Record<string, AnimationMetadata> | undefined
): Record<string, AnimationMetadata> {
	return { ...catalogStates, ...bodyStates };
}

/*
================
SkillMotionResolve_AdmitClip

Return clip metadata when the admitted model exposes the named branch.
================
*/
function SkillMotionResolve_AdmitClip(
	clips: readonly string[],
	metadata: Record<string, AnimationMetadata>,
	clip: string
): SkillMotionResolveResult | undefined {
	if ( !clips.includes( clip ) ) {
		return undefined;
	}
	const definition = metadata[clip];
	if ( !definition ) {
		return undefined;
	}
	return { clip, definition };
}

/*
================
SkillMotionResolve_FindInSet

8E7100 / AA6370 branch lookup for one animation set on the caster model.
================
*/
function SkillMotionResolve_FindInSet(
	input: SkillMotionResolveInput,
	set: string,
	stateId: number
): SkillMotionResolveResult | undefined {
	const metadata = SkillMotionResolve_MergeStates( input.bodyStates, input.catalogStates );
	const nativeRole = `native:${set}:${stateId}`;
	const banUrl = input.motionUrls?.get( nativeRole );
	const published = metadata[nativeRole];
	if ( published && banUrl ) {
		return { clip: nativeRole, definition: published, banUrl };
	}
	const resident = SkillMotionResolve_AdmitClip( input.clips, metadata, nativeRole );
	if ( resident ) {
		return resident;
	}
	const stem = skillMotionStateName( stateId )?.slice( 4 ).toLowerCase(),
		suffix = SkillMotionResolve_SetSuffix( set );
	if ( stem ) {
		const candidates = set === "default" ? [ stem, `${stem}-${suffix}` ] : [ `${stem}-${suffix}` ];
		for ( const clip of candidates ) {
			const match = SkillMotionResolve_AdmitClip( input.clips, metadata, clip );
			if ( match ) {
				return match;
			}
		}
	}
	if ( set !== "default" ) {
		return undefined;
	}
	for ( const [clip, definition] of Object.entries( metadata ) ) {
		if ( definition.stateId === stateId ) {
			const match = SkillMotionResolve_AdmitClip( input.clips, metadata, clip );
			if ( match ) {
				return match;
			}
		}
	}
	return undefined;
}

/*
================
skillMotionResolveAnimation

Port of 8EADF0 / 8E7150: resolve on the caster model (trying the override set
first without DEFAULT fallback when fallbackSet is supplied, as 8E7210 does),
then try fallbackSet (weapon set, 8E89D0 -> 8E7150), then retry DEFAULT when
the authored set does not own the requested state on this body.
================
*/
export function skillMotionResolveAnimation( input: SkillMotionResolveInput ): SkillMotionResolveResult | undefined {
	const parsed = SkillMotionResolve_ParseRole( input.role );
	if ( !parsed ) {
		return undefined;
	}
	const authored = SkillMotionResolve_FindInSet( input, parsed.set, parsed.stateId );
	if ( authored ) {
		return authored;
	}
	if ( input.fallbackSet && input.fallbackSet !== parsed.set && input.fallbackSet !== "default" ) {
		const fallback = SkillMotionResolve_FindInSet( input, input.fallbackSet, parsed.stateId );
		if ( fallback ) {
			return fallback;
		}
	}
	if ( parsed.set === "default" ) {
		return undefined;
	}
	return SkillMotionResolve_FindInSet( input, "default", parsed.stateId );
}
