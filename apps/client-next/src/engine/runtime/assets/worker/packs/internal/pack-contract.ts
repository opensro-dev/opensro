export interface PackEntry {
	publicPath: string;
	path: string;
	packPath: string;
	offset: number;
	length: number;
	mime: string;
	sha256: string;
	animationSources?: string[];
	stored?: { length: number; encoding: "gzip"; };
	span: number;
}
export interface PackDescriptor {
	path: string;
	bytes: number;
	sha256: string;
	assetCount: number;
	entries: PackEntry[];
	load?: string;
}
export type PackRange = { start: number; end: number; total: number; };
export type PackDownload = (
	url: string,
	limit: number,
	signal: AbortSignal,
	range?: PackRange,
	// The response's HTTP validator (its ETag), reported on a whole-object
	// read so the packs manifest's admitted copy can later be compared
	// against the live one without downloading it again (#3104).
	observeValidator?: ( validator: string | null ) => void
) => Promise<Uint8Array<ArrayBuffer>>;
