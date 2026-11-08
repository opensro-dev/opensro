import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile,readFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {CLIENT_NEXT_BASE_URL} from '../../../../scripts/lib/probeEndpoints.mjs';

test('Graesp progress crosses production wire, closed-journal UI and GPU banner',{timeout:60000},async()=>{
 const out='temp/artifacts/quest-progress/'+new Date().toISOString().replaceAll(':','-');await mkdir(out,{recursive:true});
 const fixture=JSON.parse(await readFile('../server/internal/game/quest/graesp_wire_fixture.json','utf8'));
 const timed=JSON.parse(await readFile('../server/internal/game/quest/timed_quest_wire_fixture.json','utf8'));
 const {browser,page}=await launchProbeBrowser();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 try{
  await page.setViewportSize({width:1200,height:900});await page.goto(CLIENT_NEXT_BASE_URL);
  await page.waitForFunction(()=>document.querySelector('output')?.textContent?.includes('runtime: running'));
  await page.evaluate(async(fixture)=>{
   (await import('/src/bootstrap.ts')).runtime.dispose();
   const {createAssets}=await import('/src/engine/runtime/assets/assets.ts'),{createUi}=await import('/src/engine/runtime/ui/ui.ts'),{createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts'),{createGameplay}=await import('/src/engine/runtime/simulation/worker/session/world/gameplay/gameplay.ts');
   const canvas=document.createElement('canvas');document.body.replaceChildren(canvas);const assets=createAssets(),renderer=createRenderer(canvas),game=createGameplay(()=>{});let scene;
   const ui=createUi({assets,commands:()=>{},publish:s=>{scene=s;renderer.setUi(s);},texture:(id,image)=>renderer.setUiTexture(id,image),base:location.origin,apiBase:'http://fixture.invalid'});
   game.bootstrap({character:{activeQuests:[]}});
   const state={session:{phase:'world',revision:1,character:'Fixture'},gameplay:game.take(),entities:[],width:1200,height:900,worldReady:true};
   function receive(count){const hex=fixture.frames[count].payloadHex;game.receive({opcode:0x31ed,payload:Uint8Array.from(hex.match(/../g),b=>parseInt(b,16))},1000);state.gameplay=game.take();}
   // The journal stays closed throughout. Bootstrap/insert is silent.
   receive(0);
   window.questProbe={ui,renderer,assets,game,receive,receiveFrame(f){game.receive({opcode:f.opcode,payload:Uint8Array.from(f.payloadHex.match(/../g),b=>parseInt(b,16))},1000);state.gameplay=game.take();},get scene(){return scene;},get state(){return state.gameplay;},draw(now=1000){ui.step({...state},now);renderer.frame({width:1200,height:900});if(renderer.error()||ui.stats().error)throw Error(renderer.error()??ui.stats().error);},dispose(){ui.dispose();game.dispose();renderer.dispose();assets.dispose();}};
  },fixture);
  await page.waitForFunction(()=>{questProbe.draw();return questProbe.ui.stats().pending===0&&questProbe.renderer.phase()==='running';});
  assert.equal(await page.evaluate(()=>questProbe.scene.quads.filter(q=>q.texture.includes('/com_quest_')).length),0);
  await page.screenshot({path:out+'/before.png'});
  await page.evaluate(()=>questProbe.receive(1));
  await page.waitForFunction(()=>{questProbe.draw();return questProbe.ui.stats().pending===0&&questProbe.scene.quads.filter(q=>q.texture.includes('/com_quest_')).length===8;},null,{timeout:10000});
  await page.screenshot({path:out+'/after.png'});
  const report=await page.evaluate(()=>({chrome:questProbe.scene.quads.filter(q=>q.texture.includes('/com_quest_')),state:questProbe.state,stats:questProbe.ui.stats()}));
  await writeFile(out+'/report.json',JSON.stringify({...report,errors},null,2)+'\n');
  assert.deepEqual(errors,[]);assert.equal(report.chrome.length,8);assert.equal(report.chrome[6].rect[1],152);assert.equal(report.chrome[4].rect[2],40);
  await page.evaluate(()=>{questProbe.receive(20);questProbe.draw();});
  await page.screenshot({path:out+'/completed.png'});
  assert.equal(await page.evaluate(()=>questProbe.state.quests[0].contents[0].kind),2,'server publishes the first-completion branch');
  await page.evaluate(f=>{questProbe.receiveFrame(f);questProbe.ui.event({kind:'activate',id:'open-window:Quests'});},timed.frames[0]);
  await page.waitForFunction(()=>{questProbe.draw();return questProbe.ui.stats().pending===0&&questProbe.ui.stats().windowReady;});
  await page.screenshot({path:out+'/timed-journal.png'});
  await page.evaluate(f=>{questProbe.receiveFrame(f);questProbe.draw(601000);},timed.frames[1]);
  await page.screenshot({path:out+'/timed-correction.png'});
  await page.evaluate(frames=>{for(const f of frames)questProbe.receiveFrame(f);questProbe.draw(7201000);},timed.frames.slice(2));
  await page.screenshot({path:out+'/timed-expiry.png'});
  const expiry=await page.evaluate(()=>({quests:questProbe.state.quests,notices:questProbe.state.notices,chrome:questProbe.scene.quads.filter(q=>q.texture.includes('/com_quest_')),stats:questProbe.ui.stats()}));
  await writeFile(out+'/timed-report.json',JSON.stringify({...expiry,errors},null,2)+'\n');
  assert.ok(!expiry.quests.some(q=>q.refId===19));assert.equal(expiry.notices.at(-1).questBanner,true);assert.equal(expiry.chrome.length,8);assert.deepEqual(errors,[]);
 }finally{try{await page.evaluate(()=>window.questProbe?.dispose());}finally{await browser.close();}}
});
