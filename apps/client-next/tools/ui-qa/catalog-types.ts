/*
===========================================================================
catalog-types.ts - portable resource inventory and source identity boundary
===========================================================================
*/
export interface Resource {
	name: string;
	nativeHash: string | null;
	publishedHash: string | null;
	url: string | null;
	references: string[];
	sections: {
		name: string;
		nodes: {
			id: number;
			name: string;
			type: string;
			rect: { x: number; y: number; width: number; height: number; };
			occurrence: number;
		}[];
	}[];
}
export interface Catalog {
	identity: {
		codeSha: string;
		sourceHash: string;
		assetHash: string;
		defineHash: string | null;
		fontHash: string | null;
		fingerprint: string;
		dirty: boolean;
	};
	roots: Record<string, string>;
	resources: Resource[];
	limitations: string;
}
