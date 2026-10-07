/*
===========================================================================

operations.js - operator observations and population summaries

The journal compares snapshots without claiming authoritative kill history.
A truncated ordinary census must not hide complete unique observations.

===========================================================================
*/
import { escape, fmt, isUnique } from "./model.js";

/*
================
uniqueCensusComplete

The ordinary census can truncate while all living uniques remain visible.
Older snapshots lack the independent count and still require a full census.
================
*/
function uniqueCensusComplete( population ) {
	return !population.truncated || (population.uniqueAlive !== undefined &&
		population.monsters.filter( m => isUnique( m ) && m.hp > 0 ).length === population.uniqueAlive);
}

// Browser-session observations, not authoritative spawn/kill history.
/*
================
createJournal
================
*/
export function createJournal( limit = 80 ) {
	const realms = new Map();
	return {
		observe( id, data ) {
			let state = realms.get( id );
			if ( !state ) {
				state = { previous: data, events: [] };
				realms.set( id, state );
				return;
			}
			const old = state.previous;
			if ( old.capturedAt === data.capturedAt ) return;
			const events = [];
			if ( data.uptimeSeconds < old.uptimeSeconds ) {
				events.push( "Realm restarted; observation baseline renewed." );
			} else if ( uniqueCensusComplete( old.population ) && uniqueCensusComplete( data.population ) ) {
				const before = new Map(
					old.population.monsters.filter( m => isUnique( m ) && m.hp > 0 ).map( m => [ m.gid, m ] )
				);
				const after = new Map(
					data.population.monsters.filter( m => isUnique( m ) && m.hp > 0 ).map( m => [ m.gid, m ] )
				);
				for ( const [gid, m] of after ) {
					if ( !before.has( gid ) ) {
						events.push( `${m.name} entered the observed population.` );
					}
				}
				for ( const [gid, m] of before ) {
					if ( !after.has( gid ) ) {
						events.push( `${m.name} left the observed population; cause unknown.` );
					}
				}
				const change = data.players.length - old.players.length;
				if ( change ) {
					events.push(
						`Online population ${change > 0 ? "increased" : "decreased"} by ${
							Math.abs( change )
						} to ${data.players.length}.`
					);
				}
			}
			state.events = [ ...events.map( message => ({ at: data.capturedAt, message }) ), ...state.events ].slice(
				0,
				limit
			);
			state.previous = data;
		},
		rows( id ) {
			return realms.get( id )?.events ?? [];
		}
	};
}

/*
================
hotspots
================
*/
export function hotspots( data ) {
	const regions = new Map();
	const row = id => {
		let r = regions.get( id );
		if ( !r ) {
			r = { id, players: 0, monsters: 0, uniques: 0, engaged: 0 };
			regions.set( id, r );
		}
		return r;
	};
	for ( const p of data.players ) row( p.region ).players++;
	for ( const m of data.population.monsters ) {
		const r = row( m.region );
		r.monsters++;
		if ( m.hp > 0 && isUnique( m ) ) r.uniques++;
		if ( m.hp > 0 && m.target ) r.engaged++;
	}
	return [ ...regions.values() ].sort( ( a, b ) => b.players - a.players || b.monsters - a.monsters || a.id - b.id );
}

/*
================
realmStrip
================
*/
export function realmStrip( shards, selected ) {
	return shards.map( s =>
		`<button class="realm ${s.id === selected ? "selected" : ""}" data-shard="${
			escape( s.id )
		}"><span class="realm-gem ${s.connected ? "" : "offline"}"></span><span><strong>${
			escape( s.name )
		}</strong><small>${
			s.connected ? `${fmt( s.data.players.length )} / ${fmt( s.capacity )} adventurers` : "Unavailable"
		}</small></span><span class="realm-state">${s.connected ? "ONLINE" : "OFFLINE"}</span></button>`
	).join( "" );
}
/*
================
hotspotView
================
*/
export function hotspotView( data ) {
	const rows = hotspots( data ).slice( 0, 8 );
	return rows.length ?
		rows.map( r =>
			`<button class="hotspot" data-region="${r.id}"><span>Sector <strong>${r.id}</strong>${
				r.id & 0x8000 ? " · Interior" : ""
			}</span><span>${fmt( r.players )} players · ${fmt( r.monsters )} creatures<small>${
				fmt( r.uniques )
			} uniques · ${fmt( r.engaged )} engaged →</small></span></button>`
		).join( "" ) :
		'<p class="panel-note">No resident population yet.</p>';
}
/*
================
journalView
================
*/
export function journalView( rows ) {
	return rows.length ?
		rows.map( e =>
			`<div class="journal-entry"><time>${escape( new Date( e.at ).toLocaleTimeString() )}</time><span>${
				escape( e.message )
			}</span></div>`
		).join( "" ) :
		'<p class="panel-note">Watching for population changes, unique arrivals and realm restarts. The first capture establishes a baseline.</p>';
}
/*
================
alerts
================
*/
export function alerts( shard, data ) {
	const items = [];
	if ( !shard?.connected ) items.push( "Realm unavailable. Values may be stale." );
	if ( data && (data.storage.LastError || data.storage.FailedWrites > 0) ) {
		items.push( "Persistence reports an error. Inspect Runtime before restarting the realm." );
	}
	if ( data?.population.truncated ) items.push( "Census is partial: entity capture limit reached." );
	if ( data && Date.now() - Date.parse( data.capturedAt ) > 15000 ) items.push( "Capture is over 15 seconds old." );
	return items.map( message => `<div class="operator-alert">${escape( message )}</div>` ).join( "" );
}
