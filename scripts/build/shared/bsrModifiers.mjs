// Shared native CModDataSetList decoding for character and scenery publishers.
import { readCountedString as readStr } from "./jmxBinaryReader.mjs";
function skipCountedString( buf, p ) {
	const len = buf.readUInt32LE( p );
	if ( len > 0x1000 || p + 4 + len > buf.length ) {
		throw new Error( `modDataSet: implausible string len ${len} @${p}` );
	}
	return p + 4 + len;
}

// One IModData payload, dispatched on the u32 typeId exactly as the native factory
// ModData_CreateByTypeId (sub_a75980) does: hi16 = class, lo16 = subclass.
function skipModPayload( buf, p, typeId, modifiers, setMeta ) {
	const baseOffset = p;
	if ( p + 28 > buf.length ) throw new Error( "truncated IModData base" );
	const base = () => ({
		baseWords: Array.from( { length: 6 }, ( _, i ) => buf.readUInt32LE( baseOffset + i * 4 ) ),
		baseBytes: Array.from( buf.subarray( baseOffset + 24, baseOffset + 28 ) )
	});
	p += 28; // IModData_LoadBase: 6x u32 + 4x u8
	const hi = typeId >>> 16, lo = typeId & 0xffff;
	if ( hi === 0 && lo === 0 ) { // ModDataMtrl (sub_acb7e0)
		const field24 = buf.readUInt32LE( p ), flags = buf.readUInt32LE( p + 4 ), mode = buf.readUInt32LE( p + 8 );
		p += 12;
		const count = buf.readUInt32LE( p );
		p += 4;
		if ( count > 65536 || p + count * 20 > buf.length ) throw Error( "Invalid material color keys" );
		const colors = [];
		for ( let i = 0; i < count; i++ ) {
			colors.push( {
				time: buf.readUInt32LE( p ),
				value: Array.from( { length: 4 }, ( _, j ) => buf.readFloatLE( p + 4 + j * 4 ) )
			} );
			p += 20;
		}
		const scalars = [];
		if ( flags & 4 ) {
			const count = buf.readUInt32LE( p );
			p += 4;
			if ( count > 65536 || p + count * 8 > buf.length ) throw Error( "Invalid material scalar keys" );
			for ( let i = 0; i < count; i++ ) {
				scalars.push( { time: buf.readUInt32LE( p ), value: buf.readFloatLE( p + 4 ) } );
				p += 8;
			}
		}
		if ( p + 36 > buf.length ) throw Error( "Truncated material state" );
		const words50 = Array.from( { length: 4 }, ( _, i ) => buf.readUInt32LE( p + i * 4 ) ),
			bytes60 = Array.from( buf.subarray( p + 16, p + 32 ) ),
			field70 = buf.readUInt32LE( p + 32 );
		p += 36;
		modifiers.materialModifiers.push( {
			...setMeta,
			...base(),
			field24,
			flags,
			mode,
			colors,
			scalars,
			words50,
			bytes60,
			field70
		} );
	} else if ( hi === 1 && lo === 0 ) { // ModDataTexAni (sub_ac58b0)
		if ( p + 84 > buf.length ) throw Error( "Truncated texture animation" );
		modifiers.textureModifiers.push( {
			...setMeta,
			...base(),
			words24: Array.from( { length: 5 }, ( _, i ) => buf.readUInt32LE( p + i * 4 ) ),
			matrix38: Array.from( { length: 16 }, ( _, i ) => buf.readFloatLE( p + 20 + i * 4 ) )
		} );
		p += 84;
	} else if ( hi === 1 && (lo === 1 || lo === 2) ) { // ModDataMultiTex(Rev)
		p += 4;
		p = skipCountedString( buf, p );
		p += 4;
	} else if ( hi === 3 && lo === 0 ) { // ModDataParticle (sub_acd4d0)
		const n = buf.readUInt32LE( p );
		p += 4;
		const entries = [];
		for ( let i = 0; i < n; i += 1 ) {
			const field00 = buf.readUInt32LE( p );
			p += 4;
			const effectEnd = skipCountedString( buf, p );
			const effect = readStr( buf, p );
			p = effectEnd;
			const boneEnd = skipCountedString( buf, p );
			const bone = readStr( buf, p );
			p = boneEnd;
			const vector3c = [ buf.readFloatLE( p ), buf.readFloatLE( p + 4 ), buf.readFloatLE( p + 8 ) ];
			p += 12;
			const field4c = buf.readUInt32LE( p );
			p += 4;
			const flags50 = [ buf.readUInt8( p ), buf.readUInt8( p + 1 ), buf.readUInt8( p + 2 ) ];
			p += 3;
			const flag53 = buf.readUInt8( p );
			p += 1;
			const vector54 = flag53 ?
				[ buf.readFloatLE( p ), buf.readFloatLE( p + 4 ), buf.readFloatLE( p + 8 ) ] :
				null;
			if ( flag53 ) p += 12;
			entries.push( {
				field00,
				effectPath: effect.value,
				boneName: bone.value,
				vector3c,
				field4c,
				flags50,
				flag53,
				vector54
			} );
		}
		modifiers.particleModifiers.push( {
			...setMeta,
			baseWords: Array.from( { length: 6 }, ( _, index ) => buf.readUInt32LE( baseOffset + index * 4 ) ),
			baseBytes: Array.from( buf.subarray( baseOffset + 24, baseOffset + 28 ) ),
			entries
		} );
	} else if ( hi === 4 && lo === 0 ) { // ModDataEnvMap (sub_ac57f0)
		if ( p + 16 > buf.length ) throw Error( "Truncated environment-map modifier" );
		modifiers.environmentModifiers.push( {
			...setMeta,
			...base(),
			words24: Array.from( { length: 4 }, ( _, i ) => buf.readUInt32LE( p + i * 4 ) )
		} );
		p += 16;
	} else if ( hi === 4 && lo === 1 ) { // ModDataBumpEnv (sub_acbe10)
		p += 16 + 24;
		const n = buf.readUInt32LE( p );
		p += 4;
		for ( let i = 0; i < n; i += 1 ) {
			const flag = buf.readUInt8( p );
			p += 1;
			if ( flag === 1 ) p = skipCountedString( buf, p );
		}
	} else if ( hi === 5 && lo === 0 ) { // ModDataSound (sub_acd050)
		const n = buf.readUInt32LE( p );
		p += 4;
		const entries = [];
		if ( n > 0 ) {
			p += 44; // ModDataSound_ReadParams (sub_a2bab0)
			for ( let i = 0; i < n; i += 1 ) {
				const animationName = readStr( buf, p );
				p = animationName.nextOffset;
				const nTracks = buf.readUInt32LE( p );
				p += 4; // assert <= 15 native
				const tracks = [];
				for ( let t = 0; t < nTracks; t += 1 ) {
					const flag = buf.readUInt32LE( p );
					p += 4;
					if ( flag === 1 ) {
						const sourcePath = readStr( buf, p );
						p = sourcePath.nextOffset;
						const triggerFrame = buf.readInt32LE( p );
						p += 4;
						const cueName = readStr( buf, p );
						p = cueName.nextOffset;
						tracks.push( {
							sourcePath: sourcePath.value,
							triggerFrame,
							cueName: cueName.value
						} );
					}
				}
				entries.push( { animationName: animationName.value, tracks } );
			}
		}
		modifiers.soundModifiers.push( { ...setMeta, entries } );
	} else if ( (hi === 6 && lo <= 2) || (hi === 7 && lo === 0) ) {
		// ModDataDyVertex/DyJoint/DyLattice/ProgEquipPow: LoadBase only
	} else {
		throw new Error( `modDataSet: unknown mod typeId 0x${typeId.toString( 16 )} @${p}` );
	}
	return p;
}

// CModDataSet_Load (sub_a75b60): u32, u32, counted name, u32 nMods, mods.
function skipModDataSet( buf, p, modifiers ) {
	const kind = buf.readUInt32LE( p );
	p += 4;
	const stateId = buf.readInt32LE( p );
	p += 4;
	const name = readStr( buf, p );
	p = name.nextOffset;
	const nMods = buf.readUInt32LE( p );
	p += 4;
	const setRow = { kind, stateId, animationSetName: name.value, count: nMods, firstBaseWord4: 0 };
	modifiers.modifierSets.push( setRow );
	let firstType = Infinity;
	for ( let i = 0; i < nMods; i += 1 ) {
		const typeId = buf.readUInt32LE( p );
		p += 4;
		if ( typeId <= firstType ) {
			firstType = typeId;
			setRow.firstBaseWord4 = buf.readUInt32LE( p + 16 );
		}
		p = skipModPayload( buf, p, typeId, modifiers, {
			kind,
			stateId,
			animationSetName: name.value
		} );
	}
	return p;
}

// CModDataSetList_Load (sub_a75c60): u32 countA entries, u32 countB entries,
// both lists use the same CModDataSet_Load record shape.
export function parseModDataSection( buf, off ) {
	let p = off;
	const modifiers = {
		modifierSets: [],
		soundModifiers: [],
		particleModifiers: [],
		materialModifiers: [],
		textureModifiers: [],
		environmentModifiers: []
	};
	const countA = buf.readUInt32LE( p );
	p += 4;
	for ( let i = 0; i < countA; i += 1 ) p = skipModDataSet( buf, p, modifiers );
	const countB = buf.readUInt32LE( p );
	p += 4;
	for ( let i = 0; i < countB; i += 1 ) p = skipModDataSet( buf, p, modifiers );
	return { next: p, ...modifiers };
}
