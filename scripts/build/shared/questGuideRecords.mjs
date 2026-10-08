// 7F22D0 kinds 19/1A, 810620, 810F10 and 8115F0: compile the same media join.
export function questGuideRecords( questRows, contentsRows ) {
	const records = new Map(), bySymbol = new Map();
	for ( const fields of questRows ) {
		if ( fields[0]?.trim() !== "1" || fields.length < 11 ) continue;
		const id = Number( fields[1] ), level = Number( fields[3] ) & 255, symbol = fields[2];
		if ( !Number.isSafeInteger( id ) || id <= 0 || !symbol ) throw Error( "Invalid quest guide source" );
		if ( level > 90 || records.has( id ) ) continue;
		const row = { id, symbol, level, contentKey: fields[8] === "xxx" ? "" : fields[8], prerequisites: [] };
		records.set( id, row );
		if ( !bySymbol.has( symbol ) ) bySymbol.set( symbol, row );
	}
	// Column 3 names successors, not prerequisites. Native groups the incoming
	// edges by successor codename before assigning its prerequisite ID list.
	for ( const fields of contentsRows ) {
		const source = bySymbol.get( fields[0] );
		if ( !source || fields[3] === "xxx" || !fields[3] ) continue;
		for ( const successor of fields[3].split( "," ) ) {
			const target = bySymbol.get( successor );
			if ( target ) target.prerequisites.push( source.id );
		}
	}
	return [ ...records.values() ].sort( ( a, b ) => a.id - b.id );
}
