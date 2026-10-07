/*
===========================================================================

crowd-models.mjs - native browser asset admission shared by renderer fixtures

Preserves materials, native mip chains and assembly parts. Asset hashes are
reported alongside results. GLB models and native effect programs use their
shipping decoders; unsupported identities fail explicitly.

===========================================================================
*/
const ASSET_TIMEOUT_MS = 30000;
const MAX_MODEL_BYTES = 128 * 1024 * 1024;
const MAX_EFFECT_IMAGES_BYTES = 32 * 1024 * 1024;
const EFFECT_PROGRAMS = "/assets/effects/programs.json";

/*
================
loadCrowdModels

The caller owns the renderer; dispose it before closing returned bitmaps.
================
*/
export async function loadCrowdModels( renderer, actors ) {
	const { createModelDecoder } = await import( "/src/engine/runtime/assets/worker/model/model.ts" );
	const { createEffectDecoder } = await import( "/src/engine/runtime/assets/worker/effects/effects.ts" );
	const { decodeNativeClip } = await import( "/src/engine/foundation/animation/native-clip.ts" );
	const { readBytes } = await import( "/src/engine/foundation/assets/read-bytes.ts" );
	const { decodeNativeTexture, NATIVE_TEXTURE_MIME } = await import(
		"/src/engine/foundation/assets/native-texture.ts"
	);
	const assets = [], bitmaps = [], bytesByPath = new Map();
	const requests = new Map(), clipsByModel = new Map(), catalogs = new Map();
	for ( const actor of actors ) {
		let clips = requests.get( actor.model );
		if ( !clips ) requests.set( actor.model, clips = new Set() );
		clips.add( actor.clip );
		for ( const layer of actor.layers ?? [] ) clips.add( layer.clip );
	}
	const effects = createEffectDecoder();
	/*
	================
	readAsset

	Effect programs and image paths are shared by many models. Retain bytes only
	during admission, hash each resource once, and never include asset bytes in
	the capture itself.
	================
	*/
	async function readAsset( path ) {
		if ( !path.startsWith( "/assets/" ) || path.includes( ".." ) || /[\\?#]/.test( path ) ) {
			throw Error( `Invalid captured asset: ${path}` );
		}
		if ( bytesByPath.has( path ) ) return bytesByPath.get( path );
		const response = await fetch( path, { signal: AbortSignal.timeout( ASSET_TIMEOUT_MS ) } );
		if ( !response.ok || !response.body ) throw Error( `Asset HTTP ${response.status}: ${path}` );
		if ( Number( response.headers.get( "content-length" ) ) > MAX_MODEL_BYTES ) {
			throw Error( `Asset exceeds byte limit: ${path}` );
		}
		const bytes = await readBytes( response.body, MAX_MODEL_BYTES );
		const digest = new Uint8Array( await crypto.subtle.digest( "SHA-256", bytes ) );
		assets.push( { path, sha256: Array.from( digest, n => n.toString( 16 ).padStart( 2, "0" ) ).join( "" ) } );
		bytesByPath.set( path, bytes );
		return bytes;
	}
	/*
	================
	readCatalog
	================
	*/
	async function readCatalog( path ) {
		if ( !catalogs.has( path ) ) {
			catalogs.set( path, JSON.parse( new TextDecoder().decode( await readAsset( path ) ) ) );
		}
		return catalogs.get( path );
	}
	/*
	================
	admitMotions

	Recorded clip identities may be streamed BANs, not embedded GLB clips.
	Use the published body mapping and the same binding owner as live admission.
	================
	*/
	async function admitMotions( id, requested ) {
		const admitted = clipsByModel.get( id );
		for ( const clip of requested ) {
			if ( admitted.has( clip ) ) continue;
			const role = /^native:([^:]+):([0-9]+)$/.exec( clip );
			if ( !role ) throw Error( `Missing captured animation ${clip}: ${id}` );
			const catalogPath = id.startsWith( "/assets/char/" ) ?
				"/assets/char/roster.json" :
				"/assets/npc/manifest.json";
			const catalog = await readCatalog( catalogPath );
			const body = Object.values( catalog.models ).find( row => row.glb === id );
			const motions = await readCatalog( "/assets/anim/manifest.json" );
			const path = motions.models[body?.codename]?.animationSets?.[role[1]]?.[role[2]]?.url;
			if ( typeof path !== "string" || !path.startsWith( "/assets/anim/" ) || !path.endsWith( ".ban" ) ) {
				throw Error( `Unpublished captured animation ${clip}: ${id}` );
			}
			renderer.setCharacterAnimation( id, clip, decodeNativeClip( await readAsset( path ) ) );
			admitted.add( clip );
		}
	}
	try {
		const decoder = createModelDecoder(), admitted = new Set();
		/*
		================
		admit
		================
		*/
		async function admit( id, requested = [] ) {
			if ( id.startsWith( "assembly:" ) ) {
				const separator = id.indexOf( ":[" );
				if ( separator < 0 ) throw Error( "Unsupported assembly identity" );
				const base = id.slice( "assembly:".length, separator );
				const parts = JSON.parse( id.slice( separator + 1 ) );
				await admit( base, requested );
				if ( admitted.has( id ) ) return;
				for ( const part of parts ) await admit( part.model );
				renderer.setCharacterAssembly( id, base, parts );
			} else {
				if ( admitted.has( id ) ) {
					await admitMotions( id, requested );
					return;
				}
				let source;
				if ( id.startsWith( EFFECT_PROGRAMS + "#" ) ) {
					const decoded = effects.model(
						await readAsset( EFFECT_PROGRAMS ),
						decodeURIComponent( id.slice( EFFECT_PROGRAMS.length + 1 ) )
					);
					const raw = [];
					let imageBytes = 0;
					for ( const path of decoded.imagePaths ) {
						const bytes = await readAsset( path );
						imageBytes += bytes.length;
						if ( imageBytes > MAX_EFFECT_IMAGES_BYTES ) throw Error( "Effect images exceed byte limit" );
						raw.push( { bytes, mime: "image/png" } );
					}
					source = { ...decoded.model, images: raw };
				} else {
					if ( !/^\/assets\/[^?#]+\.glb$/.test( id ) ) throw Error( `Unsupported captured model: ${id}` );
					source = decoder.character( decoder.decode( await readAsset( id ) ) );
				}
				const images = [];
				for ( const image of source.images ) {
					if ( image.mime === NATIVE_TEXTURE_MIME ) images.push( decodeNativeTexture( image.bytes ) );
					else if ( image.mime === "image/png" ) {
						const bitmap = await createImageBitmap( new Blob( [ image.bytes ], { type: image.mime } ), {
							premultiplyAlpha: "none",
							colorSpaceConversion: "none"
						} );
						images.push( bitmap );
						bitmaps.push( bitmap );
					} else throw Error( `Unsupported captured texture: ${image.mime}` );
				}
				renderer.setCharacterModel( id, {
					...source,
					images: images.map( image => ({ width: image.width, height: image.height }) )
				}, images );
				clipsByModel.set( id, new Set( source.clips.map( clip => clip.name ) ) );
				await admitMotions( id, requested );
			}
			admitted.add( id );
		}
		for ( const [id, clips] of requests ) await admit( id, clips );
		return {
			assets,
			/*
			================
			close
			================
			*/
			close() {
				for ( const bitmap of bitmaps ) bitmap.close();
			}
		};
	} catch ( error ) {
		for ( const bitmap of bitmaps ) bitmap.close();
		throw error;
	} finally {
		bytesByPath.clear();
		effects.dispose();
	}
}
