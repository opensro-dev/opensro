/** Parse a `Section = Name,...` header, or return null for any other line. */
export function parseResinfoSectionName( rawLine ) {
	return /^\s*Section\s*=\s*([^,]+),/.exec( rawLine )?.[1].trim() ?? null;
}

/** Extract one section header plus its brace-balanced authored body. */
export function extractResinfoSection( lines, name ) {
	const start = lines.findIndex( ( line ) => parseResinfoSectionName( line ) === name );
	if ( start < 0 ) {
		return null;
	}
	let depth = 0;
	let sawOpen = false;
	for ( let index = start; index < lines.length; index += 1 ) {
		for ( const character of lines[index] ) {
			if ( character === "{" ) {
				depth += 1;
				sawOpen = true;
			} else if ( character === "}" ) {
				depth -= 1;
			}
		}
		if ( sawOpen && depth === 0 ) {
			return lines.slice( start, index + 1 );
		}
	}
	return null;
}

/** Inventory authored sections and root/control types for the CIF catalog. */
export function parseResinfoSummary( text ) {
	const sections = [];
	const controlTypes = [];
	const rootControlTypes = [];
	const sectionControlTypes = [];
	let currentSection = null;
	let currentNode = null;

	for ( const rawLine of text.split( /\r?\n/ ) ) {
		const line = rawLine.trim();
		if ( !line || line === "Interface Text" || line === "{" ) {
			continue;
		}

		const sectionName = parseResinfoSectionName( line );
		if ( sectionName ) {
			currentSection = sectionName;
			sections.push( currentSection );
			currentNode = null;
			continue;
		}

		const nodeMatch = /^([A-Za-z0-9_]+):([A-Za-z0-9_]+)/.exec( line );
		if ( nodeMatch ) {
			currentNode = { name: nodeMatch[1], type: nodeMatch[2] };
			controlTypes.push( currentNode.type );
			if ( currentSection ) {
				sectionControlTypes.push( {
					section: currentSection,
					name: currentNode.name,
					type: currentNode.type
				} );
			}
			if ( currentSection && !rootControlTypes.some( ( entry ) => entry.section === currentSection ) ) {
				rootControlTypes.push( {
					section: currentSection,
					name: currentNode.name,
					type: currentNode.type
				} );
			}
			continue;
		}

		if ( line === "}" ) {
			currentNode = null;
		}
	}

	return {
		sections: [ ...new Set( sections ) ],
		controlTypes: [ ...new Set( controlTypes ) ].sort(),
		rootControlTypes,
		sectionControlTypes
	};
}
