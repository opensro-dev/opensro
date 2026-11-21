/*
===========================================================================

compound-part.ts - the fields every part of one compound character shares

A compound character (one CCompChar) is drawn as one compound: the draw
(A91970) applies the owner's single material register to every mesh node.
This module owns the port's half of that rule — what a part actor is made
of — so the Hwan hair, the auxiliary wings and the Item Mall mannequin's
children all build their parts the same way.

===========================================================================
*/
import type { CharacterActor } from "@/engine/contracts/character";

/*
================
compoundPart

The fields every part of one compound character shares: it stands in the
owner's pose, loops at the owner's scale, never picks, and wears the
owner's one material register, so the Hwan hair, the berserk wings and
any future part tint with the body by construction (wings staying white
on a red body is #3543). Whether a part casts a world shadow is the
caller's, not the compound's: the live parts pass shadowAttachment
themselves, the mall mannequin's children do not. Rides and booths are
separate compounds natively and stay out.
================
*/
export function compoundPart( owner: CharacterActor, ownerGid: number, bone: string ): Pick<
	CharacterActor,
	"pose" | "loop" | "scale" | "pickable" | "materialTint" | "attachment"
> {
	return {
		pose: owner.pose,
		loop: true,
		scale: 1,
		pickable: false,
		materialTint: owner.materialTint,
		attachment: { gid: ownerGid, bone, offset: [ 0, 0, 0 ], basis: "compound" }
	};
}
