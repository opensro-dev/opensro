/*
===========================================================================

model.ts - the parsed-model shapes the engine contracts share

ModelJson is any JSON-shaped value, the form parsed model and config
documents arrive in; ModelDocument pairs that JSON with the binary
payload it was decoded beside. Types only: the loaders that produce
these own the parsing and validation.

===========================================================================
*/
export type ModelJson = null | boolean | number | string | readonly ModelJson[] | {
	readonly [key: string]: ModelJson;
};
export interface ModelDocument {
	readonly json: ModelJson;
	readonly binary: ArrayBuffer;
}
