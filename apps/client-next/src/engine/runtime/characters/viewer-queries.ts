/*
===========================================================================

viewer-queries.ts - the 3D viewer's read-only window on the presentation

Port-only, not native (docs/VIEWER.md). The viewer drives the character
presentation like the world does, but must also tell a model the catalogue
lacks from one still loading, type a look's worn items, and wait until a
dressed actor wears its parts. These answers read characters.ts's state;
the game never calls them.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { CharacterActor } from "@/engine/contracts/character";
import type { ItemPresentation, Resource } from "./internal/presentation-contract";

/*
================
ViewerQuerySources

What the queries read, all owned by characters.ts.
================
*/
export interface ViewerQuerySources {
	readonly published: {
		readonly manifest: number;
		readonly manifests: readonly string[];
		readonly items: Readonly<Record<string, ItemPresentation>>;
	};
	readonly appearances: ReadonlyMap<number, { readonly parts: readonly unknown[]; readonly model: string; }>;
	// The frame's published results: read live, since a frame replaces its map.
	readonly output: { readonly displayed: ReadonlyMap<number, CharacterActor>; };
}

/*
================
createViewerQueries

resourceFor is characters.ts's appearance lookup (appearance-lookup.ts).
================
*/
export function createViewerQueries(
	sources: ViewerQuerySources,
	resourceFor: ( entity: EntityState ) => Resource | undefined
) {
	const { published, appearances, output } = sources;
	return {
		/*
		================
		knows

		Whether the catalogue has the entity's model; undefined until every
		manifest is admitted.
		================
		*/
		knows( entity: EntityState ): boolean | undefined {
			return published.manifest < published.manifests.length ?
				undefined :
				resourceFor( entity ) !== undefined;
		},
		/*
		================
		itemType

		An item's packed type word, once the catalogue has it.
		================
		*/
		itemType( refObjId: number ): number | undefined {
			return published.items[String( refObjId )]?.typeFlags;
		},
		/*
		================
		dressed

		Every worn part is resident and the actor wears them: until then the
		actor shows its fallback model (actor-appearance.ts).
		================
		*/
		dressed( gid: number ): boolean {
			const appearance = appearances.get( gid );
			return !appearance?.parts.length || output.displayed.get( gid )?.model === appearance.model;
		}
	};
}
