/** Bounds guard shared by character, object, and world JMX parsers. */
export function ensureAvailable( buffer, offset, byteCount, sourcePath = "<jmx>" ) {
	if (
		!Number.isSafeInteger( offset ) ||
		!Number.isSafeInteger( byteCount ) ||
		offset < 0 ||
		byteCount < 0 ||
		byteCount > buffer.length - offset
	) {
		throw new Error( `${sourcePath}: tried to read ${byteCount} bytes at ${offset}, past ${buffer.length}` );
	}
}

/** Offset-tracking, bounds-checked reader for little-endian JMX payloads. */
export class BinaryReader {
	constructor( buffer, sourcePath = "<jmx>", offset = 0 ) {
		this.buffer = buffer;
		this.sourcePath = sourcePath;
		this.sourceName = sourcePath;
		this.offset = offset;
		this.ensure( 0, "initial offset" );
	}

	get pos() {
		return this.offset;
	}

	ensure( byteLength, label = "data" ) {
		try {
			ensureAvailable( this.buffer, this.offset, byteLength, this.sourcePath );
		} catch {
			throw new Error( `${this.sourcePath}: truncated ${label} at ${this.offset}/${this.buffer.length}` );
		}
	}

	seek( offset, label = "seek", { allowBackward = false } = {} ) {
		if (
			!Number.isSafeInteger( offset ) ||
			offset < 0 ||
			offset > this.buffer.length ||
			(!allowBackward && offset < this.offset)
		) {
			throw new Error( `${this.sourcePath}: invalid ${label} offset ${offset}` );
		}
		this.offset = offset;
	}

	skip( byteLength, label = "data" ) {
		this.ensure( byteLength, label );
		this.offset += byteLength;
	}

	bytes( byteLength, label = "data" ) {
		this.ensure( byteLength, label );
		const value = this.buffer.subarray( this.offset, this.offset + byteLength );
		this.offset += byteLength;
		return value;
	}

	/** JMX text is CP949 (see decodeJmxText); pass an encoding only for non-JMX byte text. */
	text( byteLength, label = "text", encoding = null ) {
		const bytes = this.bytes( byteLength, label );
		return encoding ? bytes.toString( encoding ) : decodeJmxText( bytes );
	}

	u8( label = "u8" ) {
		this.ensure( 1, label );
		return this.buffer[this.offset++];
	}

	i16( label = "i16" ) {
		this.ensure( 2, label );
		const value = this.buffer.readInt16LE( this.offset );
		this.offset += 2;
		return value;
	}

	u16( label = "u16" ) {
		this.ensure( 2, label );
		const value = this.buffer.readUInt16LE( this.offset );
		this.offset += 2;
		return value;
	}

	i32( label = "i32" ) {
		this.ensure( 4, label );
		const value = this.buffer.readInt32LE( this.offset );
		this.offset += 4;
		return value;
	}

	u32( label = "u32" ) {
		this.ensure( 4, label );
		const value = this.buffer.readUInt32LE( this.offset );
		this.offset += 4;
		return value;
	}

	f32( label = "f32" ) {
		this.ensure( 4, label );
		const value = this.buffer.readFloatLE( this.offset );
		this.offset += 4;
		return value;
	}

	count( label, maximum, { signed = false } = {} ) {
		const value = signed ? this.i32( label ) : this.u32( label );
		if ( value < 0 || value > maximum ) {
			throw new Error( `${this.sourcePath}: ${label} ${value} exceeds ${maximum}` );
		}
		return value;
	}

	str( label = "string", maximum = this.buffer.length, { signedLength = false } = {} ) {
		const byteLength = this.count( `${label} length`, maximum, { signed: signedLength } );
		return this.text( byteLength, label );
	}

	skipCountedWords( label ) {
		this.skip( this.count( `${label} count`, 1 << 20 ) * 4, label );
	}
}

/** Validate a fixed-width JMX magic header. */
export function readJmxSignature( buffer, expectedSignature, sourcePath = "<jmx>" ) {
	const signatureBytes = expectedSignature.length;
	if ( buffer.length < signatureBytes ) {
		throw new Error( `${sourcePath}: too small to contain ${expectedSignature} signature` );
	}
	const signature = buffer.subarray( 0, signatureBytes ).toString( "latin1" );
	if ( signature !== expectedSignature ) {
		throw new Error( `${sourcePath}: expected ${expectedSignature}, got ${signature}` );
	}
	return signature;
}

// JMX resources store names and paths as CP949 (the v1.150 client's ANSI
// codepage); the extracted pk2 file names are decoded the same way. Decoding
// as Latin-1 kept the bytes but produced names like `Áß½Éº®` that never match
// the extracted `중심벽.ddj`. ASCII is returned unchanged (fast path).
const cp949 = new TextDecoder( "euc-kr" );

/** Decode JMX text bytes (CP949) to the Unicode name the extracted tree uses. */
export function decodeJmxText( bytes ) {
	for ( let i = 0; i < bytes.length; i++ ) {
		if ( bytes[i] >= 0x80 ) {
			return cp949.decode( bytes );
		}
	}
	return Buffer.from( bytes.buffer, bytes.byteOffset, bytes.byteLength ).toString( "latin1" );
}

/** Read the u32-length-prefixed CP949 strings used throughout JMX resources. */
export function readCountedString( buffer, offset, sourcePath = "<jmx>" ) {
	ensureAvailable( buffer, offset, 4, sourcePath );
	const byteLength = buffer.readUInt32LE( offset );
	const stringOffset = offset + 4;
	ensureAvailable( buffer, stringOffset, byteLength, sourcePath );
	const nextOffset = stringOffset + byteLength;
	return {
		value: decodeJmxText( buffer.subarray( stringOffset, nextOffset ) ),
		byteLength,
		byteOffset: offset,
		nextOffset
	};
}

/** Read an ordered little-endian u32 array with one shared bounds check. */
export function readUInt32Array( buffer, offset, count, sourcePath = "<jmx>" ) {
	ensureAvailable( buffer, offset, count * 4, sourcePath );
	return Array.from( { length: count }, ( _, index ) => buffer.readUInt32LE( offset + index * 4 ) );
}
