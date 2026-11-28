/*
===========================================================================

cull-commands.mjs - the GPU cull commands of a fake geometry owner

A world or terrain layer test with a hand-made GeometryCommands spreads
these in: the renderer hands every terrain layer draw to the GPU cull
(index-cull.ts) and gives it the view each frame, which a fake that only
records uploads has no use for. A test of the cull itself drives the real
geometry owner instead.

===========================================================================
*/
export const CULL_COMMANDS = Object.freeze( {
	cullDraw() {},
	cullSource() {},
	cullRanges() {},
	cullView() {}
} );
