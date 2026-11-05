/*
===========================================================================

crest-delete.ts - the crest delete items and their status lines

#2282 (delete half): ITEM_ETC_GUILD_CREST_DELETE (9269) and
ITEM_ETC_UNION_CREST_DELETE (9271) share the type word 3/3/12/2 with
their register twins, so the shipped reference id alone tells a delete
from a register (the name-change scroll's rule, name-change.ts). The
native trigger (item-use click 562402/56247C) opens the confirm box
0x16/0x19; its OK arm (6978A1) prints the DELETE_ERROR line when no
crest is set and otherwise composes the ordinary item use; the use's
0xB5BD answer raises the DELETE_COMPLETE box (755E40). The register
items (9268/9270) are deliberately unknown here: their flow hangs on
the crest-source decision in #2272.

===========================================================================
*/

export const GUILD_CREST_DELETE_REF_OBJ_ID = 9269;
export const UNION_CREST_DELETE_REF_OBJ_ID = 9271;

export type CrestDeleteKind = "guild" | "union";

/*
================
crestDeleteKind

Which crest a delete item erases, or undefined for any other row.
================
*/
export function crestDeleteKind( refObjId: number ): CrestDeleteKind | undefined {
	if ( refObjId === GUILD_CREST_DELETE_REF_OBJ_ID ) return "guild";
	if ( refObjId === UNION_CREST_DELETE_REF_OBJ_ID ) return "union";
	return undefined;
}

/*
================
crestDeleteKeys

The flow's three lines for one kind: the confirm box's body
(UIIT_MSG_*_CREST_DELETE), the no-crest refusal printed at OK
(UIIT_MSG_*_DELETE_ERROR) and the answer's completion body
(UIIT_MSG_*_CREST_DELETE_COMPLETE).
================
*/
export function crestDeleteKeys( kind: CrestDeleteKind ): {
	readonly confirm: string;
	readonly refused: string;
	readonly complete: string;
} {
	return kind === "guild" ?
		{
			confirm: "UIIT_MSG_GUILD_CREST_DELETE",
			refused: "UIIT_MSG_GUILD_DELETE_ERROR",
			complete: "UIIT_MSG_GUILD_CREST_DELETE_COMPLETE"
		} :
		{
			confirm: "UIIT_MSG_UNION_CREST_DELETE",
			refused: "UIIT_MSG_UNION_DELETE_ERROR",
			complete: "UIIT_MSG_UNION_CREST_DELETE_COMPLETE"
		};
}
