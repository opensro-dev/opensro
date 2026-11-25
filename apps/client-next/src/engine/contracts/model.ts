export type ModelJson = null | boolean | number | string | readonly ModelJson[] | {
	readonly [key: string]: ModelJson;
};
export interface ModelDocument {
	readonly json: ModelJson;
	readonly binary: ArrayBuffer;
}
