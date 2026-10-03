/*
===========================================================================

report.mjs - command lines and tables shared by the perf tools

Every perf tool takes `--name value` options and positional files, and
prints ranked tables of weights (time or bytes) by key. One parser and one
table keep their output alike, so numbers from different tools line up.

===========================================================================
*/

/*
================
parseOptions

spec maps an option name (without --) to its default: a boolean default
makes a flag, anything else takes the next argument (numbers parse as
numbers, a list default splits on commas). Unknown options are errors.
Returns the options plus `files`, the positional arguments.
================
*/
export function parseOptions( argv, spec, usage ) {
	const options = { ...spec, files: [] };
	for ( let i = 0; i < argv.length; i++ ) {
		const arg = argv[i];
		if ( !arg.startsWith( "--" ) ) {
			options.files.push( arg );
			continue;
		}
		const name = arg.slice( 2 ).replace( /-([a-z])/g, ( _, c ) => c.toUpperCase() );
		if ( !(name in spec) ) throw Error( `unknown option ${arg}\nusage: ${usage}` );
		const fallback = spec[name];
		if ( typeof fallback === "boolean" ) {
			options[name] = true;
			continue;
		}
		const value = argv[++i];
		if ( value === undefined ) throw Error( `${arg} needs a value\nusage: ${usage}` );
		options[name] = typeof fallback === "number" ?
			Number( value ) :
			Array.isArray( fallback ) ?
			value.split( "," ) :
			value;
	}
	return options;
}

/*
================
add
================
*/
export function add( map, key, weight ) {
	map.set( key, (map.get( key ) ?? 0) + weight );
}

/*
================
table

The top rows of map by weight, each weight divided by scale and printed
with unit: "   12.345 unit  key".
================
*/
export function table( map, { scale = 1, top = 30, unit = "", digits = 3 } = {} ) {
	return [ ...map ].sort( ( a, b ) => b[1] - a[1] ).slice( 0, top ).map( ( [key, weight] ) =>
		`${(weight / scale).toFixed( digits ).padStart( 10 )} ${unit}  ${key}`
	).join( "\n" );
}

/*
================
createStackTotals

Aggregates weighted call stacks (keys, innermost first): self weight of
the innermost key, inclusive weight of every key on the stack (once per
stack), and, for keys starting with childrenOf, the weight under each
direct callee of the innermost such frame ("(self)" for its own).
================
*/
export function createStackTotals( childrenOf = null ) {
	const self = new Map(), total = new Map(), children = new Map();
	let weight = 0;
	return {
		self,
		total,
		children,
		weight: () => weight,
		/*
		================
		add
		================
		*/
		add( stack, amount ) {
			if ( !stack.length ) return;
			weight += amount;
			add( self, stack[0], amount );
			const seen = new Set();
			for ( const key of stack ) {
				if ( seen.has( key ) ) continue;
				seen.add( key );
				add( total, key, amount );
			}
			if ( !childrenOf ) return;
			for ( let i = 0; i < stack.length; i++ ) {
				if ( !stack[i].startsWith( childrenOf ) ) continue;
				add( children, i === 0 ? "(self)" : stack[i - 1], amount );
				break;
			}
		}
	};
}
