/** Encode exactly the visible byte window of an ArrayBuffer view. */
export function typedArrayToBase64( values ) {
	if ( !ArrayBuffer.isView( values ) ) {
		throw new TypeError( "typedArrayToBase64 expects an ArrayBuffer view" );
	}
	return Buffer.from( values.buffer, values.byteOffset, values.byteLength ).toString( "base64" );
}
