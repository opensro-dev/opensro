/*
===========================================================================

characterEnvironment.mjs - authored secondary reflection texture binding

One owner for the v1.150 environment-map modifier (CRTModEnvMap, vtable
C3BE34): which modifiers reach a material, and which native stage setup
it draws with. The item builders and the avatar screen all ask here.

===========================================================================
*/
import { readCharacterTexture } from "./nativeCharacterTextures.mjs";

// AADDC0 actually loads two entries, despite constructing four path strings.
const textures = [ "prim/mtrl/etc/spheremap_gray.ddj", "prim/mtrl/etc/spheremap_highlight.ddj" ];

// ModData base word 1: the pass that calls the modifier. AB08C0
// (CMaterialAnimController_BeginMaterialPass) calls only kind 2 records,
// once per material subset; general records cover every material.
const SCOPE_GENERAL = 1;
const SCOPE_MATERIAL = 2;
const ALL_MATERIALS = 0xffffffff;

// Base word 5 bit 0 picks the stage setup inside AEE6D0. Set: the metal
// stages (TFACTOR sphere, MODULATEALPHA_ADDCOLOR, MODULATE2X). Clear: the
// table AEEAD0 fills at +0x44 (entry 0 AEE560, entry 1 AEE430), chosen by
// words24[2].
const BLEND_METAL = 1;
const TABLE_TEXTURE_OP = 1;
const D3DTOP_ADD = 7;

// Published stage contracts (the client's Material.reflection.x).
export const ENVIRONMENT_METAL = 1;
export const ENVIRONMENT_ADD = 2;

/*
================
environmentScope

Validate one modifier's ownership. General records draw every material;
material-pass records draw only the subset whose iMtrlID (A5E0E0's loop
index: the material's index inside its BMT set) equals base word 3.
================
*/
function environmentScope( m ) {
	if ( m.kind === 2 && m.stateId === -1 ) {
		if ( m.baseWords[1] === SCOPE_GENERAL && m.baseWords[3] === ALL_MATERIALS ) return SCOPE_GENERAL;
		if ( m.baseWords[1] === SCOPE_MATERIAL && m.baseWords[3] !== ALL_MATERIALS ) return SCOPE_MATERIAL;
	}
	throw Error( "Animated or material-scoped environment coverage needs runtime ownership" );
}

/*
================
environmentCoverage

The modifiers whose coverage half reaches a material. AEE6D0 ORs
0x60000000 into the material flags and sets the bit-2 alpha test before
it compares base word 3, so a material-pass record changes the coverage
of every subset it is called for, not only its own.
================
*/
export function environmentCoverage( flags, modifiers = [] ) {
	if ( flags & 0x10000 ) return [];
	const scopes = new Set( modifiers.map( environmentScope ) );
	// The two passes run at different points of the draw. No shipped model
	// carries both, so their relative order has no published contract.
	if ( scopes.size > 1 ) throw Error( "Mixed general and material-pass environment modifiers need a pass order" );
	return modifiers;
}

/*
================
characterEnvironment

Validate the native environment modifier before publishing its descriptor.
materialIndex is the material's index in its BMT set. A material-pass
record draws only that subset; the last record called wins, since each
call overwrites the stage state.
================
*/
export function characterEnvironment( flags, modifiers = [], materialIndex ) {
	const staged = environmentCoverage( flags, modifiers ).filter( m =>
		environmentScope( m ) === SCOPE_GENERAL || m.baseWords[3] === materialIndex
	);
	if ( !staged.length ) return undefined;
	const m = staged.at( -1 );
	const textureId = m.words24[0];
	if ( textureId === 0xffffffff ) {
		throw Error( "Unpublished null environment texture requires its own stage contract" );
	}
	if ( !textures[textureId] ) {
		throw Error( "Native environment texture ID outside loaded table" );
	}
	if ( m.baseWords[5] & BLEND_METAL ) return { textureId, mode: ENVIRONMENT_METAL };
	// AEE430: stage 0 SELECTARG1 the sphere with alpha disabled; stage 1
	// applies words24[3] as COLOROP to the base texture and current, alpha
	// from the base. The restore (AEE950) leaves stage 2 DISABLE, so no
	// lighting stage follows. Entry 0 (AEE560, TFACTOR from words24[3]) and
	// other ops ship in no model, so they keep no published contract.
	const entry = m.words24[2], op = m.words24[3];
	if ( entry !== TABLE_TEXTURE_OP || op !== D3DTOP_ADD ) {
		throw Error( `Environment blend table entry ${entry} with colour op ${op} requires its own stage contract` );
	}
	return { textureId, mode: ENVIRONMENT_ADD };
}

// The model image owner carries this secondary texture through assembly,
// residency and device restoration exactly like its base textures.

/*
================
embedCharacterEnvironment

Share secondary textures by authored identity through the model image owner.
================
*/
export function embedCharacterEnvironment( json, material, appendView, cache ) {
	const descriptor = material.extras?.sroEnvironment;
	delete material.extras?.sroEnvironmentTexture;
	if ( !descriptor || descriptor.textureId === 0xffffffff ) return;
	const id = descriptor.textureId;
	let index = cache.get( id );
	if ( index === undefined ) {
		json.images ??= [];
		json.textures ??= [];
		const name = "sro-environment-" + id;
		let source = json.images.findIndex( image => image.name === name );
		if ( source < 0 ) {
			const texture = readCharacterTexture( textures[id] );
			source = json.images.length;
			json.images.push( { name, bufferView: appendView( texture.bytes ), mimeType: texture.mime } );
		}
		index = json.textures.findIndex( texture => texture.source === source );
		if ( index < 0 ) {
			index = json.textures.length;
			json.textures.push( { source, sampler: 0 } );
		}
		cache.set( id, index );
	}
	material.extras.sroEnvironmentTexture = index;
}
