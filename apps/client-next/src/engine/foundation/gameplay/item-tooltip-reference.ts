import type { ItemFieldValues } from "@/engine/contracts/item-wire-fields";
export interface ItemTooltipReference {
	readonly descriptionSymbol?: string;
	readonly fields: ItemFieldValues;
}
export interface ItemMagicReference {
	readonly paramId: number;
	readonly optionName: string;
	readonly paramName: string;
	readonly degree: number;
	readonly rangeWords?: readonly [number, number, number];
}
export function itemTooltipReference( value: unknown, descriptionSymbol?: unknown ): ItemTooltipReference | undefined {
	if ( value === undefined ) return undefined;
	if ( !value || typeof value !== "object" || Array.isArray( value ) || Object.keys( value ).length > 256 ) {
		throw Error( "Invalid item tooltip reference" );
	}
	const fields: Record<string, number> = {};
	for ( const [key, n] of Object.entries( value ) ) {
		if ( !/^[a-zA-Z][a-zA-Z0-9_]*$/.test( key ) || typeof n !== "number" || !Number.isFinite( n ) ) {
			throw Error( "Invalid item tooltip field" );
		}
		fields[key] = n;
	}
	if (
		descriptionSymbol !== undefined && (typeof descriptionSymbol !== "string" || descriptionSymbol.length > 256)
	) throw Error( "Invalid item description symbol" );
	return { fields, descriptionSymbol: descriptionSymbol as string | undefined };
}
export function itemMagicReferences( value: unknown ): ReadonlyMap<number, ItemMagicReference> {
	if ( value === undefined ) return new Map();
	if ( !Array.isArray( value ) || value.length > 65536 ) throw Error( "Invalid item magic references" );
	const result = new Map<number, ItemMagicReference>();
	for ( const row of value ) {
		if (
			!row || !Number.isInteger( row.paramId ) || row.paramId < 1 || row.paramId > 65535 ||
			result.has( row.paramId ) || !Number.isInteger( row.degree ) || row.degree < 0 || row.degree > 255 ||
			[ row.optionName, row.paramName ].some( s => typeof s !== "string" || s.length > 256 )
		) throw Error( "Invalid item magic reference" );
		if ( row.rangeWords !== undefined && (!Array.isArray( row.rangeWords ) || row.rangeWords.length !== 3) ) {
			throw Error( "Invalid magic option bounds" );
		}
		if ( row.rangeWords !== undefined ) {
			for ( const n of row.rangeWords ) {
				if ( typeof n !== "number" || !Number.isInteger( n ) || n < 0 || n > 0xffffffff ) {
					throw Error( "Invalid magic option bounds" );
				}
			}
		}
		result.set( row.paramId, {
			rangeWords: row.rangeWords ? [ ...row.rangeWords ] as [number, number, number] : undefined,
			paramId: row.paramId,
			optionName: row.optionName,
			paramName: row.paramName,
			degree: row.degree
		} );
	}
	return result;
}
