// 0091B830: one record is registered for every inclusive NNN~NNN name.
export function expandCharacterInfoCodenames( codename ) {
	const match = /^(.*?)(\d{3})~(\d{3})$/.exec( String( codename ?? "" ) );
	if ( !match ) return codename ? [ String( codename ) ] : [];
	const start = Number( match[2] );
	const end = Number( match[3] );
	if ( start > end ) return [];
	return Array.from(
		{ length: end - start + 1 },
		( _unused, index ) => `${match[1]}${String( start + index ).padStart( 3, "0" )}`
	);
}

// 91B7E0 registers only existing references; the first success seeds F08F58.
// 9171B0 looks up self, then common+5C (OrgObjCodeName128), then that seed.
// The original-object lookup is one hop, not recursive inheritance.
export function resolveCharacterInfoRows( authored, characterRows ) {
	const characters = new Map( [ ...characterRows ].filter( ( [, row] ) => Number( row[0] ) === 1 ) );
	const registered = new Map(
		authored.filter( row => characters.has( row.codename ) ).map( row => [ row.codename, row ] )
	);
	const first = registered.values().next().value;
	if ( !first ) throw Error( "No registered native characterInfo default" );
	return [ ...characters ].map( ( [codename, row] ) => {
		const source = registered.get( codename ) ?? registered.get( row[4] ) ?? first;
		return { ...source, codename, contextCodename: source.codename };
	} );
}
