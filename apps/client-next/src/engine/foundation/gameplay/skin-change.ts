/*
===========================================================================

skin-change.ts - the character skin change scroll and its window's draft

ITEM_MALL_CHAR_SKIN_CHANGE_SCROLL (3/3/13/9) opens CIFChangePlayerModel
instead of using itself. The window lists the player's country's models
split by sex (CIFChangePlayerModel_BuildModelLists 6D14C0), a 1-based
figure slider over the chosen sex, and two shape sliders 0..4
(CIFChangePlayerModel_RefreshScaleScrollBar 6D0980). Confirm stays
disabled until the draft differs from the body worn
(CIFChangePlayerModel_RefreshConfirmEnabled 6D05F0); it then uses the
scroll with the tail [u32 model][u8 shape]
(CIFChangePlayerModel_OnConfirm 6D0650).

The shape byte packs height low and volume high, as character creation and
the entry packets do; 0xFF is the unset shape (both steps 2).

===========================================================================
*/

// The scroll's packed type word: 3/3/13 (0x6EC) with type 4 = 9.
const SKIN_SCROLL_FAMILY = 0x6ec;
const SKIN_SCROLL_TYPE = 9;
export const SKIN_MAX_STEP = 4;
const SKIN_DEFAULT_STEP = 2;
const SKIN_UNSET_SHAPE = 0xff;

/*
================
PlayerModel

One player model reference: its RefObjID and sex selector (+0x1AC).
================
*/
export interface PlayerModel {
	readonly refObjId: number;
	readonly sex: number;
}

/*
================
SkinChoice

What the scroll's use carries.
================
*/
export interface SkinChoice {
	readonly model: number;
	readonly shape: number;
}

/*
================
SkinDraft

The window's sliders: sex, the 1-based figure, height and volume steps.
================
*/
export interface SkinDraft {
	readonly sex: number;
	readonly figure: number;
	readonly height: number;
	readonly volume: number;
}

export type SkinDraftKey = "sex" | "figure" | "height" | "volume";

/*
================
isSkinChangeScroll
================
*/
export function isSkinChangeScroll( word: number ): boolean {
	return !(word & 2) && (word & 0x7fc) === SKIN_SCROLL_FAMILY && word >>> 11 === SKIN_SCROLL_TYPE;
}

/*
================
skinChangeTail
================
*/
export function skinChangeTail( choice: SkinChoice ): Uint8Array {
	if (
		!Number.isInteger( choice.model ) || choice.model < 1 || choice.model > 0xffffffff ||
		!Number.isInteger( choice.shape ) || choice.shape < 0 || choice.shape > 0xff
	) throw Error( "Invalid skin choice" );
	const tail = new Uint8Array( 5 );
	new DataView( tail.buffer ).setUint32( 0, choice.model, true );
	tail[4] = choice.shape;
	return tail;
}

/*
================
skinModelsOf
================
*/
export function skinModelsOf( models: readonly PlayerModel[], sex: number ): readonly PlayerModel[] {
	return models.filter( row => row.sex === sex );
}

/*
================
shapeStep
================
*/
function shapeStep( value: number ): number {
	return value > SKIN_MAX_STEP ? SKIN_DEFAULT_STEP : value;
}

/*
================
initialSkinDraft

The worn body: its sex and figure, and its shape steps.
================
*/
export function initialSkinDraft( models: readonly PlayerModel[], model: number, shape: number ): SkinDraft {
	const worn = models.find( row => row.refObjId === model );
	const sex = worn?.sex ?? 0;
	const figure = Math.max( 1, skinModelsOf( models, sex ).findIndex( row => row.refObjId === model ) + 1 );
	const packed = shape === SKIN_UNSET_SHAPE ? SKIN_DEFAULT_STEP << 4 | SKIN_DEFAULT_STEP : shape;
	return { sex, figure, height: shapeStep( packed & 15 ), volume: shapeStep( packed >>> 4 & 15 ) };
}

/*
================
skinDraftRange
================
*/
export function skinDraftRange(
	models: readonly PlayerModel[],
	draft: SkinDraft,
	key: SkinDraftKey
): readonly [number, number] {
	if ( key === "sex" ) return [ 0, 1 ];
	if ( key === "figure" ) return [ 1, Math.max( 1, skinModelsOf( models, draft.sex ).length ) ];
	return [ 0, SKIN_MAX_STEP ];
}

/*
================
setSkinDraft

A slider or sex button moved; the figure stays inside the chosen sex.
================
*/
export function setSkinDraft(
	models: readonly PlayerModel[],
	draft: SkinDraft,
	key: SkinDraftKey,
	value: number
): SkinDraft {
	const [min, max] = skinDraftRange( models, draft, key );
	const next = { ...draft, [key]: Math.max( min, Math.min( max, Math.round( value ) ) ) };
	const [, figures] = skinDraftRange( models, next, "figure" );
	return { ...next, figure: Math.min( next.figure, figures ) };
}

/*
================
skinDraftChoice
================
*/
export function skinDraftChoice( models: readonly PlayerModel[], draft: SkinDraft ): SkinChoice | null {
	const model = skinModelsOf( models, draft.sex )[draft.figure - 1];
	return model ? { model: model.refObjId, shape: draft.volume << 4 | draft.height } : null;
}

/*
================
skinDraftChanged

6D05F0: the confirm button is live once the draft differs from the body.
================
*/
export function skinDraftChanged(
	models: readonly PlayerModel[],
	draft: SkinDraft,
	model: number,
	shape: number
): boolean {
	const choice = skinDraftChoice( models, draft ), worn = initialSkinDraft( models, model, shape );
	return !!choice && (choice.model !== model || draft.height !== worn.height || draft.volume !== worn.volume);
}
