import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";

// CIFWorldMap: 57F43D swallows map button messages while AUTO MOVE is on in retail;
// client-next intentionally allows town clicks in AUTO, automatically switching to MANUAL.
// MANUAL town clicks run 57F0B0 + 57A570 once; 579920 clamps each drag step;
// 683B40 -> 575D10(1) re-arms AUTO after a world load.
test( "world map follows retail AUTO/MANUAL click, centre and drag rules", { timeout: 90000 }, async () => {
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.setViewportSize( { width: 1200, height: 900 } );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async () => {
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" ),
				{ createUi } = await import( "/src/engine/runtime/ui/ui.ts" );
			const assets = createAssets();
			let scene, semantics, clicks = 0;
			const ui = createUi( {
				assets,
				commands: () => {},
				publish: s => {
					scene = s;
				},
				texture: () => {},
				base: location.origin,
				apiBase: "http://fixture.invalid",
				clickSound: () => {
					clicks++;
				}
			} );
			// Sector 160,97 lies west of Jangan (166..170): the AUTO page is the world map.
			const field = { regionId: 97 * 256 + 160, x: 960, y: 0, z: 960, angle: 0 };
			const state = {
				session: {
					phase: "world",
					revision: 1,
					character: "Explorer",
					characters: [ { name: "Explorer", level: 1, maxHp: 200, maxMp: 200 } ]
				},
				gameplay: {
					localGid: 1,
					pose: field,
					inventory: [],
					vitals: [ { gid: 1, hp: 200, mp: 200 } ],
					casts: [],
					guide: { country: 1, seenMask: 0xffffffff },
					progression: {
						level: 1,
						experience: "0",
						skillPoints: 0,
						skillExperience: 0,
						masteries: [],
						stats: {
							strength: 20,
							intellect: 20,
							physicalMin: 10,
							physicalMax: 20,
							magicalMin: 15,
							magicalMax: 30,
							physicalDefense: 5,
							magicalDefense: 6,
							hit: 7,
							parry: 8,
							maxHp: 200,
							maxMp: 200
						}
					}
				},
				entities: [],
				width: 1200,
				height: 900,
				worldReady: true
			};
			// Fresh gameplay identity + a clock past the 100 ms poll gate: every step
			// renders, as the runtime does when the pose or loading state changes.
			let clock = performance.now();
			const step = () => {
				clock += 200;
				semantics = ui.step( { ...state, gameplay: { ...state.gameplay } }, clock ) ?? semantics;
			};
			async function settle( texture = "/worldmap/map/" ) {
				const end = performance.now() + 30000;
				let stable = 0;
				while ( performance.now() < end ) {
					step();
					if ( ui.stats().pending === 0 && scene?.quads.some( q => q.texture.includes( texture ) ) ) {
						if ( ++stable > 8 ) {
							return;
						}
					} else stable = 0;
					await new Promise( requestAnimationFrame );
				}
				throw Error( texture + " did not settle" );
			}
			const jangan = () => scene.quads.find( q => q.texture.endsWith( "map_jangan.png" ) )?.rect.slice();
			const world = () => scene.quads.find( q => q.texture.includes( "/map_world_" ) )?.rect.slice();
			const follow = () => semantics.controls.find( c => c.id === "map-pin" )?.label;
			const labelLayers = () => {
				const clip = semantics.controls.find( c => c.id === "map-pan" ).rect;
				const labels = scene.quads.flatMap( ( q, i ) =>
					q.texture.startsWith( "/assets/fonts/" ) && JSON.stringify( q.clip ) === JSON.stringify( clip ) ?
						[ i ] :
						[]
				);
				const cities = scene.quads.flatMap( ( q, i ) => q.texture.includes( "/city_" ) ? [ i ] : [] );
				return {
					labels: labels.length,
					cities: cities.length,
					labelsAboveCities: labels.length > 0 && cities.length > 0 &&
						Math.min( ...labels ) > Math.max( ...cities )
				};
			};
			try {
				await settle( "ub_window_01" );
				ui.event( { kind: "key", code: "KeyM" } );
				await settle();
				// Positive control: AUTO recentres on a walking player (57FE60 -> 57A570).
				state.gameplay.pose = { ...field, x: 1400 };
				step();
				const autoWalked = world();
				state.gameplay.pose = field;
				step();
				const autoLabel = follow(), autoWorld = world();
				const janganLabels = labelLayers();
				// In AUTO: drags on the map are swallowed.
				ui.event( { kind: "drag", id: "map-pan", dx: 120, dy: 40 } );
				step();
				const autoDragWorld = world();
				// Divergence from retail: clicking a town icon in AUTO mode opens the town
				// and automatically switches to Manual Movement State (mapFollow = false).
				// A real click jitters: the bridge reports press + 1px move as drag-end
				// over the icon (57F430 still opens a town released over the pressed icon).
				const icon = semantics.controls.find( c => c.id === "map-town:1" ).rect, townBefore = clicks;
				ui.event( { kind: "drag", id: "map-town:1", dx: 1, dy: 0 } );
				ui.event( { kind: "drag-end", id: "map-town:1", x: icon[0] + icon[2] / 2, y: icon[1] + icon[3] / 2 } );
				await settle( "map_jangan.png" );
				const opened = jangan(), townClicks = clicks - townBefore, manualLabel = follow();
				const pan = semantics.controls.find( c => c.id === "map-pan" );
				// While in MANUAL mode, walking does not move the town view.
				state.gameplay.pose = { ...field, x: 1500, z: 400 };
				step();
				const afterWalk = jangan();
				// Drag overshoot stops at the image edge; the reverse step moves at once.
				ui.event( { kind: "drag", id: "map-pan", dx: 5000, dy: 0 } );
				step();
				const pinned = jangan();
				ui.event( { kind: "drag", id: "map-pan", dx: -10, dy: 0 } );
				step();
				const reversed = jangan();
				const clip = semantics.controls.find( c => c.id === "map-pan" ).rect;
				// Switch to world map (keeps MANUAL mode) to test tile streaming.
				ui.event( { kind: "activate", id: "map-world" } );
				step();
				// A fast pan reveals tiles that are not loaded yet. They stream in; the
				// window must stay admitted or the platform bridge cancels the drag.
				const tiles = () =>
					new Map(
						scene.quads.filter( q => q.texture.includes( "/map_world_" ) ).map(
							q => [ q.texture, q.rect[0] ]
						)
					);
				// Sector 160 sits near the east end, so pan west (positive dx) where the
				// page has ~2700 px of unloaded tiles.
				ui.event( { kind: "drag", id: "map-pan", dx: 1500, dy: 0 } );
				step();
				const revealed = tiles(), panDuringLoad = semantics.controls.find( c => c.id === "map-pan" );
				ui.event( { kind: "drag", id: "map-pan", dx: 40, dy: 0 } );
				step();
				const continued = tiles();
				const shared = [ ...revealed.keys() ].find( k => continued.has( k ) ),
					continuedShift = shared ? continued.get( shared ) - revealed.get( shared ) : null;
				// Clicking the follow pin (the renewal window's one follow switch) toggles mode and plays the button click sound.
				const clicksBefore = clicks;
				ui.event( { kind: "activate", id: "map-pin" } );
				step();
				const toggleClicks = clicks - clicksBefore, toggledLabel = follow();
				// A completed world load re-arms AUTO.
				state.worldTransitionRegion = field.regionId;
				step();
				delete state.worldTransitionRegion;
				await settle();
				const rearmed = follow();
				state.gameplay.pose = { ...field, regionId: 93 * 256 + 135 };
				await settle( "map_khotan.png" );
				ui.event( { kind: "activate", id: "map-pin" } );
				ui.event( { kind: "activate", id: "map-world" } );
				await settle( "/map_world_" );
				const hotanLabels = labelLayers();
				return {
					janganLabels,
					hotanLabels,
					panDisabledDuringLoad: !!panDuringLoad?.disabled,
					continuedShift,
					toggleClicks,
					townClicks,
					panKind: pan?.kind,
					panDraggable: pan?.draggable,
					autoWalked,
					autoLabel,
					autoWorld,
					autoDragWorld,
					manualLabel,
					toggledLabel,
					opened,
					afterWalk,
					pinned,
					reversed,
					clip,
					rearmed,
					failed: ui.stats().failed
				};
			} finally {
				ui.dispose();
				assets.dispose();
			}
		} );
		assert.deepEqual( result.failed, [] );
		assert.equal( result.janganLabels.labelsAboveCities, true, JSON.stringify( result.janganLabels ) );
		assert.equal( result.hotanLabels.labelsAboveCities, true, JSON.stringify( result.hotanLabels ) );
		assert.notDeepEqual( result.autoWalked, result.autoWorld, "AUTO follows a walking player" );
		assert.deepEqual( result.autoDragWorld, result.autoWorld, "AUTO swallows the drag" );
		assert.notEqual( result.manualLabel, result.autoLabel, "clicking town switches to manual mode" );
		assert.equal( result.toggledLabel, result.autoLabel, "toggling the pin returns to auto mode" );
		assert.equal( result.toggleClicks, 1, "the follow pin clicks" );
		assert.equal( result.townClicks, 0, "town icons are CIFWorldMap hit areas, silent in retail" );
		assert.equal( result.panKind, "region", "the map surface never activates or clicks" );
		assert.equal( result.panDraggable, true );
		assert.equal(
			result.panDisabledDuringLoad,
			false,
			"streaming tiles must not disable the pan surface mid-drag"
		);
		assert.equal( result.continuedShift, 40, "the drag keeps applying after revealing unloaded tiles" );
		assert.ok( result.opened, "town click in AUTO opens Jangan" );
		assert.deepEqual( result.afterWalk, result.opened, "the town view does not follow the walking player" );
		assert.equal( result.pinned[0], result.clip[0], "overshoot pins the left image edge to the view" );
		assert.equal( result.reversed[0], result.pinned[0] - 10, "reverse drag moves immediately" );
		assert.equal( result.rearmed, result.autoLabel, "world load end re-arms AUTO MOVE" );
	} finally {
		await browser.close();
	}
} );
