/*
===========================================================================

objectTextureLevels.mjs - per-level block overrides for a few object textures

A short table of DXT blocks that replace the encoded ones in a named level
of a named retail object texture. They are applied when a container is
copied from the shared block-texture cache to its published target, so the
cache, its keys and every texture not listed stay exactly as encoded.

===========================================================================
*/

const DXT1_BLOCK_BYTES = 8;
const NTX_HEADER_BYTES = 20;
const BLOCK_SIDE = 4;
const DXT1_FOURCC = 0x31545844;

/*
================
LEVELS

Source path suffix (lower case, forward slashes) -> the levels it
overrides: { level, blocks: [ [ block index in that level, block hex ] ] }.
================
*/
/** @type {Readonly<Record<string, { level: number, blocks: [ number, string ][] }[]>>} */
const LEVELS = Object.freeze( {
	"prim/mtrl/bldg/china/jangan_enter/cj_east_door.ddj": [
		{
			level: 1,
			blocks: [
				[ 3818, "0a638431fb8b5037" ],
				[ 3819, "4b736429bf2ae176" ],
				[ 3820, "6b736431aaa0957f" ],
				[ 3821, "8b73a5313faa56a6" ],
				[ 3822, "6b736431baaa4562" ],
				[ 3823, "4a6b43296380606b" ],
				[ 3824, "8b7364312faa5aa6" ],
				[ 3825, "2a6b6431b6a04568" ],
				[ 3826, "684a6431300017c0" ],
				[ 3827, "4a6b6431aaa05aa7" ],
				[ 3828, "a85a4329a8202ba4" ],
				[ 3882, "0a638531a686a6e6" ],
				[ 3883, "0a6364296c666e67" ],
				[ 3884, "8c7385314815aebf" ],
				[ 3885, "8b7384312654a6e6" ],
				[ 3886, "ac7384314041404a" ],
				[ 3887, "6b7385314452406a" ],
				[ 3888, "2a6b44290452c8ea" ],
				[ 3889, "0a636431cb636666" ],
				[ 3890, "4b6b643178d5a7df" ],
				[ 3891, "2a6b643186b484e6" ],
				[ 3892, "09636429268e24a6" ],
				[ 3946, "ac7344295aaa2baa" ],
				[ 3947, "8c7b853169efacac" ],
				[ 3949, "cd7b64315ebeaa8a" ],
				[ 3950, "cc7b643165aba2ac" ],
				[ 3951, "6b73a5314a022008" ],
				[ 3952, "8b73642954f2b210" ],
				[ 3953, "ac7385317daa2aa2" ],
				[ 3954, "6a6b643172aa2a0a" ],
				[ 3955, "8c7b44295aaaa8ac" ],
				[ 3956, "6b738531a1820800" ]
			]
		}
	]
} );

/*
================
levelsFor
================
*/
function levelsFor( source ) {
	const key = String( source ).replaceAll( "\\", "/" ).toLowerCase();
	for ( const [suffix, levels] of Object.entries( LEVELS ) ) {
		if ( key.endsWith( "/" + suffix ) || key === suffix ) return levels;
	}
	return null;
}

/*
================
applyObjectTextureLevels

Returns the container with the source's listed blocks replaced, or the
same buffer when nothing is listed. DXT1 containers only; a listed block
outside its level fails the build rather than writing past it.
================
*/
export function applyObjectTextureLevels( bytes, source ) {
	const levels = levelsFor( source );
	if ( !levels ) return bytes;
	if ( bytes.readUInt32LE( 12 ) !== DXT1_FOURCC ) throw Error( `${source}: level overrides need a DXT1 container` );
	const out = Buffer.from( bytes );
	const width = out.readUInt32LE( 4 ), height = out.readUInt32LE( 8 );
	for ( const { level, blocks } of levels ) {
		let offset = NTX_HEADER_BYTES, levelWidth = width, levelHeight = height;
		for ( let i = 0; i < level; i++ ) {
			offset += Math.ceil( levelWidth / BLOCK_SIDE ) * Math.ceil( levelHeight / BLOCK_SIDE ) * DXT1_BLOCK_BYTES;
			levelWidth = Math.max( 1, levelWidth >> 1 );
			levelHeight = Math.max( 1, levelHeight >> 1 );
		}
		const count = Math.ceil( levelWidth / BLOCK_SIDE ) * Math.ceil( levelHeight / BLOCK_SIDE );
		for ( const [index, hex] of blocks ) {
			if ( index < 0 || index >= count ) throw Error( `${source}: block ${index} is outside level ${level}` );
			Buffer.from( hex, "hex" ).copy( out, offset + index * DXT1_BLOCK_BYTES );
		}
	}
	return out;
}
