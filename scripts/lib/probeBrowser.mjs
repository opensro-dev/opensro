/*
===========================================================================

probeBrowser.mjs - owned Chrome launcher for browser tests and probes

Keeps browser defaults consistent while allowing explicit lifecycle tests
to exercise Chrome's real background scheduling policy.

===========================================================================
*/

import { chromium } from "playwright-core";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const BACKGROUND_THROTTLING_OVERRIDES = [
	"--disable-background-timer-throttling",
	"--disable-backgrounding-occluded-windows",
	"--disable-renderer-backgrounding"
];
const PROFILE_ARGUMENT = "--user-data-dir=";
const CDP_START_TIMEOUT_MS = 10000;
const CDP_POLL_MS = 50;

/*
================
launchBackgroundBrowser

Playwright's normal connection emulates focus in every page. Its supported
CDP noDefaults connection preserves real visibility in the default context.
The owned server keeps profile cleanup and process shutdown with the launcher.
================
*/
async function launchBackgroundBrowser( launchOptions, pageOptions ) {
	const server = await chromium.launchServer( {
		...launchOptions,
		args: [ ...launchOptions.args, "--remote-debugging-port=0" ]
	} );
	try {
		const profile = server.process().spawnargs.find( arg => arg.startsWith( PROFILE_ARGUMENT ) )?.slice(
			PROFILE_ARGUMENT.length
		);
		if ( !profile ) throw Error( "Background browser has no owned profile" );
		let endpoint;
		const deadline = Date.now() + CDP_START_TIMEOUT_MS;
		while ( Date.now() < deadline ) {
			try {
				const port = Number(
					(await readFile( join( profile, "DevToolsActivePort" ), "utf8" )).split( "\n" )[0]
				);
				if ( Number.isInteger( port ) && port > 0 ) {
					endpoint = `http://127.0.0.1:${port}`;
					break;
				}
			} catch ( error ) {
				if ( error.code !== "ENOENT" ) throw error;
			}
			await delay( CDP_POLL_MS );
		}
		if ( !endpoint ) throw Error( "Background browser CDP endpoint timed out" );
		const browser = await chromium.connectOverCDP( endpoint, { noDefaults: true } );
		const context = browser.contexts()[0];
		if ( !context ) throw Error( "Background browser has no default context" );
		const page = context.pages()[0] ?? await context.newPage();
		await page.setViewportSize( pageOptions.viewport );
		const disconnect = browser.close.bind( browser );
		/*
		================
		closeBackgroundBrowser
		================
		*/
		browser.close = async function closeBackgroundBrowser() {
			try {
				await disconnect();
			} finally {
				await server.close();
			}
		};
		return { browser, page };
	} catch ( error ) {
		await server.close();
		throw error;
	}
}

/*
================
launchProbeBrowser
================
*/

/**
 * Launch the standard probe browser: system Chrome, isolated profile, the
 * headless GPU flags every existing harness uses. `headed` is for local
 * debugging (e.g. PROFILE_HEADED=1).
 *
 * SRO_PROBE_EXTRA_CHROME_ARGS appends ad-hoc space-separated Chrome switches
 * (e.g. "--enable-unsafe-webgpu" for the ?webgpu=1 runs; pair it with
 * SRO_PROBE_EXTRA_QUERY="&webgpu=1").
 *
 * @param {{
 *   headed?: boolean,
 *   backgroundThrottling?: boolean,
 *   viewport?: { width: number, height: number },
 *   deviceScaleFactor?: number,
 *   extraBrowserArgs?: string[],
 *   executablePath?: string,
 *   userDataDir?: string
 * }} [options]
 */
export async function launchProbeBrowser( {
	headed = false,
	backgroundThrottling = false,
	viewport = { width: 1600, height: 900 },
	deviceScaleFactor,
	extraBrowserArgs = [],
	executablePath,
	userDataDir
} = {} ) {
	const extraArgs = (process.env.SRO_PROBE_EXTRA_CHROME_ARGS ?? "")
		.split( /\s+/ )
		.filter( ( arg ) => arg.length > 0 );
	const unlockArgs = process.env.SRO_PROBE_UNLOCK_FPS === "1" ?
		[ "--disable-frame-rate-limit", "--disable-gpu-vsync" ] :
		[];
	const launchOptions = {
		...(executablePath ? { executablePath } : { channel: "chrome" }),
		headless: !headed,
		...(backgroundThrottling ? { ignoreDefaultArgs: BACKGROUND_THROTTLING_OVERRIDES } : {}),
		args: [
			"--no-sandbox",
			"--enable-unsafe-swiftshader",
			"--use-angle=default",
			...unlockArgs,
			...extraArgs,
			...extraBrowserArgs
		]
	};
	const pageOptions = {
		viewport,
		...(deviceScaleFactor === undefined ? {} : { deviceScaleFactor })
	};
	if ( backgroundThrottling ) {
		if ( userDataDir || deviceScaleFactor !== undefined ) {
			throw Error( "Background scheduling probes require a fresh profile and native device scale" );
		}
		return launchBackgroundBrowser( launchOptions, pageOptions );
	}
	if ( userDataDir ) {
		const context = await chromium.launchPersistentContext( userDataDir, {
			...launchOptions,
			...pageOptions
		} );
		const browser = context.browser();
		if ( !browser ) {
			await context.close();
			throw new Error( "persistent probe context did not expose its browser" );
		}
		const page = context.pages()[0] ?? (await context.newPage());
		return { browser, page };
	}

	const browser = await chromium.launch( launchOptions );
	const page = await browser.newPage( {
		...pageOptions
	} );
	return { browser, page };
}
