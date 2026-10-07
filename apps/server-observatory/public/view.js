/*
===========================================================================

view.js - read-only operator dashboard rendering

Render population and transport snapshots without changing game state.
Unique totals use the server census, independent of the capped monster list.

===========================================================================
*/
import { escape as e, fmt, coordinates, grade, isUnique, byteSize, uptime } from "./model.js";
/*
================
empty
================
*/
export const empty = ( title, subtitle ) => `<div class="empty"><strong>${e( title )}</strong>${e( subtitle )}</div>`;
/*
================
row
================
*/
const row = ( label, value, bad = false ) =>
	`<div class="health-row ${bad ? "bad" : ""}"><span>${e( label )}</span><span>${e( value )}</span></div>`;
/*
================
position
================
*/
const position = m => {
	const p = coordinates( m );
	return `${p.x.toFixed( 0 )}, ${p.y.toFixed( 0 )}${p.indoor ? " · indoor" : ""}`;
};
/*
================
metrics
================
*/
export function metrics( shard ) {
	const d = shard?.data, t = d?.transport, p = d?.population;
	const uniqueCount = p ? (p.uniqueAlive ?? p.monsters.filter( m => isUnique( m ) && m.hp > 0 ).length) : null;
	const partialUniqueCount = p && p.uniqueAlive === undefined && p.truncated;
	const cards = [
		[
			"Adventurers online",
			d ? fmt( d.players.length ) : "—",
			`of ${fmt( shard?.capacity )} slots`,
			`${d ? fmt( d.registeredCharacters ) : "—"} registered characters`,
			"♙"
		],
		[
			"World monsters",
			p ? fmt( p.resident ) : "—",
			"",
			`${p ? fmt( p.regions ) : "—"} regions · includes sleeping monsters`,
			"♧"
		],
		[
			"Uniques alive",
			p ? fmt( uniqueCount ) + (partialUniqueCount ? "+" : "") : "—",
			"",
			partialUniqueCount ? "Partial census · at least this many" : "All living uniques on this shard",
			"✧"
		],
		[
			"World tick",
			t ? t.tick_last_ms.toFixed( 2 ) : "—",
			"ms",
			t ? `${fmt( t.tick_overruns )} overruns since boot` : "Awaiting server telemetry",
			"⌁"
		]
	];
	return cards.map( ( [label, value, unit, foot, icon] ) =>
		`<article class="metric"><span class="metric-icon">${icon}</span><div class="metric-label">${label}</div><div class="metric-number">${value}<small>${
			e( unit )
		}</small></div><div class="metric-foot">${e( foot )}</div></article>`
	).join( "" );
}
/*
================
encounters
================
*/
export function encounters( monsters ) {
	const uniques = monsters.filter( m => isUnique( m ) && m.hp > 0 );
	return uniques.length ?
		uniques.map( m =>
			`<div class="encounter"><div class="crest">✧</div><div><button class="link-button encounter-name" data-entity="${m.gid}">${
				e( m.name || "Unnamed unique" )
			}</button><div class="encounter-info">Lv. ${m.level} <span> / </span> ${
				position( m )
			}</div></div><div class="encounter-state">${
				m.target ? "ENGAGED" : "ROAMING"
			}<div class="hp-track"><span style="width:${
				Math.min( 100, m.hp / Math.max( 1, m.maxHp ) * 100 )
			}%"></span></div></div></div>`
		).join( "" ) :
		empty( "A quiet horizon", "No living unique is currently resident on this shard." );
}
/*
================
atlas
================
*/
export function atlas( monsters, players ) {
	const points = [
		...monsters.filter( m => m.hp > 0 && !(m.region & 0x8000) ).map( m => ({
			...coordinates( m ),
			id: m.gid,
			kind: isUnique( m ) ? "unique" : "monster",
			name: m.name
		}) ),
		...players.filter( p => !(p.region & 0x8000) ).map( p => ({
			...coordinates( p ),
			id: p.id,
			kind: "player",
			name: p.name
		}) )
	];
	if ( !points.length ) {
		return empty( "The world is quiet", "Outdoor positions will appear as regions become active." );
	}
	const xs = points.map( p => p.x ),
		ys = points.map( p => p.y ),
		minX = Math.min( ...xs ),
		maxX = Math.max( ...xs ),
		minY = Math.min( ...ys ),
		maxY = Math.max( ...ys ),
		scale = Math.min( 640 / Math.max( 192, maxX - minX ), 240 / Math.max( 192, maxY - minY ) );
	const x = v => 360 + (v - (minX + maxX) / 2) * scale, y = v => 155 - (v - (minY + maxY) / 2) * scale;
	// Aggregate ordinary monsters into screen cells: every actor contributes,
	// while SVG element count is bounded independently of world population.
	const cells = new Map();
	for ( const p of points.filter( p => p.kind === "monster" ) ) {
		const a = Math.round( x( p.x ) / 7 ) * 7, b = Math.round( y( p.y ) / 7 ) * 7, k = a + ":" + b;
		const c = cells.get( k ) ?? { x: a, y: b, count: 0 };
		c.count++;
		cells.set( k, c );
	}
	const cloud = [ ...cells.values() ].map( c =>
		`<circle cx="${c.x}" cy="${c.y}" r="${
			Math.min( 6, 1.5 + Math.log2( c.count + 1 ) )
		}" fill="#86b499" opacity=".5"><title>${c.count} monsters in this display cell</title></circle>`
	).join( "" );
	const markers = points.filter( p => p.kind !== "monster" ).map( p =>
		`<circle class="map-point" ${p.kind === "unique" ? `data-entity="${p.id}"` : ""} cx="${x( p.x )}" cy="${
			y( p.y )
		}" r="${p.kind === "unique" ? 5 : 3}" fill="${
			p.kind === "unique" ? "#e2c38c" : "#ffffff"
		}" stroke="#15252a" stroke-width="1.5"><title>${e( p.name )} · ${p.x.toFixed( 0 )}, ${
			p.y.toFixed( 0 )
		}</title></circle>`
	).join( "" );
	return `<svg viewBox="0 0 720 310" role="img" aria-label="Live outdoor monster density and player positions"><defs><pattern id="grid" width="36" height="31" patternUnits="userSpaceOnUse"><path d="M36 0H0V31" fill="none" stroke="#8aa89b" stroke-opacity=".09"/></pattern></defs><rect width="720" height="310" fill="url(#grid)"/><path d="M360 20V290M25 155H695" stroke="#8aa89b" stroke-opacity=".12" stroke-dasharray="3 5"/>${cloud}${markers}<text x="683" y="30" fill="#b9c8ba" font-size="10" font-family="Segoe UI">N ↑</text><text x="24" y="27" fill="#829f94" font-size="9" font-family="Segoe UI">${
		minX.toFixed( 0 )
	} … ${maxX.toFixed( 0 )} X / ${minY.toFixed( 0 )} … ${
		maxY.toFixed( 0 )
	} Y</text></svg><span class="atlas-caption">${
		fmt( points.length )
	} outdoor actors · density cells<br>Extent follows active populations; indoor regions excluded.</span>`;
}
/*
================
chart
================
*/
export function chart( history ) {
	if ( history.length < 2 ) {
		return empty( "Listening to the world", "The timeline begins with your second snapshot." );
	}
	const max = Math.max( 1, ...history.map( s => s.tick ) ),
		low = history[0].at,
		range = Math.max( 1, history.at( -1 ).at - low ),
		path = history.map( ( s, i ) =>
			`${i ? "L" : "M"}${20 + (s.at - low) / range * 670},${112 - s.tick / max * 90}`
		).join( " " );
	return `<svg viewBox="0 0 720 140" preserveAspectRatio="none" role="img" aria-label="World tick duration over time"><path d="M20 22H690M20 67H690M20 112H690" stroke="#304047" stroke-dasharray="3 5"/><path d="${path} L690 112 L20 112Z" fill="#82bea1" opacity=".06"/><path d="${path}" fill="none" stroke="#82bea1" stroke-width="2" vector-effect="non-scaling-stroke"/><text x="690" y="17" text-anchor="end" fill="#8ba29a" font-size="9">${
		max.toFixed( 1 )
	} ms</text></svg>`;
}
/*
================
healthSummary
================
*/
export function healthSummary( d ) {
	return row(
		"Transport",
		`${fmt( d.transport.attached_sessions )} attached · ${fmt( d.transport.outbound_queue_depth )} queued`
	) + row( "Persistence", d.storage.FailedWrites ? "Degraded" : "Healthy", d.storage.FailedWrites > 0 ) +
		row( "World uptime", uptime( d.uptimeSeconds ) ) +
		row( "Snapshot collection", `${d.captureMs.toFixed( 2 )} ms` );
}
/*
================
monsterTable
================
*/
export function monsterTable( monsters ) {
	return monsters.length ?
		`<table><thead><tr><th>Creature / identity</th><th>Level</th><th>Grade</th><th>Health</th><th>Position X, Y</th><th>AI state</th></tr></thead><tbody>${
			monsters.map( m =>
				`<tr><td><button class="link-button" data-entity="${m.gid}">${
					e( m.name || "Unnamed creature" )
				}</button><small>GID ${m.gid} · Ref ${m.ref}</small></td><td>${m.level}</td><td><span class="badge ${
					isUnique( m ) ? "unique" : ""
				}">${grade( m.rarity )}${m.rarity >>> 4 === 1 ? " · Party" : ""}</span></td><td>${
					fmt( m.hp )
				}<small>/ ${fmt( m.maxHp )}</small></td><td>${position( m )}<small>Region ${m.region}</small></td><td>${
					e( m.mode )
				}<small>${m.target ? "Target " + m.target : "No target"}</small></td></tr>`
			).join( "" )
		}</tbody></table>` :
		empty( "No matching creatures", "Try another name, identity or grade." );
}
/*
================
playerTable
================
*/
export function playerTable( players ) {
	return players.length ?
		`<table><thead><tr><th>Character</th><th>Level</th><th>State</th><th>HP / MP</th><th>Position X, Y</th></tr></thead><tbody>${
			players.map( p =>
				`<tr><td>${e( p.name )}<small>ID ${p.id}</small></td><td>${p.level}</td><td><span class="badge ${
					p.alive ? "" : "dead"
				}">${p.alive ? "Active" : "Not combat eligible"}</span></td><td>${fmt( p.hp )} / ${
					fmt( p.mp )
				}</td><td>${position( p )}<small>Region ${p.region}</small></td></tr>`
			).join( "" )
		}</tbody></table>` :
		empty( "No adventurers here", "Only characters with an active world session are listed." );
}
/*
================
runtimePanels
================
*/
export function runtimePanels( d, last ) {
	const t = d.transport, m = d.runtime.metrics;
	const panel = ( title, rows ) =>
		`<article class="panel"><div class="panel-heading"><div><div class="eyebrow">LIVE SERVER TELEMETRY</div><h2>${title}</h2></div></div>${rows}</article>`;
	return panel(
		"Simulation",
		row( "Latest tick", t.tick_last_ms.toFixed( 2 ) + " ms" ) +
			row( "Mean tick since boot", t.tick_mean_ms.toFixed( 2 ) + " ms" ) +
			row( "Maximum tick since boot", t.tick_max_ms.toFixed( 2 ) + " ms" ) +
			row( "Completed ticks", fmt( t.tick_count ) ) +
			row( "Tick overruns", fmt( t.tick_overruns ), t.tick_overruns > 0 )
	) +
		panel(
			"Go runtime",
			row( "Version", d.runtime.goVersion ) +
				row( "Heap objects", byteSize( m["/memory/classes/heap/objects:bytes"] ) ) +
				row( "Total allocations since boot", byteSize( m["/gc/heap/allocs:bytes"] ) ) +
				row( "GC cycles", fmt( m["/gc/cycles/total:gc-cycles"] ) ) +
				row( "Goroutines / parallelism", `${d.runtime.goroutines} / ${d.runtime.parallelism}` )
		) +
		panel(
			"Network & queues",
			row( "Outbound throughput", last?.outRate === null ? "Collecting" : byteSize( last?.outRate ) + "/s" ) +
				row( "Inbound throughput", last?.inRate === null ? "Collecting" : byteSize( last?.inRate ) + "/s" ) +
				row( "Outbound queued bytes", byteSize( t.outbound_queue_bytes ) ) +
				row( "Write errors", fmt( t.write_errors ), t.write_errors > 0 ) +
				row(
					"Slow consumer disconnects",
					fmt( t.sessions_closed_slow_consumer ),
					t.sessions_closed_slow_consumer > 0
				) + row( "Unhandled frames", fmt( t.unhandled_opcode_frames ), t.unhandled_opcode_frames > 0 )
		) +
		panel(
			"World & persistence",
			row( "Authored nests", fmt( d.population.nests ) ) +
				row( "Activated regions", fmt( d.population.regions ) ) +
				row( "Queued respawn entries", fmt( d.population.respawns ) ) +
				row( "Failed writes", fmt( d.storage.FailedWrites ), d.storage.FailedWrites > 0 ) +
				row(
					"Last commit",
					d.storage.LastCommitAt.startsWith( "0001" ) ?
						"No commit yet" :
						new Date( d.storage.LastCommitAt ).toLocaleTimeString()
				) + row( "Loaded from backup", d.storage.LoadedFromBak ? "Yes" : "No" )
		);
}
/*
================
detail
================
*/
export function detail( m ) {
	return `<div class="eyebrow">ENTITY INSPECTOR / GID ${m.gid}</div><h2>${e( m.name )}</h2>${
		row( "Reference", m.ref )
	}${row( "Level / grade", m.level + " / " + grade( m.rarity ) )}${
		row( "Health", fmt( m.hp ) + " / " + fmt( m.maxHp ) )
	}${row( "Map X, Y", position( m ) )}${row( "Region", m.region )}${
		row( "Local X / height / Z", `${m.x.toFixed( 2 )} / ${m.y.toFixed( 2 )} / ${m.z.toFixed( 2 )}` )
	}${row( "AI / target", m.mode + " / " + (m.target || "none") )}`;
}
