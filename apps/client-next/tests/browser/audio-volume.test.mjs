/*
===========================================================================

audio-volume.test.mjs - real browser controls, storage and rendered sample gains

Uses the production platform and UI on an isolated page. Native resources
come from the published tree, with no source patches or private bundles.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { defined } from "../helpers/defined.mjs";
import { firefox } from "playwright-core";
const FIXTURE_URL = CLIENT_NEXT_BASE_URL + "/tests/browser/fixtures/audio-volume.html";

/*
================
launchAudioBrowser

The normal probe uses Chromium; Firefox exercises its independent media/range implementation.
================
*/
async function launchAudioBrowser() {
	if ( process.env.SRO_AUDIO_PROBE_BROWSER !== "firefox" ) {
		return launchProbeBrowser( { executablePath: process.env.SRO_PROBE_CHROME_EXECUTABLE } );
	}
	const browser = await firefox.launch( { headless: true } );
	const page = await browser.newPage( { viewport: { width: 1600, height: 900 } } );
	return { browser, page };
}

/*
================
streaming quiet fade

Drive the production fade from an actual MP3 media clock and a trusted click.
================
*/
test(
	"quiet streaming BGM retains its element, mutes exactly and fades across media notifications",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchAudioBrowser();
		try {
			await page.goto( FIXTURE_URL );
			await page.evaluate( async () => {
				const { createMusic } = await import( "/src/engine/runtime/audio/music/music.ts" );
				const { audioAmplitude } = await import( "/src/engine/foundation/audio/options.ts" );
				const bytes = await (await fetch( "/assets/audio/music/maintheme_cut.mp3" )).arrayBuffer();
				let serial = 0;
				const results = new Map();
				const music = createMusic( {
					available: () => 4,
					request() {
						const id = ++serial;
						results.set( id, { kind: "bytes", id, buffer: bytes.slice( 0 ) } );
						return id;
					},
					take( id ) {
						const result = results.get( id );
						results.delete( id );
						return result;
					},
					cancel: id => results.delete( id )
				}, location.origin );
				music.volume( audioAmplitude( .02, false ) );
				music.active( true, true );
				music.regional( "/assets/audio/music/maintheme_cut.mp3", 0 );
				music.step();
				music.step();
				const button = document.createElement( "button" );
				button.textContent = "Play music";
				button.onclick = () => music.unlock();
				document.body.prepend( button );
				window.fixture = { music, original: music.element(), amplitude: audioAmplitude( .02, false ) };
			} );
			await page.getByRole( "button", { name: "Play music" } ).click();
			await page.waitForFunction( () => fixture.music.element()?.currentTime > .3 );
			assert.equal(
				await page.evaluate( () => fixture.music.element().volume ),
				await page.evaluate( () => fixture.amplitude )
			);
			await page.evaluate( () => {
				fixture.music.volume( 0 );
				if ( fixture.music.element().volume !== 0 ) throw Error( "BGM mute leaked" );
				fixture.music.volume( fixture.amplitude );
				fixture.music.regional( "/assets/audio/music/event_carol_01.mp3", 0 );
			} );
			await page.waitForFunction( () => {
				fixture.music.step();
				return fixture.music.element()?.volume < fixture.amplitude;
			} );
			assert.equal( await page.evaluate( () => fixture.music.element() === fixture.original ), true );
			assert.equal( await page.evaluate( () => fixture.music.status() ), "fading" );
			await page.waitForFunction(
				() => {
					fixture.music.step();
					return fixture.music.element() === null;
				},
				null,
				{ timeout: 30000 }
			);
			await page.evaluate( () => fixture.music.dispose() );
		} finally {
			await browser.close();
		}
	}
);

/*
================
restorePlatform
================
*/
async function restorePlatform( page ) {
	return page.evaluate( async () => {
		const { createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
		const events = [];
		const platform = createPlatform(
			document.querySelector( "canvas" ),
			document.querySelector( "output" ),
			() => {},
			() => {},
			() => {},
			event => events.push( event )
		);
		window.fixture = { platform, events };
		return events.find( event => event.kind === "audio-preferences" ).value;
	} );
}

/*
================
storage compatibility
================
*/
test(
	"production platform restores old levels, saves quiet fractions and reports invalid records",
	{ timeout: 60000 },
	async () => {
		const { browser, page } = await launchAudioBrowser();
		try {
			await page.goto( FIXTURE_URL );
			const original = {
				bgm: 1,
				effects: 20,
				environment: 50,
				muteBgm: false,
				muteEffects: false,
				muteEnvironment: true
			};
			await page.evaluate(
				value => localStorage.setItem( "sro:v1150:audio-options:1", JSON.stringify( value ) ),
				original
			);
			assert.deepEqual( await restorePlatform( page ), original );
			await page.evaluate( () => {
				const control = {
					id: "audio-semantic",
					label: "Effects",
					kind: "range",
					rect: [ 10, 10, 202, 16 ],
					min: 0,
					max: 149,
					value: "5",
					valueText: "Quiet 0.1"
				};
				fixture.platform.presentUi( { title: "Audio", message: "", controls: [ control ] } );
			} );
			assert.equal(
				await page.locator( '[data-ui-id="audio-semantic"]' ).getAttribute( "aria-valuetext" ),
				"Quiet 0.1"
			);
			await page.evaluate( () =>
				fixture.platform.presentUi( {
					title: "Audio",
					message: "",
					controls: [ {
						id: "audio-semantic",
						label: "Effects",
						kind: "range",
						rect: [ 10, 10, 202, 16 ],
						min: 0,
						max: 100,
						value: "20"
					} ]
				} )
			);
			assert.equal(
				await page.locator( '[data-ui-id="audio-semantic"]' ).getAttribute( "aria-valuetext" ),
				null
			);
			const quiet = { ...original, bgm: .02, effects: .123456789, environment: .98 };
			await page.evaluate( value => fixture.platform.saveAudioOptions( value ), quiet );
			assert.deepEqual( await page.evaluate( () => fixture.events.at( -1 ).value ), quiet );
			await page.reload();
			assert.deepEqual( await restorePlatform( page ), quiet );
			await page.evaluate( () => {
				fixture.platform.dispose();
				localStorage.setItem( "sro:v1150:audio-options:1", "{bad" );
			} );
			assert.equal( (await restorePlatform( page )).bgm, 30 );
			assert.match(
				defined( await page.locator( "output" ).textContent() ),
				/Audio options could not be restored/
			);
			await page.evaluate( () => {
				fixture.platform.dispose();
				localStorage.setItem( "sro:v1150:audio-options:1", JSON.stringify( { bgm: 1.1 } ) );
			} );
			assert.equal( (await restorePlatform( page )).effects, 50 );
		} finally {
			await browser.close();
		}
	}
);

/*
================
rendered amplitude
================
*/
test( "browser audio rendering preserves native gains and provides measurable quiet output down to zero", {
	timeout: 60000
}, async () => {
	const { browser, page } = await launchAudioBrowser();
	try {
		await page.goto( FIXTURE_URL );
		const samples = await page.evaluate( async () => {
			const { audioAmplitude } = await import( "/src/engine/foundation/audio/options.ts" );
			const levels = [ 0, .02, .1, .5, .98, 1, 2, 5, 10, 50, 100 ];
			const samples = [];
			for ( const level of levels ) {
				const context = new OfflineAudioContext( 1, 128, 48000 );
				const source = context.createBufferSource(), gain = context.createGain();
				const buffer = context.createBuffer( 1, 128, 48000 );
				buffer.getChannelData( 0 ).fill( 1 );
				source.buffer = buffer;
				gain.gain.value = audioAmplitude( level, false );
				source.connect( gain ).connect( context.destination );
				source.start();
				const result = await context.startRendering();
				samples.push( {
					level,
					sample: result.getChannelData( 0 )[64],
					expected: audioAmplitude( level, false )
				} );
			}
			return samples;
		} );
		let previous = -1;
		for ( const sample of samples ) {
			assert.ok( Math.abs( sample.sample - sample.expected ) < 1e-7 );
			assert.ok( sample.sample > previous );
			previous = sample.sample;
		}
		assert.equal( samples[0].sample, 0 );
		assert.equal( defined( samples.at( -1 ) ).sample, 1 );
	} finally {
		await browser.close();
	}
} );

/*
================
initializeUi
================
*/
async function initializeUi( page ) {
	await page.evaluate( async () => {
		const { createUi } = await import( "/src/engine/runtime/ui/ui.ts" );
		const { createPlatform } = await import( "/src/engine/runtime/platform/platform.ts" );
		const jobs = new Map(), changes = [], scenes = [];
		let serial = 0;
		const assets = {
			available: () => Math.max( 0, 32 - jobs.size ),
			request( url, _limit, kind ) {
				const id = ++serial;
				jobs.set( id, null );
				fetch( url ).then( async response => {
					if ( !response.ok ) throw Error( `${response.status} ${url}` );
					const result = kind === "png" ?
						{ kind: "image", id, image: await createImageBitmap( await response.blob() ) } :
						{ kind: "bytes", id, buffer: await response.arrayBuffer() };
					if ( jobs.has( id ) ) jobs.set( id, result );
					else if ( result.image ) result.image.close();
				} ).catch( error => {
					if ( jobs.has( id ) ) jobs.set( id, { kind: "error", id, error: String( error ) } );
				} );
				return id;
			},
			take( id ) {
				const result = jobs.get( id );
				if ( result ) jobs.delete( id );
				return result;
			},
			cancel( id ) {
				jobs.delete( id );
			}
		};
		let platform;
		const ui = createUi( {
			assets,
			commands: () => {},
			publish: scene => scenes.push( scene ),
			texture: ( _id, image ) => image?.close(),
			base: location.origin,
			apiBase: "http://fixture.invalid",
			clickSound: () => {},
			sound: () => {},
			audioPreference: ( value, commit ) => {
				changes.push( { value, commit } );
				if ( commit ) platform.saveAudioOptions( value );
			},
			extensions: { saveExperimental: value => platform.saveExperimentalOptions( value ) }
		} );
		platform = createPlatform(
			document.querySelector( "canvas" ),
			document.querySelector( "output" ),
			() => {},
			() => {},
			() => {},
			ui.event,
			ui.blocks
		);
		const entity = {
			gid: 1,
			regionId: 1,
			x: 0,
			y: 0,
			z: 0,
			heading: 0,
			kind: "player",
			name: "Audio fixture",
			mountedOn: 0
		};
		const state = {
			session: { phase: "world", revision: 1, character: "Audio fixture" },
			gameplay: { localGid: 1, pose: { ...entity, angle: 0 }, inventory: [], vitals: [], target: 0 },
			entities: [ entity ],
			width: 1600,
			height: 900,
			worldReady: true
		};
		window.fixture = {
			ui,
			platform,
			changes,
			scenes,
			state,
			semantic: null,
			draw() {
				const semantic = ui.step( { ...state }, performance.now() );
				if ( semantic ) {
					this.semantic = semantic;
					platform.presentUi( semantic );
				}
			},
			activate( id ) {
				ui.event( { kind: "activate", id } );
				this.draw();
			}
		};
		fixture.draw();
		fixture.activate( "open-window:Option" );
		/*
		================
		frame

		The real runtime keeps stepping while a newly opened window loads its artwork.
		================
		*/
		requestAnimationFrame( function frame() {
			fixture.draw();
			requestAnimationFrame( frame );
		} );
	} );
	await page.waitForFunction(
		() => {
			fixture.draw();
			return document.querySelector( '[data-ui-id="option-tab:1"]' );
		},
		null,
		{ timeout: 60000 }
	).catch( async error => {
		console.error( await page.evaluate( () => fixture.ui.stats() ) );
		throw error;
	} );
	await page.evaluate( () => fixture.activate( "option-tab:1" ) );
	await page.waitForFunction( () => {
		fixture.draw();
		return document.querySelector( '[data-ui-id="option-audio:bgm"]' );
	} );
}

/*
================
audio controls
================
*/
test(
	"all audio controls map positions, keyboard steps, draft rollback and saved quiet levels",
	{ timeout: 180000 },
	async () => {
		const { browser, page } = await launchAudioBrowser();
		try {
			await page.goto( FIXTURE_URL );
			await page.evaluate( () => {
				localStorage.clear();
			} );
			await initializeUi( page );
			for ( const key of [ "bgm", "effects", "environment" ] ) {
				const slider = page.locator( `[data-ui-id="option-audio:${key}"]` );
				assert.equal( await slider.getAttribute( "max" ), "149" );
				await slider.fill( "49" );
				await page.evaluate( () => fixture.draw() );
				assert.equal( await page.evaluate( key => fixture.changes.at( -1 ).value[key], key ), .98 );
				assert.equal( await slider.getAttribute( "aria-valuetext" ), "Quiet 0.98" );
				await page.waitForFunction( key => {
					fixture.draw();
					const control = fixture.semantic.controls.find( control => control.id === `option-audio:${key}` );
					return fixture.scenes.at( -1 ).quads.some( quad =>
						quad.texture?.endsWith( "/com_scroll_button.png" ) &&
						quad.rect[1] === control.rect[1] &&
						quad.rect[0] === control.rect[0] + Math.trunc( 49 * 186 / 149 )
					);
				}, key );
				const box = defined( await slider.boundingBox() );
				await page.mouse.click( box.x + box.width * .1, box.y + box.height / 2 );
				await page.evaluate( () => fixture.draw() );
				const mouseLevel = await page.evaluate( key => fixture.changes.at( -1 ).value[key], key );
				assert.ok( mouseLevel > 0 && mouseLevel < 1, "mouse travel reaches the quiet range" );
				await slider.fill( "49" );
				await slider.press( "ArrowRight" );
				await page.evaluate( () => fixture.draw() );
				assert.equal( await page.evaluate( key => fixture.changes.at( -1 ).value[key], key ), 1 );
				await page.evaluate( key => fixture.activate( `option-audio-step:${key}:1` ), key );
				assert.equal( await page.evaluate( key => fixture.changes.at( -1 ).value[key], key ), 2 );
				await slider.press( "Home" );
				await page.evaluate( () => fixture.draw() );
				assert.equal( await page.evaluate( key => fixture.changes.at( -1 ).value[key], key ), 0 );
				await slider.press( "End" );
				await page.evaluate( () => fixture.draw() );
				assert.equal( await page.evaluate( key => fixture.changes.at( -1 ).value[key], key ), 100 );
				await slider.fill( "5" );
				await page.evaluate( () => fixture.draw() );
			}
			assert.equal( await page.evaluate( () => localStorage.getItem( "sro:v1150:audio-options:1" ) ), null );
			await page.evaluate( () => fixture.activate( "option-apply" ) );
			assert.equal( await page.locator( '[data-ui-id="option-audio:bgm"]' ).count(), 1 );
			assert.equal(
				await page.evaluate( () =>
					JSON.parse( localStorage.getItem( "sro:v1150:audio-options:1" ) ?? "null" ).effects
				),
				.1
			);
			await page.locator( '[data-ui-id="option-audio:bgm"]' ).fill( "20" );
			await page.evaluate( () => fixture.activate( "option-cancel" ) );
			assert.equal( await page.evaluate( () => fixture.changes.at( -1 ).value.bgm ), .1 );
			await page.evaluate( () => {
				fixture.activate( "open-window:Option" );
				fixture.activate( "option-tab:1" );
				fixture.activate( "option-default" );
			} );
			assert.equal( await page.evaluate( () => fixture.changes.at( -1 ).value.bgm ), 50 );
			await page.evaluate( () => {
				fixture.ui.event( { kind: "key", code: "Escape" } );
				fixture.draw();
			} );
			assert.equal( await page.evaluate( () => fixture.changes.at( -1 ).value.bgm ), .1 );
			await page.evaluate( () => {
				fixture.activate( "open-window:Option" );
				fixture.activate( "option-tab:1" );
			} );
			await page.locator( '[data-ui-id="option-audio:bgm"]' ).fill( "10" );
			await page.evaluate( () => fixture.activate( "open-window:Experimental" ) );
			assert.equal( await page.evaluate( () => fixture.changes.at( -1 ).value.bgm ), .1 );
			await page.reload();
			await initializeUi( page );
			assert.equal( await page.locator( '[data-ui-id="option-audio:bgm"]' ).inputValue(), "5" );
		} finally {
			await browser.close();
		}
	}
);
