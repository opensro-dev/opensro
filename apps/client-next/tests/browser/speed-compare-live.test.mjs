import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
test('capture ground movement speed against authoritative receipt',{skip:!process.env.SRO_SPEED_COMPARE_CHARACTER,timeout:120000},async()=>{
 const {browser,page}=await launchProbeBrowser(),packets=[];
 try{
 page.on('console',m=>{if(m.text().startsWith('SPEED_COMPARE '))packets.push(JSON.parse(m.text().slice(14)));});
 await page.route('**/src/engine/runtime/simulation/worker/session/world/core.ts',async route=>{
  const response=await route.fetch(),source=await response.text();const pattern=/function receive\(frame, now\)\s*\{/;assert.ok(pattern.test(source));
  await route.fulfill({response,body:source.replace(pattern,m=>m+"if([10,0xb738,0x376f].includes(frame.opcode))console.info('SPEED_COMPARE '+JSON.stringify({now,opcode:frame.opcode,payload:[...frame.payload]}));"),contentType:'application/javascript'});
 });
 await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{
 const response=await route.fetch(),source=await response.text();await route.fulfill({response,body:source.replace('export function createUi(','function observedUi(')+`\nexport function createUi(ports){const owner=observedUi(ports);return {...owner,step(view,now){globalThis.__speedView=view;return owner.step(view,now);}};}`,contentType:'application/javascript'});
 });
 await bootPlayableSession(page,process.env.SRO_SPEED_COMPARE_CHARACTER);
 const before=await page.evaluate(()=>{const g=__playableRuntime.gameplay();return {game:g,entity:__speedView.entities.find(e=>e.gid===g.localGid)};});
 await page.evaluate(()=>{const p=__playableRuntime.gameplay().pose;__playableRuntime.session({kind:'gameplay',command:{kind:'move',destination:{...p,x:Math.min(1850,p.x+100)}}});});
 await page.waitForFunction(()=>__playableRuntime.gameplay().acknowledgedMove>0,null,{timeout:15000});
 const samples=await page.evaluate(async()=>{const rows=[];for(let i=0;i<20;i++){rows.push({at:performance.now(),game:__playableRuntime.gameplay()});await new Promise(r=>setTimeout(r,100));}return rows;});
 await mkdir('temp/artifacts/speed-compare',{recursive:true});await writeFile('temp/artifacts/speed-compare/incident.json',JSON.stringify({before,packets,samples},null,2));
 }finally{await browser.close();}
});

