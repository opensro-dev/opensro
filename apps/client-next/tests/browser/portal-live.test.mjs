// Authenticated scratch fixture: asd2 within interaction range of the Jangan gate.
// Paid travel is exercised only when the character has its authored fee.
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
import {writeFile} from 'node:fs/promises';
test('live city portal picking, destination UI and authoritative travel outcome',{timeout:180000},async()=>{
const {browser,page}=await launchProbeBrowser();const errors=[];page.on('pageerror',e=>errors.push(String(e)));
try{
 await page.route('**/src/engine/runtime/ui/ui.ts',async route=>{const response=await route.fetch(),source=await response.text();await route.fulfill({response,body:source.replace('export function createUi(','function createObservedUi(')+'\nexport function createUi(ports){const owner=createObservedUi(ports);globalThis.__portal={owner,commands:ports.commands};return {...owner,step(view,now){globalThis.__portal.view=view;return owner.step(view,now);}};}',contentType:'application/javascript'});});

 await page.route('**/src/engine/runtime/renderer/renderer.ts',async route=>{const response=await route.fetch(),source=await response.text();await route.fulfill({response,body:source.replace('export function createRenderer(', 'function createObservedRenderer(').replace('pickView = scene.matrix;', 'pickView = scene.matrix; globalThis.__portalMatrix=Array.from(scene.matrix);globalThis.__portalOrigin=scene.originRegion;')+'\nexport function createRenderer(...args){const owner=createObservedRenderer(...args);return {...owner,pickEntity(...args){const value=owner.pickEntity(...args);globalThis.__portalHit={args,value};return value;}};}',contentType:'application/javascript'});});
 await bootPlayableSession(page,'asd2');
 await page.waitForFunction(()=>__portal.view.entities.some(e=>e.kind==='teleport'),null,{timeout:20000});
 const gate=await page.evaluate(()=>__portal.view.entities.find(e=>e.kind==='teleport'&&e.refObjId===2094));
 if(!gate)throw Error('Jangan gate absent');
 await page.evaluate(gid=>__portal.commands({kind:'gameplay',command:{kind:'select',gid}}),gate.gid);
 await page.waitForFunction(()=>__portal.view.gameplay.npcConversation?.phase==='menu',null,{timeout:45000});
 await page.locator('[data-ui-id="npc-close"]').waitFor({timeout:20000});
 await page.screenshot({path:'temp/artifacts/portal-menu.png'});
 

 await page.locator('[data-ui-id="npc-close"]').waitFor({timeout:20000});
 await page.keyboard.press('Escape');await page.waitForFunction(()=>__portal.view.gameplay.npcConversation?.phase==='closed');await page.locator('[data-ui-id="npc-close"]').waitFor({state:'detached'});await page.waitForFunction(()=>!__portal.view.gameplay.targetPending);
 const point=await page.evaluate(gate=>{const m=__portalMatrix,o=__portalOrigin;const x=gate.x+((gate.regionId&255)-(o&255))*1920,y=gate.y,z=gate.z+((gate.regionId>>>8)-(o>>>8))*1920,w=m[3]*x+m[7]*y+m[11]*z+m[15];return {x:((m[0]*x+m[4]*y+m[8]*z+m[12])/w+1)*512,y:(1-(m[1]*x+m[5]*y+m[9]*z+m[13])/w)*384};},gate);
 await page.mouse.click(point.x,point.y);await page.waitForFunction(()=>__portal.view.gameplay.npcConversation?.phase==='menu',null,{timeout:15000});
 await page.locator('[data-ui-id="npc-portal-open"]').click();await page.locator('[data-ui-id="npc-portal:2"]').waitFor();
 await page.screenshot({path:'temp/artifacts/portal-destinations.png'});
 const before=await page.evaluate(()=>({region:__portal.view.gameplay.pose.regionId,gold:__portal.view.gameplay.progression.gold}));
 const caption=await page.locator('[data-ui-id="npc-portal:2"]').getAttribute('aria-label');
 const price=Number(caption.match(/\[(\d+)\]gold/)[1]);
 await page.locator('[data-ui-id="npc-portal:2"]').click();
 if(BigInt(before.gold)<BigInt(price)){
  await page.waitForFunction(()=>__portal.view.gameplay.notices.some(n=>n.key==='UIIT_MSG_INTERACTION_FAIL_NOT_ENOUGH_MONEY'),null,{timeout:10000});
  const after=await page.evaluate(()=>({region:__portal.view.gameplay.pose.regionId,gold:__portal.view.gameplay.progression.gold}));assert.deepEqual(after,before);
 }else{
  await page.waitForFunction(region=>__portal.view.gameplay.pose?.regionId&&__portal.view.gameplay.pose.regionId!==region&&__playableRuntime.sessionState().phase==='world',before.region,{timeout:60000});
  assert.equal(await page.evaluate(()=>__portal.view.gameplay.progression.gold),String(BigInt(before.gold)-BigInt(price)));
 }
 await page.screenshot({path:'temp/artifacts/portal-outcome.png'});
 assert.deepEqual(errors,[]);
 const result=await page.evaluate(()=>({pose:__portal.view.gameplay.pose,entities:__portal.view.entities.filter(e=>e.kind==='npc'||e.kind==='teleport'),stats:__portal.owner.stats()}));
 await writeFile('temp/artifacts/portal-after.json',JSON.stringify({result,errors,qualification:BigInt(before.gold)<BigInt(price)?'live insufficient-gold refusal; paid travel covered by server integration':'live paid travel'},null,2));console.log('City gate live outcome:',BigInt(before.gold)<BigInt(price)?'native insufficient-gold refusal':'paid travel');
}catch(error){console.log('FAILURE',await page.evaluate(()=>({hit:globalThis.__portalHit,pose:__portal.view.gameplay.pose,target:__portal.view.gameplay.target,pending:__portal.view.gameplay.targetPending,conversation:__portal.view.gameplay.npcConversation,stats:__portal.owner.stats()})));await page.screenshot({path:'temp/artifacts/portal-failure.png'});await writeFile('temp/artifacts/portal-failure.json',JSON.stringify({error:String(error),errors,state:await page.evaluate(()=>__portal.view.gameplay)},null,2));throw error;}finally{await browser.close();}

});
