/*
===========================================================================

meshCloth.mjs - preserve the native BMS dynamic geometry streams

===========================================================================
*/

/*
================
readMeshCloth

A85F80 reads vertex mobility/pin pairs, distance constraints, their traversal
order, and the 36-byte force parameters. A77530 consumes these exact fields.
================
*/
export function readMeshCloth( bytes, offsets, vertexCount, flipZ = true ) {
	const vertices = offsets[3], edges = offsets[4];
	if ( !vertices || !edges ) return undefined;
	const count = bytes.readUInt32LE( vertices ), edgeCount = bytes.readUInt32LE( edges );
	if ( !count || !edgeCount ) return undefined;
	if ( count !== vertexCount || vertices + 4 + count * 8 > edges || edges + 4 + edgeCount * 16 + 36 > bytes.length ) {
		throw Error( "Invalid BMS cloth streams" );
	}
	const mobility = [], pins = [], constraints = [], order = [];
	for ( let i = 0; i < count; i++ ) {
		mobility.push( bytes.readFloatLE( vertices + 4 + i * 8 ) );
		pins.push( bytes.readUInt32LE( vertices + 8 + i * 8 ) );
	}
	for ( let i = 0; i < edgeCount; i++ ) {
		const at = edges + 4 + i * 12;
		constraints.push( [ bytes.readUInt32LE( at ), bytes.readUInt32LE( at + 4 ), bytes.readFloatLE( at + 8 ) ] );
		order.push( bytes.readUInt32LE( edges + 4 + edgeCount * 12 + i * 4 ) );
	}
	const params = edges + 4 + edgeCount * 16;
	return {
		mobility,
		pins,
		constraints,
		order,
		force: bytes.readUInt32LE( params ) ?
			[
				bytes.readFloatLE( params + 4 ),
				bytes.readFloatLE( params + 8 ),
				bytes.readFloatLE( params + 12 ) * (flipZ ? -1 : 1)
			] :
			null,
		gravity: bytes.readFloatLE( params + 16 ),
		gravityMobility: bytes.readFloatLE( params + 20 ),
		windMobility: bytes.readFloatLE( params + 24 ),
		damping: bytes.readFloatLE( params + 28 ),
		windPeriod: bytes.readUInt32LE( params + 32 )
	};
}
