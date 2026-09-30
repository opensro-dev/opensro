/*
===========================================================================

item-catalog.mjs - export the server item references for the dashboard

Runs the server's own item loader (cmd/tools/sro-item-catalog) over the
verified server textdata projection and publishes the result atomically to
.generated/observatory/items.json. `pnpm task build server-game-data` runs
it after the projection it reads.

===========================================================================
*/
import { execFile } from "node:child_process";
import { mkdir, rename, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { ITEM_CATALOG_URL } from "../server/items.mjs";

const SERVER_DIR = fileURLToPath( new URL( "../../server/", import.meta.url ) );
const MAX_EXPORT_BYTES = 32 << 20;

const { stdout } = await promisify( execFile )( "go", [ "run", "./cmd/tools/sro-item-catalog" ], {
	cwd: SERVER_DIR,
	maxBuffer: MAX_EXPORT_BYTES,
	windowsHide: true
} );
const data = JSON.parse( stdout );
if ( data.version !== 1 || !data.items?.length ) throw new Error( "Invalid item export" );
const temporary = new URL( "items.tmp.json", ITEM_CATALOG_URL );
await mkdir( new URL( ".", ITEM_CATALOG_URL ), { recursive: true } );
await writeFile( temporary, JSON.stringify( data ) );
await rename( temporary, ITEM_CATALOG_URL );
console.log( `Exported ${data.items.length} server item references.` );
