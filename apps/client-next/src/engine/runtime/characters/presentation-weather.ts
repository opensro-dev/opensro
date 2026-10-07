/*
===========================================================================

presentation-weather.ts - the event-rain lifecycle journal

Some characters bring rain with them (a catalogue row's eventRain). World
lifecycle events arrive before the character catalogues may be admitted, so
they are journalled in delivery order and replayed once the NPC catalogue is
resident: rain starts when such a character spawns or changes into one, and
stops only when that character despawns or the world resets (changing away
from a rain model leaves the rain on, as the original journal does).

A replayed world reset also resets the presentation owners that a reset
retires; the presentation owner passes that work in, and it runs before this
journal's own state clears, as it always has.

===========================================================================
*/
import type { Resource } from "./internal/presentation-contract";

/*
================
WeatherLifecycleEvent

The part of a world lifecycle event the rain journal keeps.
================
*/
export type WeatherLifecycleEvent =
	| { kind: "spawn" | "state"; gid: number; refObjId: number; }
	| { kind: "despawn"; gid: number; }
	| { kind: "reset"; };

/*
================
WeatherCatalog

The catalogue reads the replay needs: admission progress and eventRain rows.
================
*/
export interface WeatherCatalog {
	readonly manifest: number;
	readonly catalog: ReadonlyMap<number, Pick<Resource, "eventRain">>;
}

// Bounds the journal while the catalogues are still loading.
const MAX_JOURNALLED_EVENTS = 65536;

/*
================
createPresentationWeather
================
*/
export function createPresentationWeather() {
	let rainEventActive = false;
	const rainEventEntities = new Map<number, number>();
	const rainEvents: WeatherLifecycleEvent[] = [];
	return {
		/*
		================
		receive

		Append one delivery's lifecycle events; a reset discards what came before.
		================
		*/
		receive( next: readonly WeatherLifecycleEvent[] ) {
			if ( next[0]?.kind === "reset" ) rainEvents.length = 0;
			if ( rainEvents.length + next.length > MAX_JOURNALLED_EVENTS ) {
				throw Error( "Weather lifecycle journal overflow" );
			}
			for ( const event of next ) rainEvents.push( event );
		},
		/*
		================
		eventRain

		Replay the journal into the rain state once the NPC catalogue is admitted.
		================
		*/
		eventRain( published: WeatherCatalog, resetPresentation: () => void ) {
			// Preserve delivery order while the character catalogs load.
			if ( published.manifest < 2 ) return rainEventActive;
			for ( const event of rainEvents ) {
				if ( event.kind === "reset" ) {
					resetPresentation();
					rainEventActive = false;
					rainEventEntities.clear();
				} else if ( event.kind === "despawn" ) {
					if ( published.catalog.get( rainEventEntities.get( event.gid ) ?? 0 )?.eventRain ) {
						rainEventActive = false;
					}
					rainEventEntities.delete( event.gid );
				} else {
					if (
						rainEventEntities.get( event.gid ) !== event.refObjId &&
						published.catalog.get( event.refObjId )?.eventRain
					) rainEventActive = true;
					rainEventEntities.set( event.gid, event.refObjId );
				}
			}
			rainEvents.length = 0;
			return rainEventActive;
		},
		/*
		================
		reset
		================
		*/
		reset() {
			rainEventActive = false;
			rainEventEntities.clear();
			rainEvents.length = 0;
		}
	};
}
