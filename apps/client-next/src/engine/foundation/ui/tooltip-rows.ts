/*
===========================================================================

tooltip-rows.ts - the row record every tooltip builder emits

The item, skill, buff, mastery and commerce tooltips all describe their
text as a list of TooltipRow and share the two helpers here: filling a
printf-style template and splitting a packed ARGB colour. Pure: no
browser, no assets.

===========================================================================
*/

/*
================
TooltipRow

One line of tooltip text. color is a packed 0xAARRGGBB value; heading,
strong and ornament pick the line's style.
================
*/
export interface TooltipRow {
	readonly value: string;
	readonly color: number;
	readonly heading?: boolean;
	readonly strong?: boolean;
	readonly ornament?: "item" | "diamond";
}

/*
================
tooltipFormat

Fills %s and %d from values in order and turns %% into a literal %.
A placeholder with no value left becomes the empty string.
================
*/
export function tooltipFormat( template: string, ...values: (string | number)[] ): string {
	let i = 0;
	return template.replace( /%%|%[sd]/g, token => token === "%%" ? "%" : String( values[i++] ?? "" ) );
}

/*
================
tooltipColor

Splits a packed 0xAARRGGBB colour into [r, g, b, a] in the 0..1 range.
================
*/
export function tooltipColor( argb: number ): readonly [number, number, number, number] {
	return [ (argb >>> 16 & 255) / 255, (argb >>> 8 & 255) / 255, (argb & 255) / 255, (argb >>> 24) / 255 ];
}
