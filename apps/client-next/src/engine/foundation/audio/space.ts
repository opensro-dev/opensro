/*
===========================================================================

space.ts - world coordinates into Web Audio's coordinate system

The world is the native left-handed space (world-math.ts: facing +Z with
up +Y, right is +X), which DirectSound3D shares, so the original client
handed it positions unchanged. Web Audio is right-handed: its listener's
right is forward x up, which for that same pose is -X, so world positions
passed through as they are swap left and right. Mirroring Z on every
position and direction handed to Web Audio restores the native sides and
keeps front, back and up where they were.

===========================================================================
*/

/*
================
audioSpace

A world position or direction as Web Audio reads it. 0 - z, not -z, so an
axis-aligned direction keeps a plain zero rather than -0.
================
*/
export function audioSpace( v: readonly [number, number, number] ): [number, number, number] {
	return [ v[0], v[1], 0 - v[2] ];
}
