import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,writeFile} from 'node:fs/promises';
import path from 'node:path';
import {launchProbeBrowser} from '../../../../scripts/lib/probeBrowser.mjs';
import {assertCharacterAllowed} from '../../../../scripts/lib/probeCharacter.mjs';
import {openProbeAgentSession,fetchProbeSessionJson,resolveProbeDivisionId} from '../../../../scripts/lib/probeSession.mjs';
import {resetMissionMovementFixture} from '../../../../scripts/lib/missionMovementFixture.mjs';
import {readTextDataRowsSync} from '../../../../scripts/build/shared/textDataIo.mjs';
import {retailTextdataRoot} from '../../../../scripts/build/world/paths.mjs';
import {bootPlayableSession,bindPlayableRuntime,waitPlayableWorld} from './helpers/playable-session.mjs';

// The fixture seeds only level/stats/funds on a disposable, newly created EU
// character. Every mastery, prerequisite, rank and equipment transition below
// goes through the authenticated production protocol; the diagnostic endpoint
// reads the same stat projection as combat and never dictates the outcome.
const damage=process.env.SRO_PASSIVE_FAMILY==='damage';
test('passive '+(damage?'damage':'critical')+' follows learned rank, equipment and authenticated reload',{timeout:180000},async()=>{
 assert.equal(resolveProbeDivisionId(),'test','fixture belongs to the test shard');
 const name=assertCharacterAllowed(damage?'PowerProbe':'PassiveProbe',{context:'passive lifecycle'}),out=damage?'temp/artifacts/passive-damage/live':'temp/artifacts/passive-critical/live';await mkdir(out,{recursive:true});
 const session=await openProbeAgentSession(),endpoint='/development/passive-critical-fixture';
 const fixture=command=>fetchProbeSessionJson(session,endpoint,{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({characterName:name,command})});
 const listing=await fetchProbeSessionJson(session,'/character/list');
 if(!listing.characters.some(c=>c.name===name)) {
  const created=await fetchProbeSessionJson(session,'/character/create',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({divisionId:'test',characterName:name,modelCodename:'CHAR_EU_MAN_NOBLE',heightIndex:0,volumeIndex:0,weaponIndex:3,protectorIndex:1,armorSelected:true,weaponSelected:true})});
  assert.equal(created.nativeResult,1,'ordinary EU character creation');
 }
 const report={wire:[],stages:[],errors:[],fixture:await fixture('seed')};assert.equal(report.fixture.level,30);
 await resetMissionMovementFixture({session,characterName:name,fixture:{id:'passive-critical-mangyang',movementMode:3,start:{regionId:0x60a5,x:986,y:208,z:1020},startYawRadians:0},onLog:console.log});
 const rows=[];for(const[file]of readTextDataRowsSync(path.join(retailTextdataRoot,'skilldata.txt')))for(const r of readTextDataRowsSync(path.join(retailTextdataRoot,file)))if(r[0]==='1')rows.push(r);
 const family=rows.filter(r=>r[3].startsWith(damage?'SKILL_EU_WARRIOR_TWOHANDP_ATTACK_A_':'SKILL_EU_WARRIOR_TWOHANDP_CRITICALUP_A_')).sort((a,b)=>+a[7]-+b[7]);assert.equal(family.length,damage?22:8);
 const bonusAt=rank=>rank?(damage?+family[rank-1][71]:rank+1):0;
 const {browser,page}=await launchProbeBrowser();page.setDefaultTimeout(15000);
 page.on('pageerror',e=>report.errors.push(String(e)));page.on('console',m=>{if(m.text().startsWith('[passive-wire]'))report.wire.push(JSON.parse(m.text().slice(14)));});
 try{
  await page.route('**/runtime/simulation/worker/network/network.ts*',async route=>{const response=await route.fetch();let source=await response.text();const anchor='const frame = codec.decode(new Uint8Array(event.data));';assert.ok(source.includes(anchor));source=source.replace(anchor,anchor+`if([0xb2cb,0xb2ca,0xb034,0x3013,0x3017].includes(frame.opcode))console.log('[passive-wire]'+JSON.stringify({opcode:frame.opcode,payload:Array.from(frame.payload)}));`);await route.fulfill({response,body:source,contentType:'application/javascript'});});
  await bootPlayableSession(page,name);await page.context().tracing.start({screenshots:true,snapshots:true});
  const command=c=>page.evaluate(command=>__playableRuntime.session({kind:'gameplay',command}),c);
  const note=async(stage,bonus)=>{const state=await fixture('status');assert.equal(damage?state.twoHandPowerPercent:state.passiveBonus,bonus,stage);if(damage){assert.equal(state.physicalAttackMin,state.equipmentAttackMin);assert.equal(state.physicalAttackMax,state.equipmentAttackMax);}report.stages.push({stage,...state});console.log('[passive]',stage,JSON.stringify({critical:state.criticalRate,bonus:state.passiveBonus,power:state.twoHandPowerPercent}));};
  const initial=await page.evaluate(()=>{const g=__playableRuntime.gameplay();return {weapon:g.inventory.find(i=>i.slot===6),skills:g.skills,masteries:g.progression.masteries};});
  assert.equal(initial.weapon.typeFlags>>>11,8,'creation weapon must be two-handed');
  const current=family.find(r=>initial.skills.includes(+r[1]));const initialRank=current?+current[7]:0;
  await note('initial',bonusAt(initialRank));
  for(let level=initial.masteries.find(m=>m.id===513)?.level??0;level<30;level++){
   await command({kind:'mastery-train',id:513});await page.waitForFunction(level=>__playableRuntime.gameplay().progression.masteries.find(m=>m.id===513)?.level>level,level);
  }
  const train=async row=>{if(await page.evaluate(id=>__playableRuntime.gameplay().skills.includes(id),+row[1]))return;await command({kind:'skill-train',id:+row[1]});await page.waitForFunction(id=>__playableRuntime.gameplay().skills.includes(id),+row[1]);};
  const learnRequirements=async row=>{for(let i=40;i<=42;i++){if(+row[i]===0)continue;for(const prerequisite of rows.filter(r=>r[2]===row[i]&&+r[7]<=+row[i+3]).sort((a,b)=>+a[7]-+b[7])){const known=await page.evaluate(group=>__playableRuntime.gameplay().skills.some(id=>group.includes(id)),rows.filter(r=>r[2]===prerequisite[2]&&+r[7]>=+prerequisite[7]).map(r=>+r[1]));if(!known){await learnRequirements(prerequisite);await train(prerequisite);}}}};
  const move=async(source,destination)=>{await command({kind:'inventory-move',source,destination,quantity:1});await page.waitForFunction(({destination,ref})=>__playableRuntime.gameplay().inventory.some(i=>i.slot===destination&&i.refObjId===ref),{destination,ref:initial.weapon.refObjId});};
  const bag=await page.evaluate(()=>{const g=__playableRuntime.gameplay();for(let i=13;i<32;i++)if(!g.inventory.some(r=>r.slot===i))return i;});assert.ok(bag>=13);
  if(initialRank===0){await move(6,bag);await learnRequirements(family[0]);await train(family[0]);await note('learned-unequipped',damage?bonusAt(1):0);await move(bag,6);await note('rank-one-equipped',bonusAt(1));}
  if(initialRank<2){await learnRequirements(family[1]);await train(family[1]);await note('rank-two-replaces-one',bonusAt(2));}
  const rank=Math.max(initialRank,2),bonus=bonusAt(rank);
  for(let i=0;i<3;i++){await move(6,bag);await note('unequipped-'+i,damage?bonus:0);await move(bag,6);await note('reequipped-'+i,bonus);}
  await page.screenshot({path:out+'/equipped.png'});
  await page.reload();await bindPlayableRuntime(page);await waitPlayableWorld(page,name);await note('authenticated-reload',bonus);
  const learned=await page.evaluate(()=>__playableRuntime.gameplay().skills);assert.equal(family.filter(r=>learned.includes(+r[1])).length,1,'only current rank survives reload');
  assert.deepEqual(report.errors,[]);report.verdict='PASS';
 }catch(e){report.failure=String(e);throw e;}
 finally{await page.context().tracing.stop({path:out+'/trace.zip'}).catch(()=>{});await writeFile(out+'/incident.json',JSON.stringify(report,null,2));await browser.close();}
});
