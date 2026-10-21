/*
===========================================================================
ui-entry-evidence.d.mts - target-specific mount evidence contract
===========================================================================
*/
export const ENTRY_WITNESSES: Record<string, readonly string[]>;
export function entryEvidence( name: string, controls: readonly { id: string; }[] ): {
	required: readonly string[];
	missing: readonly string[];
	admitted: boolean;
	entryFlow: string;
};
