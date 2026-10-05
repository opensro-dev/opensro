import {test} from 'node:test';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
test('capture Cerberus environment and minimap',{timeout:90000,skip:!process.env.SRO_PROBE_CHARACTER},async()=>{
 const {browser,page}=await launchProbeBrowser();const dir='temp/artifacts/cerberus-weather';await mkdir(dir,{recursive:true});
 try{
 await page.route('**/src/engine/runtime/renderer/world/world.ts',async route=>{const response=await route.fetch();let body=await response.text();body=body.replace('const dt = fadeSeconds',"globalThis.__cerberusEnvironment={weatherOptions,hasEnvironment:!!current?.environment,environment:[...environment],state:environmentState&&[...environmentState]}; const dt = fadeSeconds");await route.fulfill({response,body,contentType:'application/javascript'});});
 await bootPlayableSession(page,process.env.SRO_PROBE_CHARACTER);await page.waitForFunction(()=>globalThis.__cerberusEnvironment);
 await page.waitForTimeout(15000);await page.screenshot({path:dir+'/live.png'});await writeFile(dir+'/live.json',JSON.stringify(await page.evaluate(()=>({env:__cerberusEnvironment,pose:__playableRuntime.gameplay().pose,phase:__playableRuntime.sessionState()})),null,2));
 }finally{await browser.close();}
});
