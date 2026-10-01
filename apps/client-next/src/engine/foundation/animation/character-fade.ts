/*
===========================================================================

character-fade.ts - a character's model alpha and what it reaches

Native fades (the local camera fade 866B90, body state 4, concealment, the
death fade) all set alpha on the character's CSkeletonModel (+0x98 -> +4,
vtable +0x14). The model includes its hair and equipment; skill effects are
separate CIDecoSkillEffectEntity objects positioned from bone sockets and
keep their own blend.

===========================================================================
*/

/*
================
CharacterFade
================
*/
export interface CharacterFade {
	mode: boolean;
	current: number;
	start: number;
	progress: number;
}

/*
================
advanceCharacterFade

8632D0 snapshots the current alpha only when the target changes. 862C60
stores float progress, then 9C2660 (cvttsd2si) truncates the interpolated
integer alpha.
================
*/
export function advanceCharacterFade( state: CharacterFade, hidden: boolean, seconds: number ): number {
	if ( !Number.isFinite( seconds ) || seconds < 0 ) throw new Error( "Invalid character fade clock" );
	if ( state.mode !== hidden ) {
		state.mode = hidden;
		state.start = state.current;
		state.progress = 0;
	}
	if ( state.progress < 1 ) {
		state.progress = Math.min( 1, Math.fround( state.progress + 2 * Math.fround( seconds ) ) );
		const target = hidden ? 0 : 255;
		state.current = state.progress === 1 ?
			target :
			Math.trunc( state.start + (target - state.start) * state.progress );
	}
	return state.current / 255;
}

/*
================
attachedOpacity

The opacity an attached actor draws with under its owner's opacity. A
model part ("compound": hair, equipment) fades with the owner's model; any
other attachment is an effect entity and keeps its own opacity, hidden only
while the owner is fully hidden (blind, full concealment), so an invisible
character never shows through its effects.
================
*/
export function attachedOpacity( own: number, owner: number, modelPart: boolean ): number {
	if ( modelPart ) return own * owner;
	return owner <= 0 ? 0 : own;
}
