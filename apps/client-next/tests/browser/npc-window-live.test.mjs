import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';

// Read-only UI observation; real authentication, selection, conversation and
// target-release commands still cross the production simulation/transport.
test('live NPC title drag and ESC retire the selected conversation',{timeout:300000},async()=>{
 const {browser,page}=await launchProbeBrowser();
 const baseline=process.env.NPC_WINDOW_BASELINE==='1',directory='temp/artifacts/npc-window/'+(baseline?'before':process.env.NPC_WINDOW_TARGET?'targeted-'+process.env.NPC_WINDOW_TARGET.replace(/[^a-z0-9_-]/gi,'_'):'after');
 await mkdir(directory,{recursive:true});
  const errors=[];page.on('pageerror',e=>errors.push(String(e)));
  try {
  await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{
   const response=await route.fetch(),source=await response.text();
   assert.ok(source.includes('export function createUi('));
   const body=source.replace('export function createUi(','function createObservedUi(')+`\nexport function createUi(...args){const commands=args[1],publish=args[2],events=[];let scene;args[2]=s=>{scene=s;return publish(s);};args[1]=c=>{events.push(c);return commands(c);};const owner=createObservedUi(...args);globalThis.__npcWindowProbe={owner,commands:args[1],events,get scene(){return scene;}};return {...owner,step(view,now){globalThis.__npcWindowProbe.view=view;return owner.step(view,now);}};}`;
   await route.fulfill({response,body,contentType:'application/javascript'});
  });
  await bootPlayableSession(page,'asd2');
  await page.waitForFunction(()=>__npcWindowProbe.view.entities.some(e=>e.kind==='npc'),null,{timeout:20000});
  const target=await page.evaluate(name=>{
   const p=__npcWindowProbe.view.gameplay.pose;
   const rows=__npcWindowProbe.view.entities.filter(e=>e.kind==='npc'&&(!name||e.name.includes(name))).map(e=>({gid:e.gid,name:e.name,heading:e.heading,distance:Math.hypot(e.x+((e.regionId&255)-(p.regionId&255))*1920-p.x,e.z+((e.regionId>>>8)-(p.regionId>>>8))*1920-p.z)})).sort((a,b)=>a.distance-b.distance);
   if(!rows.length)throw Error('No requested scratch-session NPC in scope');return rows[0];
  },process.env.NPC_WINDOW_TARGET??'');
  if(target.name.includes('Sansan'))assert.equal(target.heading,16565,'live spawn carries the recovered heading');
  await page.evaluate(gid=>__npcWindowProbe.commands({kind:'gameplay',command:{kind:'select',gid}}),target.gid);
  const talk=page.locator('[data-ui-id="npc-talk"]');await talk.waitFor({timeout:20000});
  if(!baseline){
   const frame=await page.evaluate(()=>__npcWindowProbe.scene.sections?.flatMap(s=>s.quads)??__npcWindowProbe.scene.quads);
   const pieces=frame.filter(q=>q.texture.includes('/npc/npc_conversation_window_'));
   assert.equal(new Set(pieces.map(q=>q.texture)).size,8,'NPC child frame is admitted with its content');
  }
  if(!baseline&&target.distance>150&&target.name.includes('Chulsan')){
   const purchase=page.locator('[data-ui-id^="shop-group:"], [data-ui-id="shop-open"]').first();
   await purchase.click();
   await page.waitForFunction(()=>!!__playableRuntime.gameplay().shop?.error&&!__playableRuntime.gameplay().inventoryPending,null,{timeout:15000});
   assert.notEqual(await page.evaluate(()=>__npcWindowProbe.owner.stats().panel),'Shop','rejected distant merchant must not open a shop');
   assert.equal(await page.locator('[data-ui-id^="shop-empty:"]').count(),0);
   await talk.waitFor();
  }
  const before=await talk.boundingBox();await page.screenshot({path:directory+'/selected.png'});
  // Native talk origin = frame + (34,71); drag title at frame +(80,17).
  const x=before.x-34+80,y=before.y-71+17;
  await page.mouse.move(x,y);await page.mouse.down();await page.mouse.move(x+90,y+50,{steps:10});await page.mouse.up();
  const after=await talk.boundingBox();await page.screenshot({path:directory+'/dragged.png'});
  const stages={};
  const note=async(name,data)=>{stages[name]=data;await writeFile(directory+'/incident.json',JSON.stringify({target,before,after,stages,errors},null,2));};
  await page.keyboard.press('Escape');
  if(!baseline)await talk.waitFor({state:'detached',timeout:10000});
  const result=await page.evaluate(()=>({phase:__playableRuntime.gameplay().npcConversation?.phase,target:__playableRuntime.gameplay().target,panel:__npcWindowProbe.owner.stats().panel,events:__npcWindowProbe.events}));
  await note('esc-close',{after,result});
  if(!baseline){assert.equal(after.x-before.x,90);assert.equal(after.y-before.y,50);assert.equal(result.phase,'closed');assert.equal(result.target,0);assert.notEqual(result.panel,'System');assert.ok(result.events.some(e=>e.command?.kind==='npc-close'));assert.deepEqual(errors,[]);}
  const reopen=async()=>{
   await page.evaluate(gid=>__npcWindowProbe.commands({kind:'gameplay',command:{kind:'select',gid}}),target.gid);
   await talk.waitFor({timeout:20000});
  };
  // X button (16x16 chrome, sole npc-close control) closes through real
  // pointer input. The TalkEnd row carries its own npc-talkend id so the
  // DOM bridge keeps a separate hit target for the chrome.
  await reopen();
  const closes=page.locator('[data-ui-id="npc-close"]');
  const closeCount=await closes.count();assert.equal(closeCount,1);
  const xBox=await closes.first().boundingBox();assert.deepEqual([xBox.width,xBox.height],[16,16]);
  await note('close-controls',{closeCount,xBox});
  await closes.first().click();await talk.waitFor({state:'detached',timeout:10000});
  const xClosed=await page.evaluate(()=>({phase:__playableRuntime.gameplay().npcConversation?.phase,panel:__npcWindowProbe.owner.stats().panel,events:__npcWindowProbe.events.length}));
  await page.screenshot({path:directory+'/x-closed.png'});
  await note('x-close',{xBox,xClosed});
  if(!baseline){assert.equal(xClosed.phase,'closed');}
  // TalkEnd row (npc-talkend) retires the reopened conversation.
  // In dialogue phases the row is replaced by npc-choice options instead.
  await reopen();
  const talkend=page.locator('[data-ui-id="npc-talkend"]');
  const talkendCount=await talkend.count();
  const choices=page.locator('[data-ui-id^="npc-choice:"]');
  const choiceCount=await choices.count();
  await note('talkend-or-dialogue',{talkendCount,choiceCount});
  if(talkendCount>0){
   const endBox=await talkend.first().boundingBox();assert.ok(endBox.width>16);
   await talkend.first().click();await talk.waitFor({state:'detached',timeout:10000});
   const endClosed=await page.evaluate(()=>({phase:__playableRuntime.gameplay().npcConversation?.phase,panel:__npcWindowProbe.owner.stats().panel,events:__npcWindowProbe.events}));
   await page.screenshot({path:directory+'/talkend-closed.png'});
   await note('talkend-close',{phase:endClosed.phase});
   if(!baseline){assert.equal(endClosed.phase,'closed');assert.ok(endClosed.events.some(e=>e.command?.kind==='npc-close'));}
  }else{
   assert.ok(choiceCount>0,'dialogue phase offers choices');
   const choiceEvents=await page.evaluate(()=>__npcWindowProbe.events.length);
   await choices.first().click();
   const chosen=await page.evaluate(count=>({events:__npcWindowProbe.events.slice(count),phase:__playableRuntime.gameplay().npcConversation?.phase}),choiceEvents);
   await page.screenshot({path:directory+'/dialogue-choice.png'});
   await note('dialogue-choice',{commands:chosen.events.map(e=>e.command?.kind),phase:chosen.phase});
   if(!baseline){assert.ok(chosen.events.some(e=>e.command?.kind==='npc-choice'));}
   await page.keyboard.press('Escape');await talk.waitFor({state:'detached',timeout:10000});
  }
  // Talk button fires while the conversation stays open (pending server reply).
  await reopen();
  const talkEvents=await page.evaluate(()=>__npcWindowProbe.events.length);
  await talk.click();
  const pending=await page.evaluate(count=>({events:__npcWindowProbe.events.slice(count),phase:__playableRuntime.gameplay().npcConversation?.phase,panel:__npcWindowProbe.owner.stats().panel}),talkEvents);
  await note('talk-pending',{commands:pending.events.map(e=>e.command?.kind),phase:pending.phase});
  if(!baseline){assert.ok(pending.events.some(e=>e.command?.kind==='npc-talk'));assert.notEqual(pending.phase,'closed');}
  // Informational Confirm must finish on the actual server close notification.
  await page.locator('[data-ui-id="npc-choice:1"]').waitFor({timeout:15000});
  await page.locator('[data-ui-id="npc-choice:1"]').click();
  await page.waitForFunction(()=>__playableRuntime.gameplay().npcConversation?.phase==='closed',null,{timeout:10000});
  assert.equal(await page.evaluate(()=>__playableRuntime.gameplay().target),0);
  await page.screenshot({path:directory+'/confirmed-closed.png'});
  await reopen();
  // ESC chain: conversation closes first, System opens second, ESC closes System.
  await page.keyboard.press('Escape');await talk.waitFor({state:'detached',timeout:10000});
  await page.keyboard.press('Escape');
  await page.locator('[data-ui-id="system-restart"]').waitFor({timeout:10000});
  const systemOpen=await page.evaluate(()=>__npcWindowProbe.owner.stats().panel);
  await page.screenshot({path:directory+'/system.png'});
  await note('esc-system',{systemOpen});
  if(!baseline){assert.equal(systemOpen,'System');}
  await page.keyboard.press('Escape');
  await page.waitForFunction(()=>__npcWindowProbe.owner.stats().panel==='',null,{timeout:10000});
  // Retail UI_HideMenuAndRequestRestartType2 hides the menu and sends restart type 2; no placeholder panel.
  await page.keyboard.press('Escape');
  await page.locator('[data-ui-id="system-restart"]').waitFor({timeout:10000});
  await page.locator('[data-ui-id="system-restart"]').click();
  await page.waitForFunction(()=>__npcWindowProbe.owner.stats().panel==='',null,{timeout:10000});
  await page.waitForFunction(()=>__playableRuntime.sessionState()?.phase==='character-select',null,{timeout:30000});
  await page.screenshot({path:directory+'/restart.png'});
  await page.evaluate(()=>__playableRuntime.session({kind:'logout'}));
  await page.waitForFunction(()=>__playableRuntime.sessionState()?.phase==='signed-out',null,{timeout:30000});
  const logout=await page.evaluate(()=>({events:__npcWindowProbe.events.slice(-3).map(e=>e.kind||e.command?.kind),session:__playableRuntime.sessionState()?.phase}));
  await note('logout',{logout});
  if(!baseline){assert.equal(logout.session,'signed-out');assert.deepEqual(errors,[]);}
 } catch(error) {
  await writeFile(directory+'/failure.json',JSON.stringify({error:String(error),errors,state:await page.evaluate(()=>({session:globalThis.__playableRuntime?.sessionState(),game:globalThis.__playableRuntime?.gameplay(),ui:globalThis.__npcWindowProbe?.owner.stats(),events:globalThis.__npcWindowProbe?.events})).catch(()=>null)},null,2));
  await page.screenshot({path:directory+'/failure.png'}).catch(()=>{});throw error;
 } finally {await browser.close();}
});
