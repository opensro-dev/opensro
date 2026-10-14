/*
===========================================================================

moonlitThiefSuit.mjs - the Moonlit Ash recolour of the level-20 thief suit

Port-only, not native (owner decision 2026-10-15, the look the balance
advisor picked on 2026-10-14). The level-20 thief suits the thief NPC
sells (CH thief_02, EU thief_eu_01, both sexes) get a second published
model: the retail mesh of the higher thief suit (CH thief_03, EU
thief_eu_03) with its texture recoloured, indigo cloth to ash-silver silk
and gold trim to pale gold. The client draws it only while the server's
SRO_MOONLIT_THIEF_SUIT is on; off, every suit keeps its retail model, and
the higher suits always do.

The recipe's numbers are the only source: no recoloured pixels live in the
repository. The build reads the retail texture already converted into the
model and recolours the two endpoint colours of every DXT block, at every
mip level. A block's texels are blends of its endpoints, so the colour
change carries through, and the texture keeps its format, size and block
indices: the variant model is the retail model with only those colour
bytes changed.

===========================================================================
*/

// The level-20 thief suit models (itemdata column 52 of the 3.1.7.2 rows
// STORE_TD_THIEF_TAB1/2 sells: CH 9026/9027 M and 9086/9087 W, EU 9146/9147
// M and 10310/10311 W), each to the retail mesh its Moonlit look wears.
// The CH woman's thief_03 cloth is a dark mauve-brown, not the man's
// indigo, so her recipe also turns that warm cloth to silk (warmCloth).
export const MOONLIT_MESHES = new Map( [
	[ "res/item/china/man_item/thief_02.bsr", { mesh: "res/item/china/man_item/thief_03.bsr", warmCloth: false } ],
	[ "res/item/china/woman_item/thief_02.bsr", { mesh: "res/item/china/woman_item/thief_03.bsr", warmCloth: true } ],
	[ "res/item/europe/man_item/thief_eu_01.bsr", {
		mesh: "res/item/europe/man_item/thief_eu_03.bsr",
		warmCloth: false
	} ],
	[
		"res/item/europe/woman_item/thief_eu_01.bsr",
		{ mesh: "res/item/europe/woman_item/thief_eu_03.bsr", warmCloth: false }
	]
] );

// The Moonlit Ash recipe. Cloth: texels of the retail indigo (hue 205-290,
// saturation from 0.12) take hue 212 at a fifth of their saturation and
// 1.65 times their value. Trim: the retail gold (hue 25-72, saturation from
// 0.18) takes hue 44 at 0.45 of its saturation with a lifted value. Every
// other colour is tinted slightly cool.
const CLOTH_HUE = 212;
const CLOTH_SATURATION_SCALE = 0.22;
const CLOTH_VALUE_SCALE = 1.65;
const CLOTH_SATURATION_MAX = 0.9;
const TRIM_HUE = 44;
const TRIM_SATURATION_SCALE = 0.45;
const TRIM_SATURATION_MAX = 0.85;
const TRIM_VALUE_CONTRAST = 1.25;
const TRIM_VALUE_LIFT = 0.72;
const NEUTRAL_TINT = [ 1.0, 1.0, 1.04 ];
// Warm cloth (warmCloth only): dull, dark texels of hue -70..45 (mauve to
// brown). Skin shares the hue but is brighter and more saturated, so the
// saturation and value bounds keep it. It takes the cloth's silk, lifted.
const WARM_HUE_LOW = [ -70, -55 ];
const WARM_HUE_HIGH = [ 30, 45 ];
const WARM_SATURATION_MAX = [ 0.36, 0.48 ];
const WARM_VALUE_MAX = [ 0.30, 0.42 ];
const WARM_VALUE_LIFT = 0.10;

const NTX_MAGIC = 0x3158544e;
const NTX_HEADER = 20;
const DXT1 = 0x31545844;
const DXT3 = 0x33545844;
const DXT5 = 0x35545844;
const GLB_MAGIC = 0x46546c67;
const GLB_HEADER = 12;
const CHUNK_HEADER = 8;
const CHUNK_JSON = 0x4e4f534a;
const CHUNK_BIN = 0x004e4942;
const NATIVE_MIME = "application/x-sro-texture";

/*
================
smoothstep
================
*/
function smoothstep( x, edge0, edge1 ) {
	const t = Math.min( 1, Math.max( 0, (x - edge0) / (edge1 - edge0) ) );
	return t * t * (3 - 2 * t);
}

/*
================
hsvToRgb
================
*/
function hsvToRgb( hue, saturation, value ) {
	const h = (hue % 360 + 360) % 360 / 60, sector = Math.floor( h ), f = h - sector;
	const p = value * (1 - saturation), q = value * (1 - saturation * f), t = value * (1 - saturation * (1 - f));
	return [
		[ value, q, p, p, t, value ][sector % 6],
		[ t, value, value, q, p, p ][sector % 6],
		[ p, p, t, value, value, q ][sector % 6]
	];
}

/*
================
moonlitColour

The recipe for one colour, channels 0..1. The two masks blend smoothly,
so a colour near a mask's edge is part retail, part Moonlit. warmCloth
adds the warm-cloth mask for a texture whose cloth is not indigo.
================
*/
export function moonlitColour( r, g, b, warmCloth = false ) {
	const max = Math.max( r, g, b ), min = Math.min( r, g, b ), delta = max - min;
	const value = max, saturation = max > 0 ? delta / max : 0;
	let hue = 0;
	if ( delta > 1e-6 ) {
		if ( max === r ) hue = ((g - b) / delta % 6 + 6) % 6;
		else if ( max === g ) hue = (b - r) / delta + 2;
		else hue = (r - g) / delta + 4;
		hue *= 60;
	}
	const cloth = smoothstep( saturation, 0.12, 0.30 ) * smoothstep( hue, 205, 220 ) *
		(1 - smoothstep( hue, 275, 290 ));
	const trim = smoothstep( saturation, 0.18, 0.35 ) * smoothstep( hue, 25, 35 ) * (1 - smoothstep( hue, 62, 72 ));
	const silk = hsvToRgb(
		CLOTH_HUE,
		Math.min( CLOTH_SATURATION_MAX, saturation * CLOTH_SATURATION_SCALE ),
		Math.min( 1, value * CLOTH_VALUE_SCALE )
	);
	const gold = hsvToRgb(
		TRIM_HUE,
		Math.min( TRIM_SATURATION_MAX, saturation * TRIM_SATURATION_SCALE ),
		Math.min( 1, Math.max( 0, (value - 0.5) * TRIM_VALUE_CONTRAST + TRIM_VALUE_LIFT ) )
	);
	const signedHue = hue > 180 ? hue - 360 : hue;
	const warm = warmCloth ?
		smoothstep( signedHue, ...WARM_HUE_LOW ) * (1 - smoothstep( signedHue, ...WARM_HUE_HIGH )) *
		(1 - smoothstep( saturation, ...WARM_SATURATION_MAX )) * (1 - smoothstep( value, ...WARM_VALUE_MAX )) *
		Math.max( 0, 1 - cloth - trim ) :
		0;
	const warmSilk = hsvToRgb(
		CLOTH_HUE,
		Math.min( CLOTH_SATURATION_MAX, saturation * CLOTH_SATURATION_SCALE ),
		Math.min( 1, value * CLOTH_VALUE_SCALE + WARM_VALUE_LIFT )
	);
	const rest = Math.min( 1, Math.max( 0, 1 - cloth - trim - warm ) );
	return [ r, g, b ].map( ( channel, i ) =>
		Math.min(
			1,
			Math.max( 0, channel * NEUTRAL_TINT[i] * rest + silk[i] * cloth + gold[i] * trim + warmSilk[i] * warm )
		)
	);
}

/*
================
recolour565
================
*/
function recolour565( packed, warmCloth ) {
	const [r, g, b] = moonlitColour(
		(packed >> 11 & 31) / 31,
		(packed >> 5 & 63) / 63,
		(packed & 31) / 31,
		warmCloth
	);
	return Math.round( r * 31 ) << 11 | Math.round( g * 63 ) << 5 | Math.round( b * 31 );
}

/*
================
recolourColourBlock

One BC colour block at offset. A DXT1 block's endpoint order picks its
mode (c0 > c1: four colours; else three and transparent), so a recoloured
pair that changed order is swapped back with its indices remapped, and a
four-colour pair that became equal is made one solid colour. DXT3 and DXT5
colour blocks are always four-colour, so they keep their indices.
================
*/
function recolourColourBlock( bytes, offset, dxt1, warmCloth ) {
	const c0 = bytes.readUInt16LE( offset ), c1 = bytes.readUInt16LE( offset + 2 );
	let n0 = recolour565( c0, warmCloth ),
		n1 = recolour565( c1, warmCloth ),
		indices = bytes.readUInt32LE( offset + 4 );
	if ( dxt1 ) {
		const fourColour = c0 > c1;
		if ( fourColour && n0 === n1 ) {
			indices = 0;
		} else if ( fourColour ? n0 < n1 : n0 > n1 ) {
			[n0, n1] = [ n1, n0 ];
			// Four colours: 0<->1 and 2<->3 (the two blends swap ends). Three
			// colours: 0<->1; 2 is the midpoint and 3 transparent either way.
			let swapped = 0;
			for ( let texel = 0; texel < 16; texel++ ) {
				const index = indices >>> texel * 2 & 3;
				const mapped = fourColour ? index ^ 1 : index < 2 ? index ^ 1 : index;
				swapped |= mapped << texel * 2;
			}
			indices = swapped >>> 0;
		}
	}
	bytes.writeUInt16LE( n0, offset );
	bytes.writeUInt16LE( n1, offset + 2 );
	bytes.writeUInt32LE( indices, offset + 4 );
}

/*
================
recolourNativeTexture

An NTX1 resource (header, then every BC level) recoloured in place on a
copy. Every level is recoloured, so the mips stay consistent. warmCloth
selects the warm-cloth mask (MOONLIT_MESHES).
================
*/
export function recolourNativeTexture( source, warmCloth = false ) {
	const bytes = Buffer.from( source );
	if ( bytes.length < NTX_HEADER || bytes.readUInt32LE( 0 ) !== NTX_MAGIC ) {
		throw Error( "Moonlit: not an NTX texture" );
	}
	const width = bytes.readUInt32LE( 4 ), height = bytes.readUInt32LE( 8 );
	const format = bytes.readUInt32LE( 12 ), levels = bytes.readUInt32LE( 16 );
	if ( ![ DXT1, DXT3, DXT5 ].includes( format ) ) throw Error( "Moonlit: unsupported texture format" );
	const blockBytes = format === DXT1 ? 8 : 16, colourOffset = format === DXT1 ? 0 : 8;
	let offset = NTX_HEADER;
	for ( let level = 0; level < levels; level++ ) {
		const blocks = Math.ceil( Math.max( 1, width >> level ) / 4 ) * Math.ceil( Math.max( 1, height >> level ) / 4 );
		if ( offset + blocks * blockBytes > bytes.length ) throw Error( "Moonlit: truncated texture" );
		for ( let block = 0; block < blocks; block++, offset += blockBytes ) {
			recolourColourBlock( bytes, offset + colourOffset, format === DXT1, warmCloth );
		}
	}
	if ( offset !== bytes.length ) throw Error( "Moonlit: trailing texture bytes" );
	return bytes;
}

/*
================
moonlitGlb

A converted suit model with each native texture recoloured. The texture
keeps its byte length, so the model keeps its layout and only the colour
bytes differ.
================
*/
export function moonlitGlb( source, warmCloth = false ) {
	const bytes = Buffer.from( source );
	if ( bytes.readUInt32LE( 0 ) !== GLB_MAGIC || bytes.readUInt32LE( GLB_HEADER + 4 ) !== CHUNK_JSON ) {
		throw Error( "Moonlit: not a binary glTF" );
	}
	const jsonLength = bytes.readUInt32LE( GLB_HEADER );
	const json = JSON.parse(
		bytes.toString( "utf8", GLB_HEADER + CHUNK_HEADER, GLB_HEADER + CHUNK_HEADER + jsonLength )
	);
	const bin = GLB_HEADER + CHUNK_HEADER + jsonLength;
	if ( bytes.readUInt32LE( bin + 4 ) !== CHUNK_BIN ) throw Error( "Moonlit: missing binary chunk" );
	const images = (json.images ?? []).filter( image => image.mimeType === NATIVE_MIME );
	if ( !images.length ) throw Error( "Moonlit: the suit model has no native texture" );
	for ( const image of images ) {
		const view = json.bufferViews[image.bufferView];
		const start = bin + CHUNK_HEADER + (view.byteOffset ?? 0), end = start + view.byteLength;
		recolourNativeTexture( bytes.subarray( start, end ), warmCloth ).copy( bytes, start );
	}
	return bytes;
}

/*
================
moonlitModelPath

The variant's public path beside its retail model.
================
*/
export function moonlitModelPath( glb ) {
	if ( !glb.endsWith( ".glb" ) ) throw Error( "Moonlit: not a model path " + glb );
	return glb.slice( 0, -".glb".length ) + "_moonlit.glb";
}
