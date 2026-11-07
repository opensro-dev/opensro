import {test} from 'node:test';
import assert from 'node:assert/strict';
import {build} from 'esbuild';
const factories={
 './resources/resources':'createUiAssets','./hud/resources':'createHudResources',
 './guide/resources':'createGuideResources','./localization/localization':'createLocalization',
 './title/title':'createTitleUi','./text/text':'createUiText'
};
const compiled=await build({entryPoints:['src/engine/runtime/ui/ui.ts'],bundle:true,platform:'node',format:'esm',write:false,plugins:[{name:'ui-owner-faults',setup(build){
 build.onResolve({filter:/.*/},args=>args.importer.replaceAll('\\','/').endsWith('/ui/ui.ts')&&factories[args.path]?{path:args.path,namespace:'ui-owner'}:undefined);
 build.onLoad({filter:/.*/,namespace:'ui-owner'},args=>({contents:`export function ${factories[args.path]}(){return globalThis.__uiCleanupOwners[${JSON.stringify(args.path)}];}`}));
}}]});
const {createUi}=await import('data:text/javascript;base64,'+Buffer.from(compiled.outputFiles[0].contents).toString('base64'));
test('a failed UI child cannot strand sibling owners or leave the published scene alive',t=>{
 const original=Object.getOwnPropertyDescriptor(globalThis,'__uiCleanupOwners');t.after(()=>{if(original)Object.defineProperty(globalThis,'__uiCleanupOwners',original);else delete globalThis.__uiCleanupOwners;});
 for(const failed of Object.keys(factories)){
  const disposed=[],scenes=[];globalThis.__uiCleanupOwners=Object.fromEntries(Object.keys(factories).map(key=>[key,{dispose(){disposed.push(key);if(key===failed)throw Error(key);}}]));
  const ui=createUi({assets:{},commands:()=>{},publish:scene=>scenes.push(scene),texture:()=>{},base:'https://fixture.invalid',apiBase:'https://fixture.invalid'});
  assert.throws(()=>ui.dispose(),error=>error instanceof AggregateError&&error.errors.length===1);
  assert.deepEqual(disposed.sort(),Object.keys(factories).sort());assert.deepEqual(scenes,[null]);ui.dispose();assert.equal(disposed.length,6);
 }
});
