/*
===========================================================================

buildHdTextureVariants.mjs - x2 AI-upscaled variants of the world textures

Port-only, not native (#2061). With the Experimental "HD textures" row
on, a client is served an x2 re-encoding of each world object texture
instead of the retail texture (client foundation/assets/texture-variants
.ts, hdTextureVariantPath). The set is published outside /assets, under
CLIENT_PUBLIC_ROOT/hd, so the default manifest and precache hold no HD
bytes; buildHdTexturePacks.mjs packs it behind its own index there.

Each texture's level 0 is decoded with the client's own NTX decoder,
upscaled by the pinned Real-ESRGAN (encoders.mjs, realesrgan-x4plus),
and re-encoded per level: BC7 by the pinned texconv for a device that
samples BC, ASTC 4x4 by the pinned astcenc for one that does not. The
model runs at its native x4 (this build's x2 mode crushes its output to
near black; measured 2026-11-07) and the result is box-downscaled to
the x2 the set ships, from which the mip chain is re-derived, so a
variant has the full chain of its new size.

The upscaler's alpha channel is not usable (it processes colour; a flat
255 alpha comes back scattered between 0 and 255), so alpha travels as
its own greyscale image in the same run and is recombined before the
encode. As with the ASTC set, a variant whose upscaled alpha is anywhere
below 255 carries that alpha (level 0, one byte a texel) after its
levels, so a variant picks exactly as the original (native-texture.ts
NTX_PICK_ALPHA). A texture whose upscaled level 0 is not whole 4x4
blocks, or exceeds the client's dimension limit, keeps its original:
the HD set is an overlay, never a requirement.

Upscaling runs a batch of textures per Real-ESRGAN process (its model
load costs seconds; one process a texture would make the full set a
multi-day encode). Results are cached by the source's SHA-256 under a
cache name that carries every tool pin, so a pin change re-encodes.

	node scripts/build/textures/buildHdTextureVariants.mjs
		[--jobs N] [--only /assets/world/prefix] [--limit N]
		[--out DIR]

--out writes the variants (and their cache) below DIR instead of the
published tree: a private build that leaves the shared tree untouched.
The exported pass also takes a publicRoot override for fixture trees
(the buildAssetPacks convention); its output then lands in that tree.

===========================================================================
*/
import { createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { cpus } from "node:os";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { CLIENT_PUBLIC_ROOT, generatedPath } from "../../lib/generatedRoot.mjs";
import { ENCODERS, encoderPath } from "./encoders.mjs";
import {
	decodeNativeTexture,
	decodeNativeTextureLevel
} from "../../../apps/client-next/src/engine/foundation/assets/native-texture.ts";
import { hdTextureVariantPath } from "../../../apps/client-next/src/engine/foundation/assets/texture-variants.ts";

const run = promisify( execFile );

const NTX_MAGIC = 0x3158544e;
const NTX_HEADER_BYTES = 20;
const NTX_BC7 = 0x55374342; // "BC7U", native-texture.ts
const NTX_ASTC_4X4 = 0x34345341; // "AS44", native-texture.ts
const NTX_PICK_ALPHA = 0x38414b50; // "PKA8", native-texture.ts
const ASTC_HEADER_BYTES = 16;
const ASTC_MAGIC = 0x5ca1ab13;
const TGA_HEADER_BYTES = 18;
// Uncompressed true colour, 32 bits, top-left origin with 8 alpha bits.
const TGA_TRUE_COLOUR = 2;
const TGA_DESCRIPTOR = 0x28;
// A DDS with the DX10 extension: 128 bytes of header, then the format.
const DDS_HEADER_BYTES = 148;
const DXGI_FORMAT_BC7_UNORM = 98;
const BLOCK = 4;
const BLOCK_BYTES = 16;
// Client MAX_DIMENSION (native-texture.ts): a variant past it is refused.
const MAX_DIMENSION = 8192;
const SCALE = 2;
// The model's native ratio; the shipped x2 is a box downscale of it.
const UPSCALE = 4;
const MODEL = "realesrgan-x4plus";
const PRESET = "-medium";
// Textures per Real-ESRGAN process; see the banner.
const UPSCALE_BATCH = 48;
const FORMATS = [ "bc7", "astc" ];
// The world object families (#2061: world objects first). Terrain
// lightmaps are authored gradient data and stay out, as do the variants
// tree and the pack containers.
const WORLD_OBJECT_TEXTURE = /^\/assets\/world\/[^/]+\/object-textures\//;

/*
================
options
================
*/
function options( argv ) {
	const out = {
		jobs: Math.max( 1, cpus().length - 1 ),
		only: "/assets/",
		limit: Infinity,
		out: null
	};
	for ( let i = 0; i < argv.length; i++ ) {
		const value = argv[i + 1];
		if ( argv[i] === "--jobs" ) out.jobs = Math.max( 1, Number( value ) ), i++;
		else if ( argv[i] === "--only" ) out.only = value, i++;
		else if ( argv[i] === "--limit" ) out.limit = Number( value ), i++;
		else if ( argv[i] === "--out" ) out.out = path.resolve( value ), i++;
		else throw new Error( `Unknown option ${argv[i]}` );
	}
	return out;
}

/*
================
worldObjectTextures

Every published world object texture below /assets/world/, as
[file, public path].
================
*/
function worldObjectTextures( root, only ) {
	const out = [];
	const walk = directory => {
		for ( const entry of readdirSync( directory, { withFileTypes: true } ) ) {
			const file = path.join( directory, entry.name );
			if ( entry.isDirectory() ) {
				walk( file );
				continue;
			}
			if ( !entry.name.endsWith( ".texture" ) ) continue;
			const publicPath = "/" + path.relative( root, file ).split( path.sep ).join( "/" );
			if (
				WORLD_OBJECT_TEXTURE.test( publicPath ) && publicPath.startsWith( only ) &&
				hdTextureVariantPath( publicPath, "bc7" )
			) {
				out.push( [ file, publicPath ] );
			}
		}
	};
	walk( path.join( root, "assets", "world" ) );
	return out.sort( ( a, b ) => a[1].localeCompare( b[1] ) );
}

/*
================
tga

An RGBA level as a 32-bit top-left TGA (the encoders' simplest input).
================
*/
function tga( rgba, width, height ) {
	const out = Buffer.alloc( TGA_HEADER_BYTES + width * height * 4 );
	out[2] = TGA_TRUE_COLOUR;
	out.writeUInt16LE( width, 12 );
	out.writeUInt16LE( height, 14 );
	out[16] = 32;
	out[17] = TGA_DESCRIPTOR;
	for ( let i = 0; i < width * height; i++ ) {
		const at = TGA_HEADER_BYTES + i * 4;
		out[at] = rgba[i * 4 + 2];
		out[at + 1] = rgba[i * 4 + 1];
		out[at + 2] = rgba[i * 4];
		out[at + 3] = rgba[i * 4 + 3];
	}
	return out;
}

/*
================
readTga

The RGBA pixels of the uncompressed 32-bit TGAs texconv writes, rows
top first whatever origin the header names.
================
*/
function readTga( bytes ) {
	if ( bytes.length < TGA_HEADER_BYTES || bytes[2] !== TGA_TRUE_COLOUR || bytes[1] !== 0 || bytes[16] !== 32 ) {
		throw new Error( "Unexpected TGA layout" );
	}
	const width = bytes.readUInt16LE( 12 ), height = bytes.readUInt16LE( 14 );
	// Pixels follow the header and its image ID; texconv appends a TGA 2.0
	// footer after them, so the file runs longer than the pixel bytes.
	const pixels = TGA_HEADER_BYTES + bytes[0];
	if ( bytes.length < pixels + width * height * 4 ) {
		throw new Error( `TGA size ${bytes.length} does not hold ${width}x${height}x4` );
	}
	const topOrigin = (bytes[17] & 0x20) !== 0, rightOrigin = (bytes[17] & 0x10) !== 0;
	const rgba = Buffer.alloc( width * height * 4 );
	for ( let y = 0; y < height; y++ ) {
		const sourceY = topOrigin ? y : height - 1 - y;
		for ( let x = 0; x < width; x++ ) {
			const sourceX = rightOrigin ? width - 1 - x : x;
			const from = pixels + (sourceY * width + sourceX) * 4, to = (y * width + x) * 4;
			rgba[to] = bytes[from + 2];
			rgba[to + 1] = bytes[from + 1];
			rgba[to + 2] = bytes[from];
			rgba[to + 3] = bytes[from + 3];
		}
	}
	return { rgba, width, height };
}

/*
================
downscaleLevel

One mip step: each texel the rounded mean of its 2x2 parents. The chain
is all powers of two, so every parent block is whole.
================
*/
function downscaleLevel( rgba, width, height ) {
	const outWidth = Math.max( 1, width >> 1 ), outHeight = Math.max( 1, height >> 1 );
	const out = Buffer.alloc( outWidth * outHeight * 4 );
	for ( let y = 0; y < outHeight; y++ ) {
		for ( let x = 0; x < outWidth; x++ ) {
			const to = (y * outWidth + x) * 4;
			for ( let channel = 0; channel < 4; channel++ ) {
				let sum = 2;
				for ( let dy = 0; dy < 2; dy++ ) {
					for ( let dx = 0; dx < 2; dx++ ) {
						const px = Math.min( width - 1, x * 2 + dx ), py = Math.min( height - 1, y * 2 + dy );
						sum += rgba[(py * width + px) * 4 + channel];
					}
				}
				out[to + channel] = sum >> 2;
			}
		}
	}
	return out;
}

/*
================
upscaledPickAlpha

The upscaled level 0's alpha as a trailing NTX section, or an empty
buffer when every texel is opaque (pickAlphaSection's rule, on the
pixels the variant's GPU actually samples).
================
*/
function upscaledPickAlpha( rgba, width, height ) {
	const alpha = Buffer.alloc( width * height );
	let opaque = true;
	for ( let i = 0; i < alpha.length; i++ ) {
		alpha[i] = rgba[i * 4 + 3];
		if ( alpha[i] !== 255 ) opaque = false;
	}
	if ( opaque ) return Buffer.alloc( 0 );
	const tag = Buffer.alloc( 4 );
	tag.writeUInt32LE( NTX_PICK_ALPHA, 0 );
	return Buffer.concat( [ tag, alpha ] );
}

/*
================
ntx
================
*/
function ntx( width, height, code, levels ) {
	const header = Buffer.alloc( NTX_HEADER_BYTES );
	header.writeUInt32LE( NTX_MAGIC, 0 );
	header.writeUInt32LE( width, 4 );
	header.writeUInt32LE( height, 8 );
	header.writeUInt32LE( code, 12 );
	header.writeUInt32LE( levels.length, 16 );
	return Buffer.concat( [ header, ...levels ] );
}

/*
================
bc7LevelBytes
================
*/
function bc7LevelBytes( width, height ) {
	return Math.ceil( width / BLOCK ) * Math.ceil( height / BLOCK ) * BLOCK_BYTES;
}

/*
================
ddsBc7Level

The one BC7 level inside a texconv DDS: DX10 header, then exactly the
level's blocks.
================
*/
function ddsBc7Level( bytes, width, height ) {
	const expected = DDS_HEADER_BYTES + bc7LevelBytes( width, height );
	if (
		bytes.length !== expected || bytes.subarray( 0, 4 ).toString( "latin1" ) !== "DDS " ||
		bytes.subarray( 84, 88 ).toString( "latin1" ) !== "DX10" ||
		bytes.readUInt32LE( 128 ) !== DXGI_FORMAT_BC7_UNORM
	) {
		throw new Error( `Unexpected texconv DDS (${bytes.length} bytes for ${width}x${height})` );
	}
	return bytes.subarray( DDS_HEADER_BYTES );
}

/*
================
encodeBatch

One batch of cache misses through the chain in the banner: level-0 TGAs,
one texconv PNG bridge, one Real-ESRGAN folder run, one texconv run
back, then the per-level encodes. Writes each finished variant to the
cache and publishes it.
================
*/
async function encodeBatch( batch, tools, cacheRoot, outputRoot, jobs ) {
	const scratch = mkdtempSync( path.join( cacheRoot, ".scratch-" ) );
	try {
		const inDir = path.join( scratch, "in" ),
			pngDir = path.join( scratch, "png" ),
			upDir = path.join( scratch, "up" ),
			upTgaDir = path.join( scratch, "uptga" ),
			levelDir = path.join( scratch, "levels" ),
			ddsDir = path.join( scratch, "dds" );
		for ( const dir of [ inDir, pngDir, upDir, upTgaDir, levelDir, ddsDir ] ) mkdirSync( dir );

		// Colour and alpha upscale as separate opaque images (the banner's
		// alpha rule): <i>.tga carries the colour with alpha forced opaque,
		// <i>-a.tga the alpha as grey. Both ride the same folder run.
		const inputs = [];
		for ( const [index, item] of batch.entries() ) {
			const level0 = decodeNativeTextureLevel( item.texture, 0 );
			const colour = Buffer.from( level0 ), grey = Buffer.alloc( level0.length );
			item.opaque = true;
			for ( let texel = 0; texel < item.texture.width * item.texture.height; texel++ ) {
				colour[texel * 4 + 3] = 255;
				grey[texel * 4] = grey[texel * 4 + 1] = grey[texel * 4 + 2] = level0[texel * 4 + 3];
				grey[texel * 4 + 3] = 255;
				if ( level0[texel * 4 + 3] !== 255 ) item.opaque = false;
			}
			const colourFile = path.join( inDir, `${index}.tga` ), greyFile = path.join( inDir, `${index}-a.tga` );
			writeFileSync( colourFile, tga( colour, item.texture.width, item.texture.height ) );
			writeFileSync( greyFile, tga( grey, item.texture.width, item.texture.height ) );
			inputs.push( colourFile, greyFile );
		}
		await run( tools.texconv, [ "-nogpu", "-ft", "png", "-o", pngDir, ...inputs, "-y" ], { maxBuffer: 64 << 20 } );
		await run( tools.upscaler, [
			"-i",
			pngDir,
			"-o",
			upDir,
			"-s",
			String( UPSCALE ),
			"-n",
			MODEL,
			"-m",
			tools.models
		], { maxBuffer: 64 << 20 } );
		const upscaled = [];
		for ( let index = 0; index < batch.length; index++ ) {
			upscaled.push( path.join( upDir, `${index}.png` ), path.join( upDir, `${index}-a.png` ) );
		}
		await run( tools.texconv, [ "-nogpu", "-ft", "tga", "-o", upTgaDir, ...upscaled, "-y" ], {
			maxBuffer: 64 << 20
		} );

		// Every level of every texture as a TGA first, so both encoders run
		// as batches over files that all exist.
		const bc7Levels = [], astcLevels = [];
		for ( const [index, item] of batch.entries() ) {
			const up = readTga( readFileSync( path.join( upTgaDir, `${index}.tga` ) ) );
			const grey = readTga( readFileSync( path.join( upTgaDir, `${index}-a.tga` ) ) );
			const rawWidth = item.texture.width * UPSCALE, rawHeight = item.texture.height * UPSCALE;
			if (
				up.width !== rawWidth || up.height !== rawHeight || grey.width !== rawWidth || grey.height !== rawHeight
			) {
				throw new Error( `Upscaled size ${up.width}x${up.height} is not x${UPSCALE} of ${item.publicPath}` );
			}
			// The shipped x2 is a box downscale of the native x4 (the
			// banner's scale rule), for the colour and the grey alpha alike.
			let colour = up.rgba, alpha = grey.rgba, width = rawWidth, height = rawHeight;
			while ( width > item.width ) {
				colour = downscaleLevel( colour, width, height );
				alpha = downscaleLevel( alpha, width, height );
				width >>= 1;
				height >>= 1;
			}
			// Recombine: the colour pass's RGB, the grey pass's luminance as
			// alpha (its channels agree; the mean rides out any tint).
			for ( let texel = 0; texel < item.width * item.height; texel++ ) {
				const at = texel * 4;
				colour[at + 3] = Math.round( (alpha[at] + alpha[at + 1] + alpha[at + 2]) / 3 );
			}
			// The mask decision is the source's, as in the ASTC set: an
			// opaque source carries none, whatever the network's ringing
			// does to a flat field; a transparent one's mask values are
			// the upscaled alpha.
			item.mask = item.opaque ? Buffer.alloc( 0 ) : upscaledPickAlpha( colour, item.width, item.height );
			const chain = [ colour ];
			while ( chain.length < item.levelCount ) {
				const level = chain.length;
				chain.push(
					downscaleLevel(
						chain[level - 1],
						Math.max( 1, item.width >> (level - 1) ),
						Math.max( 1, item.height >> (level - 1) )
					)
				);
			}
			item.bc7 = item.need.bc7 ? [] : null;
			item.astc = item.need.astc ? [] : null;
			for ( let level = 0; level < item.levelCount; level++ ) {
				const width = Math.max( 1, item.width >> level ), height = Math.max( 1, item.height >> level );
				const file = path.join( levelDir, `${index}-${level}.tga` );
				writeFileSync( file, tga( chain[level], width, height ) );
				if ( item.need.bc7 ) bc7Levels.push( { item, level, file, width, height } );
				if ( item.need.astc ) astcLevels.push( { item, level, file, width, height } );
			}
		}

		if ( bc7Levels.length ) {
			await run( tools.texconv, [
				"-nogpu",
				"-f",
				"BC7_UNORM",
				"-m",
				"1",
				"-ft",
				"dds",
				"-o",
				ddsDir,
				...bc7Levels.map( entry => entry.file ),
				"-y"
			], { maxBuffer: 64 << 20 } );
			for ( const entry of bc7Levels ) {
				const dds = path.join( ddsDir, path.basename( entry.file, ".tga" ) + ".dds" );
				entry.item.bc7[entry.level] = ddsBc7Level( readFileSync( dds ), entry.width, entry.height );
			}
		}

		let nextAstc = 0;
		/*
		================
		astcWorker
		================
		*/
		async function astcWorker() {
			while ( nextAstc < astcLevels.length ) {
				const entry = astcLevels[nextAstc++];
				const output = entry.file + ".astc";
				await run( tools.astcenc, [ "-cl", entry.file, output, "4x4", PRESET, "-silent" ], {
					maxBuffer: 64 << 20
				} );
				const file = readFileSync( output );
				if ( file.readUInt32LE( 0 ) !== ASTC_MAGIC || file[4] !== BLOCK || file[5] !== BLOCK ) {
					throw new Error( "Unexpected astcenc output" );
				}
				entry.item.astc[entry.level] = file.subarray( ASTC_HEADER_BYTES );
			}
		}
		await Promise.all( Array.from( { length: Math.min( jobs, Math.max( 1, astcLevels.length ) ) }, astcWorker ) );

		for ( const item of batch ) {
			for ( const format of FORMATS ) {
				if ( !item.need[format] ) continue;
				const code = format === "bc7" ? NTX_BC7 : NTX_ASTC_4X4;
				const variant = Buffer.concat( [ ntx( item.width, item.height, code, item[format] ), item.mask ] );
				writeFileSync( path.join( cacheRoot, `${item.digest}.${format}.ntx` ), variant );
				item.publish( format, variant );
			}
		}
	} finally {
		rmSync( scratch, { recursive: true, force: true } );
	}
}

/*
================
buildHdTextureVariants

The pass, for the resource build (resourceBuild.mjs) and the CLI below.
Only the windows-x64 tools are pinned: elsewhere the pass is skipped and
clients read the originals (the ASTC pass's rule).
================
*/
export async function buildHdTextureVariants( flags = {} ) {
	flags = {
		jobs: Math.max( 1, cpus().length - 1 ),
		only: "/assets/",
		limit: Infinity,
		out: null,
		publicRoot: CLIENT_PUBLIC_ROOT,
		...flags
	};
	if ( process.platform !== "win32" ) {
		console.log( "HD texture variants: skipped (the pinned tools are windows-x64 only)" );
		return { skipped: true };
	}
	const tools = {
		texconv: await encoderPath( "texconv" ),
		upscaler: await encoderPath( "realesrgan" ),
		astcenc: await encoderPath( "astcenc" )
	};
	tools.models = path.join( path.dirname( tools.upscaler ), "models" );
	const cacheName = `hd-x${SCALE}-${MODEL}-${ENCODERS.realesrgan.version}` +
		`-texconv-${ENCODERS.texconv.version}-astc-${ENCODERS.astcenc.version}${PRESET}`;
	const cacheRoot = flags.out ?
		path.join( flags.out, "intermediate", "hd-texture-variants", cacheName ) :
		generatedPath( "intermediate", "hd-texture-variants", cacheName );
	const outputRoot = flags.out ? path.join( flags.out, "client-public" ) : flags.publicRoot;
	mkdirSync( cacheRoot, { recursive: true } );

	const textures = worldObjectTextures( flags.publicRoot, flags.only ).slice( 0, flags.limit );
	let encoded = 0, cached = 0, kept = 0, masked = 0, sourceBytes = 0, variantBytes = 0;
	const startedAt = performance.now();
	const misses = [];
	/*
	================
	publish

	Writes one finished variant to its published path under the HD root.
	================
	*/
	const publish = ( outputRoot, publicPath, format, variant ) => {
		const target = path.join( outputRoot, ...hdTextureVariantPath( publicPath, format ).split( "/" ) );
		mkdirSync( path.dirname( target ), { recursive: true } );
		writeFileSync( target, variant );
		variantBytes += variant.length;
	};

	for ( const [file, publicPath] of textures ) {
		const source = readFileSync( file ), digest = createHash( "sha256" ).update( source ).digest( "hex" );
		const texture = decodeNativeTexture( new Uint8Array( source ) );
		sourceBytes += source.length;
		const width = texture.width * SCALE, height = texture.height * SCALE;
		if ( width % BLOCK || height % BLOCK || width > MAX_DIMENSION || height > MAX_DIMENSION ) {
			kept++;
			continue;
		}
		const item = {
			publicPath,
			digest,
			texture,
			width,
			height,
			levelCount: 1 + Math.floor( Math.log2( Math.max( width, height ) ) ),
			need: {},
			mask: Buffer.alloc( 0 ),
			publish: ( format, variant ) => publish( outputRoot, publicPath, format, variant )
		};
		let missing = false;
		for ( const format of FORMATS ) {
			const cacheFile = path.join( cacheRoot, `${digest}.${format}.ntx` );
			item.need[format] = !existsSync( cacheFile );
			if ( item.need[format] ) missing = true;
		}
		if ( !missing ) {
			for ( const format of FORMATS ) {
				publish(
					outputRoot,
					publicPath,
					format,
					readFileSync( path.join( cacheRoot, `${digest}.${format}.ntx` ) )
				);
			}
			cached++;
			continue;
		}
		misses.push( item );
	}

	for ( let start = 0; start < misses.length; start += UPSCALE_BATCH ) {
		const batch = misses.slice( start, start + UPSCALE_BATCH );
		await encodeBatch( batch, tools, cacheRoot, outputRoot, flags.jobs );
		for ( const item of batch ) {
			encoded++;
			if ( item.mask.length ) masked++;
		}
		console.log( `${Math.min( start + UPSCALE_BATCH, misses.length )}/${misses.length} HD textures` );
	}

	const mb = bytes => (bytes / 1048576).toFixed( 1 );
	console.log(
		`HD texture variants (x${SCALE} ${MODEL}, bc7 + astc 4x4 ${PRESET}): ${textures.length} textures, ` +
			`${encoded} encoded, ${cached} cached, ${kept} not whole blocks kept their original, ` +
			`${masked} carry a picking mask, ${mb( sourceBytes )} MB BC -> ${mb( variantBytes )} MB HD, ` +
			`${((performance.now() - startedAt) / 1000).toFixed( 1 )} s`
	);
	return { textures: textures.length, encoded, cached, kept, masked, sourceBytes, variantBytes };
}

if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	await buildHdTextureVariants( options( process.argv.slice( 2 ) ) );
}
