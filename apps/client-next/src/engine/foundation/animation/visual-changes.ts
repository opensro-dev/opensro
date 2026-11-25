/*
===========================================================================

visual-changes.ts - the costume a lent Spirit skill's buff puts on

Port-only, not native to v1.150 (SRO_NASRUN, #4407). The later clients'
charactervisualchange.txt dresses a character while a skill's buff holds
(ConditionID 3): a Devil's Spirit's lent skill puts its costume on its
caster - the hat, the shoulders and the body. The build publishes each
piece as an avatar record of the equipment catalog under a reserved id
above every RefItemID, and dress.visualChanges names each skill's pieces
(scripts/build/char/visualChanges.mjs). While such a buff is attached to
an actor, the costume replaces its worn avatars; the pieces' masks and
priority hide the equipment they cover, as any avatar's do.

===========================================================================
*/

/*
================
visualChangeAvatars

The costume pieces, as avatar rows, of the first visual-change buff
attached to gid, or undefined while none is.
================
*/
export function visualChangeAvatars(
	gid: number,
	effects: readonly { readonly gid: number; readonly skill: number; }[] | undefined,
	visualChanges: Readonly<Record<string, readonly number[]>> | undefined
): { slot: number; refObjId: number; typeFlags: number; plus: number; }[] | undefined {
	if ( !visualChanges || !effects ) return undefined;
	for ( const effect of effects ) {
		if ( effect.gid !== gid ) continue;
		const pieces = visualChanges[String( effect.skill )];
		if ( pieces?.length ) return pieces.map( refObjId => ({ slot: 0, refObjId, typeFlags: 0, plus: 0 }) );
	}
	return undefined;
}
