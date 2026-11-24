import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {assertCharacterAllowed} from '../../../../scripts/lib/probeCharacter.mjs';
import {bootPlayableSession} from './helpers/playable-session.mjs';
import {holdProbeRuntime} from './helpers/hold-runtime.mjs';

// Opt-in: requires an already dead scratch actor above level 10. No character
// mutation, injected death packet, substituted state or automatic resurrection.
test('live corpse canvas selection reopens the rescued-by-player prompt twice',{timeout:120000,skip:!process.env.SRO_REBIRTH_SELF_CHARACTER},async()=>{
 const character=assertCharacterAllowed(process.env.SRO_REBIRTH_SELF_CHARACTER,{context:'rebirth self-selection regression'});
 const out='temp/artifacts/rebirth-self-selection',evidence={character,phases:[],errors:[]};
 await mkdir(out,{recursive:true});const {browser,page}=await launchProbeBrowser();let tracing=false;
 const capture=async label=>{const state=await page.evaluate(()=>{const g=__playableRuntime.gameplay();return {gid:g.localGid,level:g.progression?.level,life:__playableRuntime.entity(g.localGid)?.appearanceState?.[0],hp:g.vitals.find(v=>v.gid===g.localGid)?.hp,pose:g.pose,pending:g.rebirthPending,prompt:!!document.querySelector('[data-ui-id="rebirth-point"]')};});evidence.phases.push({label,...state});return state;};
 try{
  await holdProbeRuntime(page);page.on('pageerror',error=>evidence.errors.push(error.message));
  // Read-only access to the production picker; clicks still enter through the
  // real canvas mousedown listener, platform, runtime and UI owners.
  await page.route('**/runtime/renderer/renderer.ts*',async route=>{
   const response=await route.fetch(),source=await response.text(),marker='export function createRenderer(';
   assert.ok(source.includes(marker));
   await route.fulfill({response,body:source.replace(marker,'function createObservedRenderer(')+'\nexport function createRenderer(...args){const owner=createObservedRenderer(...args);globalThis.__rebirthPick=owner.pickEntity;return owner;}'});
  });
  await page.addInitScript(()=>{
   globalThis.__rebirthCommands=[];const Original=Worker;
   globalThis.Worker=class extends Original {postMessage(message,...rest){if(message?.kind==='session'&&message.command?.kind==='gameplay')__rebirthCommands.push({at:performance.now(),kind:message.command.command.kind});return super.postMessage(message,...rest);}};
  });
  console.log('[rebirth-self] authenticated boot',character);await bootPlayableSession(page,character);
  const initial=await capture('boot');assert.equal(initial.life,2,'scratch actor must already be dead');assert.ok(initial.level>10);
  const prompt=page.locator('[data-ui-id="rebirth-point"]'),alternate=page.locator('[data-ui-id="rebirth-alternate"]');
  await prompt.waitFor({timeout:15000});await page.context().tracing.start({screenshots:true,snapshots:true});tracing=true;
  await capture('automatic-prompt');await page.screenshot({path:out+'/before.png'});await page.evaluate(()=>{__rebirthCommands.length=0;});
  for(let attempt=1;attempt<=2;attempt++){
   await alternate.click();await prompt.waitFor({state:'detached',timeout:3000});await capture('dismissed-'+attempt);
   const point=await page.evaluate(()=>{
    const gid=__playableRuntime.gameplay().localGid,c=document.querySelector('canvas'),r=c.getBoundingClientRect();
    for(let y=.25;y<.8;y+=.015)for(let x=.25;x<.75;x+=.015)if(__rebirthPick(x,y,0)===gid)return {x:r.left+x*r.width,y:r.top+y*r.height};
    return null;
   });assert.ok(point,'rendered own corpse must be pickable');evidence.phases.push({label:'corpse-hit-'+attempt,point});
   await page.mouse.click(point.x,point.y);await prompt.waitFor({timeout:3000});
   const state=await capture('reopened-'+attempt);assert.equal(state.life,2);assert.deepEqual(state.pose,initial.pose);
   console.log('[rebirth-self] corpse click reopened',attempt);
  }
  await page.screenshot({path:out+'/reopened.png'});
  evidence.commands=await page.evaluate(()=>__rebirthCommands);
  assert.deepEqual(evidence.commands.filter(c=>['select','rebirth','move','ground-move','attack','cos-attack','pickup'].includes(c.kind)),[],'dismissal and self-selection must send no player action');
  assert.deepEqual(evidence.errors,[]);evidence.verdict='PASS SUCCESS';
 }catch(error){evidence.error=String(error);throw error;}
 finally{
  if(tracing)await page.context().tracing.stop({path:out+'/trace.zip'});
  await page.evaluate(()=>__playableRuntime.session({kind:'logout'})).catch(()=>{});await browser.close();
  await writeFile(out+'/incident.json',JSON.stringify(evidence,null,2));
 }
});
