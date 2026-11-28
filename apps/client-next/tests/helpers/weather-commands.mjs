/*
===========================================================================

weather-commands.mjs - the GPU weather commands of a fake geometry owner

A world test with a hand-made GeometryCommands spreads these in: weather
hands its drops to the resident GPU pool (device/weather-particles.ts),
which a fake that only records uploads has no use for. A test of the
drops themselves uses tests/helpers/weather-reference.mjs instead.

===========================================================================
*/
export const WEATHER_COMMANDS = Object.freeze( {
	weatherSpawn() {},
	weatherCorners() {},
	weatherStep() {
		return true;
	}
} );
