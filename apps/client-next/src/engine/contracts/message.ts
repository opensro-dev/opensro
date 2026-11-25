/*
===========================================================================

message.ts - the catalog message contract

A catalog lookup is identified by its key and suffix; the numeric format
arguments travel separately from the text so a catalog entry is never
mistaken for a finished line.

===========================================================================
*/

/*
================
CatalogMessage
================
*/
export interface CatalogMessage {
	readonly key: string;
	readonly suffix: string;
	readonly args?: readonly number[];
}
