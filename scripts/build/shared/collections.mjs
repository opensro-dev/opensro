/** Stable string de-duplication; falsy inputs are outside the resource-list contract. */
export function uniqueStrings( values ) {
	const seen = new Set();
	const output = [];
	for ( const value of values ) {
		if ( !value || seen.has( value ) ) {
			continue;
		}
		seen.add( value );
		output.push( value );
	}
	return output;
}
