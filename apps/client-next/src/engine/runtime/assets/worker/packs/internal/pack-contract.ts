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
	range?: PackRange
) => Promise<Uint8Array<ArrayBuffer>>;
