import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';

test('live authored speed item consumes once, publishes speed and buff, and shows retail empty tooltip text',{skip:process.env.SRO_SPEED_SCROLL_USE!=='1',timeout:120000},async()=>{
 const itemId=Number(process.env.SRO_SPEED_ITEM_ID??24198),skillId=Number(process.env.SRO_SPEED_SKILL_ID??31107),itemCode=process.env.SRO_SPEED_ITEM_CODE??'ITEM_ETC_SPEED_UP_BASIC';
 const character=process.env.SRO_PROBE_CHARACTER;assert.ok(character,'explicit character required');
 const {browser,page}=await launchProbeBrowser(),directory='temp/artifacts/speed-scroll-live',errors=[],packets=[];
 await mkdir(directory,{recursive:true});page.on('pageerror',e=>errors.push(String(e)));page.on('console',msg=>{if(msg.text().startsWith('SCROLL_FRAME '))packets.push(msg.text());});
 try{
  await page.route('**/src/engine/runtime/simulation/worker/session/world/core.ts',async route=>{
   const response=await route.fetch(),source=await response.text(),pattern=/function receive\(frame, now\)\s*\{/;
   assert.ok(pattern.test(source),'receive observation anchor');
   let body=source.replace(pattern,match=>match+"if([0xb419,0xb6a0,0x376f,0xb5bd].includes(frame.opcode))console.info('SCROLL_FRAME '+JSON.stringify({opcode:frame.opcode,payload:[...frame.payload]}));");
   body=body.replace('if (!gameplay.receive(frame, now, chatSender))',"if ((frame.opcode===0xb419&&console.info('SCROLL_FRAME before-game')), !gameplay.receive(frame, now, chatSender))");
   await route.fulfill({response,body,contentType:'application/javascript'});
  });
  await page.route('**/src/engine/runtime/simulation/worker/session/world/gameplay/combat/combat.ts',async route=>{
   const response=await route.fetch(),source=await response.text();
   const body=source.replace(/receive\(op, p, now = 0\)\s*\{/,match=>match+"if(op===0xb419)console.info('SCROLL_FRAME combat');").replace(/seedEffects\(gid, skills, now = 0\)\s*\{/,match=>match+"console.info('SCROLL_FRAME seed '+JSON.stringify({gid,skills}));");
   await route.fulfill({response,body,contentType:'application/javascript'});
  });
  await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{
   const response=await route.fetch(),source=await response.text();
   const body=source.replace('export function createUi(','function createObservedUi(')+`\nexport function createUi(ports){const owner=createObservedUi(ports);globalThis.__speedScrollProbe={};return {...owner,step(view,now){globalThis.__speedScrollProbe.view=view;const result=owner.step(view,now);if(result)globalThis.__speedScrollProbe.controls=result.controls;return result;}};}`;
   await route.fulfill({response,body,contentType:'application/javascript'});
  });
  await bootPlayableSession(page,character);
  if(await page.evaluate(()=>{const g=__playableRuntime.gameplay();return g.vitals?.some(v=>v.gid===g.localGid&&v.hp===0);})){await page.locator('[data-ui-id="rebirth-point"]').click();await page.waitForFunction(()=>{const g=__playableRuntime.gameplay();return g.vitals?.some(v=>v.gid===g.localGid&&v.hp>0);},null,{timeout:20000});}
  await page.waitForFunction(()=>__speedScrollProbe.view.worldReady===true,null,{timeout:30000});
  if(process.env.SRO_SPEED_SCROLL_REAPPLY==='1'){
   const effects=await page.evaluate(()=>__playableRuntime.gameplay().attachedEffects.filter(e=>[31107,5410,5411].includes(e.skill)));
   for(const effect of effects){await page.evaluate(e=>__playableRuntime.session({kind:'gameplay',command:{kind:'effect-cancel',skillId:e.skill,token:e.token}}),effect);await page.waitForFunction(token=>!__playableRuntime.gameplay().attachedEffects.some(e=>e.token===token),effect.token,{timeout:10000});}
  }
  let state=await page.evaluate(()=>({items:__playableRuntime.gameplay().inventory,effects:__playableRuntime.gameplay().attachedEffects}));
  let item=state.items.find(r=>r.refObjId===itemId);
  if(!item&&!state.effects?.some(e=>e.skill===skillId)){
   assert.equal(await page.evaluate(()=>__playableRuntime.gameplay().eligibility?.gm),true,'grant requires existing GM authority');
   await page.keyboard.press('Shift+Backquote');const input=page.locator('[data-ui-id="gm-input"]');await input.waitFor();
   await input.fill('/MAKEITEM '+itemCode+' 1');await input.press('Enter');
   await page.waitForFunction(itemId=>__speedScrollProbe.view.entities.some(e=>e.groundItem&&e.refObjId===itemId),itemId);
   const gid=await page.evaluate(itemId=>__speedScrollProbe.view.entities.find(e=>e.groundItem&&e.refObjId===itemId).gid,itemId);
   await page.keyboard.press('Escape');await page.evaluate(gid=>__playableRuntime.session({kind:'gameplay',command:{kind:'pickup',gid}}),gid);
   await page.waitForFunction(itemId=>__playableRuntime.gameplay().inventory.some(r=>r.refObjId===itemId),itemId);
   item=await page.evaluate(itemId=>__playableRuntime.gameplay().inventory.find(r=>r.refObjId===itemId),itemId);
  }
  if(!state.effects?.some(e=>e.skill===skillId)){
   assert.ok(item);if(itemId===24198)assert.equal(item.name,'','native English title is empty');
   await page.keyboard.press('KeyI');const slot=page.locator('[data-ui-id="slot:'+item.slot+'"]');await slot.waitFor();await slot.hover();
   await page.screenshot({path:directory+'/tooltip.png'});
   await slot.dblclick();
   await page.waitForFunction(skillId=>__playableRuntime.gameplay().attachedEffects?.some(e=>e.skill===skillId),skillId,{timeout:15000});
   const remaining=await page.evaluate(slot=>__playableRuntime.gameplay().inventory.find(r=>r.slot===slot)?.quantity??0,item.slot);
   assert.equal(remaining,item.quantity-1);
  }
  await page.waitForFunction(skillId=>__speedScrollProbe.controls?.some(c=>c.id.endsWith(':'+skillId)&&c.id.startsWith('buff:')),skillId);
  const result=await page.evaluate(()=>({effects:__playableRuntime.gameplay().attachedEffects,entity:__speedScrollProbe.view.entities.find(e=>e.gid===__playableRuntime.gameplay().localGid),buffs:__speedScrollProbe.controls.filter(c=>c.id.startsWith('buff:')),error:__playableRuntime.gameplay().error}));
  assert.equal(result.entity.runSpeed,100);assert.equal(result.entity.walkSpeed,40);
  const effect=result.effects.find(e=>e.skill===skillId);assert.ok(effect.remainingMs>0&&effect.remainingMs<=3600000);
  await page.screenshot({path:directory+'/active.png'});
  await writeFile(directory+'/incident.json',JSON.stringify({character,item,result,errors,packets},null,2));assert.deepEqual(errors,[]);
 }catch(error){await page.screenshot({path:directory+'/failure.png'}).catch(()=>{});await writeFile(directory+'/failure.json',JSON.stringify({error:String(error),errors,packets,session:await page.evaluate(()=>globalThis.__playableRuntime?.sessionState()).catch(()=>null),state:await page.evaluate(()=>globalThis.__playableRuntime?.gameplay()).catch(()=>null)},null,2));throw error;}
 finally{await browser.close();}
});
