import type { CharacterChannel, CharacterClip, CharacterNode } from "@/engine/contracts/character";
export interface NativeClip {
	readonly duration: number;
	readonly channels: readonly (Omit<CharacterChannel, "node"> & { readonly bone: string; })[];
}
// JMXVBAN 0102. A6C421 inserts timestamp/source-index pairs; duplicate times
// retain the first key. Convert the same native LH basis as the GLB publisher.
export function decodeNativeClip( bytes: Uint8Array ): NativeClip {
	const view = new DataView( bytes.buffer, bytes.byteOffset, bytes.byteLength ),
		decoder = new TextDecoder( "utf-8", { fatal: true } );
	let at = 0;
	const take = ( n: number ) => {
		if ( !Number.isSafeInteger( n ) || n < 0 || at + n > bytes.length ) throw Error( "Truncated BAN" );
		const start = at;
		at += n;
		return start;
	};
	const u32 = () => view.getUint32( take( 4 ), true ),
		f32 = () => {
			const v = view.getFloat32( take( 4 ), true );
			if ( !Number.isFinite( v ) ) throw Error( "Invalid BAN component" );
			return v;
		};
	const string = () => {
		const n = u32();
		if ( n > 4096 ) throw Error( "BAN string budget" );
		const start = take( n );
		return decoder.decode( bytes.subarray( start, start + n ) );
	};
	take( 12 );
	if ( decoder.decode( bytes.subarray( 0, 12 ) ) !== "JMXVBAN 0102" ) throw Error( "Invalid BAN signature" );
	// The clip's own name is never read, and later clips write it in the
	// retail codepage (nasrun1_man_run.ban is CP949 Korean, #4441): skip its
	// bytes rather than decode them, or the whole clip fails to load.
	u32();
	u32();
	take( u32() );
	const durationMs = u32();
	u32();
	u32();
	const count = u32();
	if ( !durationMs || durationMs > 3600000 || !count || count > 65536 ) throw Error( "BAN timeline budget" );
	const first = new Map<number, number>();
	for ( let i = 0; i < count; i++ ) {
		const t = u32();
		if ( !first.has( t ) ) first.set( t, i );
	}
	const order = [ ...first ].sort( ( a, b ) => a[0] - b[0] ),
		times = Float32Array.from( order, entry => entry[0] / 1000 ),
		indices = new Map( order.map( ( entry, i ) => [ entry[1], i ] ) );
	for ( let i = 1; i < times.length; i++ ) if ( times[i]! <= times[i - 1]! ) throw Error( "BAN timestamp precision" );
	const bones = u32();
	if ( bones > 1024 || bones * count > 1048576 ) throw Error( "BAN channel budget" );
	const channels: NativeClip["channels"][number][] = [], names = new Set<string>();
	for ( let b = 0; b < bones; b++ ) {
		const bone = string();
		if ( names.has( bone ) ) throw Error( "Duplicate BAN bone" );
		names.add( bone );
		if ( u32() !== count ) throw Error( "BAN key count mismatch" );
		const rotation = new Float32Array( times.length * 4 ), translation = new Float32Array( times.length * 3 );
		for ( let k = 0; k < count; k++ ) {
			const values = [ f32(), f32(), f32(), f32(), f32(), f32(), f32() ], i = indices.get( k );
			if ( i === undefined ) continue;
			rotation.set( [ -values[0]!, -values[1]!, values[2]!, values[3]! ], i * 4 );
			translation.set( [ values[4]!, values[5]!, -values[6]! ], i * 3 );
		}
		channels.push( { bone, path: "rotation", interpolation: "LINEAR", times, values: rotation }, {
			bone,
			path: "translation",
			interpolation: "LINEAR",
			times,
			values: translation
		} );
	}
	if ( at !== bytes.length ) throw Error( "Trailing BAN bytes" );
	return { duration: durationMs / 1000, channels };
}
export function bindNativeClip( source: NativeClip, name: string, nodes: readonly CharacterNode[] ): CharacterClip {
	const indices = new Map( nodes.map( ( node, i ) => [ node.name, i ] ) ), channels: CharacterChannel[] = [];
	for ( const { bone, ...channel } of source.channels ) {
		const node = indices.get( bone );
		if ( node !== undefined ) channels.push( { ...channel, node } );
	}
	if ( !channels.length ) throw Error( "BAN has no matching skeleton tracks" );
	return { name, duration: source.duration, channels };
}
