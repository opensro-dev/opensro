import {uiAdmissionReference} from './helpers/ui-admission-reference.mjs';
import {test} from "node:test";
import assert from "node:assert/strict";
import {launchProbeBrowser} from "../../../../scripts/lib/probeBrowser.mjs";
import {CLIENT_NEXT_BASE_URL} from "../../../../scripts/lib/probeEndpoints.mjs";
import {holdProbeRuntime} from "./helpers/hold-runtime.mjs";
import {mkdir} from 'node:fs/promises';
import {runPython} from '../../../../scripts/build/shared/pythonRun.mjs';
test("GPU UI routes IME, focus, selection and authoritative inventory without marker DOM",{timeout:45000},async()=>{
 const {browser,page}=await launchProbeBrowser();const verifyAdmissionReference=await uiAdmissionReference(page);const errors=[];page.on("pageerror",e=>errors.push(e.message));
 try{
  await holdProbeRuntime(page);await page.setViewportSize({width:1600,height:900});await page.goto(CLIENT_NEXT_BASE_URL);await page.waitForFunction(()=>document.querySelector("output")?.textContent?.includes("runtime: running"));
  await page.evaluate(async()=>{
   const {runtime}=await import(Array.from(document.scripts).find(s=>s.src&&new URL(s.src).pathname==='/src/bootstrap.ts').src);runtime.dispose();
   const {createUi}=await import('/src/engine/runtime/ui/ui.ts');const {createPlatform}=await import('/src/engine/runtime/platform/platform.ts');const {createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts');
   const canvas=document.querySelector('canvas'),renderer=createRenderer(canvas),commands=[],inputs=[],sounds=[],audioChanges=[],bindingChanges=[],videoChanges=[];let scenes=0,textures=0;
   const {createAssets}=await import('/src/engine/runtime/assets/assets.ts'),assets=createAssets();
   const ui=createUi({assets,commands:c=>commands.push(c),publish:s=>{scenes++;try{renderer.setUi(s);}catch(error){throw Error(String(error)+' '+JSON.stringify(s.quads.filter(q=>q.rect[2]<0||q.rect[3]<0||q.clip[2]<0||q.clip[3]<0||q.color.some(v=>v<0||v>1))));}},texture:(id,image)=>{textures++;renderer.setUiTexture(id,image);},base:location.origin,apiBase:'http://fixture.invalid',clickSound:()=>sounds.push('click'),sound:kind=>sounds.push(kind),audioPreference:(value,commit)=>audioChanges.push({value,commit}),saveBindings:value=>bindingChanges.push(value),saveVideo:value=>{videoChanges.push(value);renderer.videoOptions(value);}});
   const platform=createPlatform(canvas,document.querySelector('output'),()=>{},e=>inputs.push(e),()=>{},ui.event,ui.blocks);
   const state={frontend:{phase:"login",generation:1,elapsed:0,alpha:1,logoAlpha:1,error:null},session:{phase:'signed-out',revision:1,servers:[{id:'test',name:'Fixture server',operating:true,onlinePlayers:1,capacity:100}]},gameplay:null,entities:[],width:1600,height:900,worldReady:true};
   window.fixture={assets,ui,renderer,platform,state,commands,inputs,sounds,audioChanges,bindingChanges,videoChanges,stats:()=>({scenes,textures}),draw(){const semantic=ui.step({...state},performance.now());if(semantic)platform.presentUi(semantic);renderer.frame({width:1600,height:900});}};
   fixture.settle=async()=>{const deadline=performance.now()+15000;let stable=0;while(performance.now()<deadline){fixture.draw();if(ui.stats().pending===0&&ui.stats().windowReady&&!(ui.stats().windowMissing?.length)&&(state.session?.phase!=="world"||!!document.querySelector('[data-ui-id="hotbar:1"], [data-ui-id="invite-drag"], [data-ui-id="rebirth-drag"], [data-ui-id="disconnect-drag"], [data-ui-id="quest-abandon-yes"], [data-ui-id="skill-confirm-ok"]'))){if(++stable===5)return;}else stable=0;await new Promise(requestAnimationFrame);}throw Error('UI resources did not settle: '+JSON.stringify({ui:ui.stats(),assets:assets.health(),renderer:renderer.error()}));};
   await fixture.settle();state.session={...state.session,revision:2};window.fixture.draw();commands.length=0;sounds.length=0;
  });
  await page.evaluate(()=>{fixture.ui.event({kind:'activate',id:'native:servers'});fixture.draw();fixture.ui.event({kind:'key',code:'Escape'});fixture.draw();});
  assert.deepEqual(await page.evaluate(()=>fixture.sounds),['click','open','close']);
  await page.evaluate(()=>{fixture.state.session={...fixture.state.session,revision:3};fixture.draw();fixture.commands.length=0;fixture.sounds.length=0;});
  await page.getByRole('textbox',{name:'Account',exact:true}).focus();
  await page.evaluate(()=>{const input=document.querySelector('[data-ui-id="account"]');input.dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true,data:'?'}));input.value='\u6e2c\u8a66';input.setSelectionRange(2,2);input.dispatchEvent(new InputEvent('input',{bubbles:true,isComposing:true}));input.dispatchEvent(new KeyboardEvent('keydown',{bubbles:true,code:'Enter',key:'Enter',isComposing:true}));fixture.draw();});
  assert.equal(await page.evaluate(()=>fixture.commands.length),0);assert.deepEqual(await page.evaluate(()=>fixture.sounds),[]);
  await page.evaluate(()=>document.querySelector('[data-ui-id="account"]').dispatchEvent(new CompositionEvent('compositionend',{bubbles:true,data:'??'})));
  await page.getByLabel('Password',{exact:true}).fill('fixture-secret');await page.evaluate(()=>fixture.draw());
  await page.getByRole('button',{name:'Connect',exact:true}).click();await page.evaluate(()=>fixture.draw());
  assert.deepEqual(await page.evaluate(()=>({kind:fixture.commands[0]?.kind,id:fixture.commands[0]?.id,passwordKept:document.querySelector('[data-ui-id="password"]').value,gameKeys:fixture.inputs.filter(e=>e.kind==='key').length})),{kind:'login',id:'\u6e2c\u8a66',passwordKept:'fixture-secret',gameKeys:0});
  assert.deepEqual(await page.evaluate(()=>fixture.sounds),['click','message']);
  await page.evaluate(()=>{fixture.state.frontend={phase:'dock',generation:2,elapsed:3,alpha:1,logoAlpha:0,cameraMoving:false,selectedCharacter:'Fixture'};fixture.state.session={phase:'character-select',revision:2,characters:[{id:1,name:'Fixture',level:1,deletePending:false,maxHp:100,maxMp:50}]};fixture.draw();});
  await page.getByRole('button',{name:'Start',exact:true}).click();assert.equal(await page.evaluate(()=>fixture.commands.at(-1).kind),'enter-world');
  await page.evaluate(()=>{fixture.state.frontend=null;fixture.state.session={phase:'world',revision:3,character:'Fixture'};fixture.state.gameplay={revision:1,localGid:1,pose:{regionId:0x271b,x:900,y:0,z:900,angle:0},vitals:[{gid:1,hp:100,mp:50}],inventory:[{slot:13,refObjId:42,quantity:2}],inventorySlotCount:45,equipmentSlotCount:13,inventoryPending:false,target:0,casts:[]};fixture.state.entities=Array.from({length:400},(_,i)=>({gid:i+1,regionId:0x271b,x:800+i,z:900,y:0,kind:'npc',name:'NPC '+i}));fixture.draw();});
  await page.evaluate(()=>{fixture.state.gameplay={...fixture.state.gameplay,target:901};fixture.state.entities=[...fixture.state.entities,{gid:901,refObjId:3810,regionId:0x271b,x:900,y:0,z:900,kind:'ground-item',name:'Gold'}];fixture.ui.event({kind:'focus',id:null});fixture.draw();});
  assert.equal(await page.getByRole('button',{name:'Pick up',exact:true}).count(),0,'native pickup belongs to world gestures, not a substitute HUD button');
  await page.evaluate(()=>{fixture.state.gameplay={...fixture.state.gameplay,target:902,quickSlots:[{slot:1,kind:0x4a,payload:5000}],activeCos:{gid:902,refObjId:3914,hp:100,mp:0,status:0,dead:false}};fixture.state.entities=[...fixture.state.entities,{gid:902,refObjId:3914,regionId:0x271b,x:900,y:0,z:900,kind:'cos',name:'Horse',ownerGid:1}];fixture.ui.event({kind:'focus',id:null});fixture.draw();});
  await page.evaluate(()=>fixture.settle());await page.locator('[data-ui-id="hotbar:1"]').click();assert.deepEqual(await page.evaluate(()=>fixture.commands.at(-1)),{kind:'gameplay',command:{kind:'mount',gid:902}});
  await page.evaluate(()=>{fixture.state.gameplay={...fixture.state.gameplay,target:0};fixture.ui.event({kind:'focus',id:null});fixture.draw();});
  assert.equal(await page.getByRole('button',{name:/^NPC \d+$/}).count(),0,'World entities must not become per-entity DOM controls');
  await page.evaluate(async()=>{fixture.ui.event({kind:'key',code:'Escape'});await fixture.settle();});
  const fpsBounds=await page.locator('#fps-chip').boundingBox();assert.ok(fpsBounds&&fpsBounds.x+fpsBounds.width<=1600-129-6,'FPS toggle stays outside the native minimap');
  assert.deepEqual(await page.evaluate(()=>['open-window:Option','open-window:Game Guide','system-restart','system-exit'].map(id=>document.querySelector(`[data-ui-id="${id}"]`)?.textContent)),['Option','Help','Restart','Exit']);
  await mkdir('temp/artifacts/native-windows',{recursive:true});await page.screenshot({path:'temp/artifacts/native-windows/system.png'});
  await runPython(['tools/compare-native-system.py','temp/artifacts/native-windows/system.png'],{task:'Retail System window pixel comparison'});
  // Use a published map neighborhood; 0x5c62 has no minimap tiles.
  await page.evaluate(async()=>{const data=await (await fetch('/assets/data/skillUi.json')).json();fixture.state.gameplay={...fixture.state.gameplay,progression:{level:20,skillPoints:100,masteries:[257,258,259,273,274,275,276].map(id=>({id,level:10}))},skills:data.skills.filter(s=>s.mastery===257&&s.level===1).map(s=>s.id),quests:[],academy:{member:false,page:0,pages:0,rows:[],request:null,result:null}};});
  for(const name of ['Character','Party','Skills','Quests','Party Matching','Academy Matching','Option']){
   await page.evaluate(name=>{fixture.ui.event({kind:'activate',id:'open-window:'+name});fixture.draw();},name);await page.evaluate(()=>fixture.settle());
   await page.screenshot({path:'temp/artifacts/native-windows/'+name.toLowerCase()+'.png'});
   if(name==='Skills'||name==='Quests'){assert.deepEqual(await page.evaluate(()=>fixture.ui.stats().windowMissing),[],name+' admits all native resources');}if(name==='Skills'||name==='Quests')assert.equal(await page.locator('[data-ui-id="main-popup-drag"]').count(),1);
   if(name==='Academy Matching'){assert.equal(await page.locator('[data-ui-id="academy-refresh"]').count(),1);assert.deepEqual(await page.evaluate(()=>fixture.ui.stats().windowMissing),[]);}
   if(name==='Party Matching')assert.ok(await page.locator('[data-ui-id^="party-match:"]').count()>0);
   if(name==='Party Matching'){await page.keyboard.press('Escape');await page.evaluate(()=>fixture.draw());await page.keyboard.press('e');await page.evaluate(()=>fixture.settle());assert.ok(await page.locator('[data-ui-id^="party-match:"]').count()>0);}
   if(name==='Skills'){
    await page.evaluate(async()=>{const data=await (await fetch('/assets/data/skillUi.json')).json();fixture.state.gameplay={...fixture.state.gameplay,skillCatalog:data.skills.filter(s=>s.group===174&&s.level<=2).map(s=>({...s,name:'Smashing Series',nameSymbol:s.name,icon:s.icon.replace(/^icon[\\/]/,''),spCost:1,trainable:true,targetRequired:true,cooldownMs:0,masteries:[],prerequisites:[]}))};await fixture.settle();});
    await page.locator('[data-ui-id="skill-learn:291"]').click();await page.evaluate(()=>fixture.settle());assert.equal(await page.locator('[data-ui-id="skill-confirm-ok"]').count(),1);await page.screenshot({path:'temp/artifacts/native-windows/skill-learn.png'});
    await page.evaluate(()=>{fixture.commands.length=0;fixture.state.gameplay={...fixture.state.gameplay,progression:{...fixture.state.gameplay.progression,skillPoints:0}};fixture.draw();});await page.keyboard.press('Enter');await page.evaluate(()=>fixture.draw());assert.equal(await page.evaluate(()=>fixture.commands.length),0,'confirm revalidates changed authoritative SP');
    await page.evaluate(async()=>{fixture.state.gameplay={...fixture.state.gameplay,progression:{...fixture.state.gameplay.progression,skillPoints:100}};await fixture.settle();});await page.locator('[data-ui-id="skill-learn:291"]').click();await page.evaluate(()=>fixture.settle());await page.locator('[data-ui-id="skill-confirm-ok"]').click();assert.deepEqual(await page.evaluate(()=>fixture.commands.at(-1)),{kind:'gameplay',command:{kind:'skill-train',id:291}});
   }
   if(name==='Quests'){
    await page.evaluate(async()=>{fixture.state.gameplay={...fixture.state.gameplay,quests:[{refId:3,u08:0x11,u09:0,u10:1,flags:0,contents:[{tag:1,kind:1,description:'SN_QNO_CH_SMITH_1',objectiveSentinel:false,objectiveValues:[]}],targetIds:[]}]};await fixture.settle();});
    assert.ok((await page.locator('[data-ui-id="quest:3"]').getAttribute('aria-label')).length>6,'quest title resolves from textquest');
    await page.locator('[data-ui-id="quest-expand:3"]').click();await page.evaluate(()=>fixture.settle());await page.screenshot({path:'temp/artifacts/native-windows/quest-expanded.png'});
    await page.locator('[data-ui-id="quest:3"]').click();await page.evaluate(()=>fixture.settle());assert.equal(await page.locator('[data-ui-id="quest-details-close"]').count(),1);await page.screenshot({path:'temp/artifacts/native-windows/quest-details.png'});
    await page.locator('[data-ui-id="quest-abandon"]').click();await page.evaluate(()=>fixture.settle());assert.equal(await page.locator('[data-ui-id="quest-abandon-yes"]').count(),1);await page.screenshot({path:'temp/artifacts/native-windows/quest-giveup.png'});
    await page.locator('[data-ui-id="quest-abandon-no"]').click();await page.evaluate(()=>fixture.draw());await page.locator('[data-ui-id="quest-details-close"]').click();await page.evaluate(()=>fixture.draw());
   }
   const optionAnchor=name==='Option'?await page.locator('[data-ui-id="option-tab:0"]').boundingBox():null;
   if(name==='Option')for(let tab=1;tab<5;tab++){
    await page.evaluate(tab=>{fixture.ui.event({kind:'activate',id:'option-tab:'+tab});fixture.draw();},tab);await page.evaluate(()=>fixture.settle());
    await page.screenshot({path:'temp/artifacts/native-windows/option-'+tab+'.png'});
    const anchor=await page.locator('[data-ui-id="option-tab:0"]').boundingBox();assert.equal(anchor.x,optionAnchor.x);assert.equal(anchor.y,optionAnchor.y);
    if(tab===3){await page.locator('[data-ui-id="option-bind:0"]').click();await page.keyboard.press('B');await page.evaluate(()=>fixture.draw());assert.match(await page.locator('[data-ui-id="option-bind:0"]').getAttribute('aria-label'),/ B$/);await page.locator('[data-ui-id="option-default"]').click();await page.evaluate(()=>fixture.draw());}

    if(tab===1){
     const slider=page.locator('[data-ui-id="option-audio:bgm"]');await slider.fill('83');await page.evaluate(()=>fixture.draw());assert.equal(await page.evaluate(()=>fixture.audioChanges.at(-1).value.bgm),83);
     await page.locator('[data-ui-id="option-mute:muteEnvironment"]').click();await page.evaluate(()=>fixture.settle());assert.equal(await page.evaluate(()=>fixture.audioChanges.at(-1).value.muteEnvironment),true);
     await page.locator('[data-ui-id="option-default"]').click();await page.evaluate(()=>fixture.draw());assert.equal(await slider.inputValue(),'50');
    }

   }
  }
  await page.evaluate(async()=>{fixture.ui.event({kind:'activate',id:'option-tab:0'});await fixture.settle();});await page.locator('[data-ui-id="option-video-combo:2"]').click();await page.evaluate(()=>fixture.settle());assert.equal(await page.locator('[data-ui-id^="option-video-choice:2:"]').count(),5);
  await page.evaluate(async()=>{fixture.ui.event({kind:'activate',id:'option-tab:0'});for(let i=0;i<5;i++)fixture.ui.event({kind:'activate',id:'option-video-down'});await fixture.settle();});
  await page.locator('[data-ui-id="option-video-combo:8"]').click();await page.evaluate(()=>fixture.draw());await page.locator('[data-ui-id="option-video-choice:8:0"]').click();await page.evaluate(()=>fixture.draw());await page.locator('[data-ui-id="option-apply"]').click();await page.evaluate(()=>fixture.settle());assert.equal(await page.evaluate(()=>fixture.videoChanges.at(-1).records[0][8]),0);
  await page.screenshot({path:'temp/artifacts/native-windows/video-controls-applied.png'});
  await page.evaluate(()=>{fixture.ui.event({kind:'activate',id:'open-window:System'});fixture.draw();});

  await page.evaluate(()=>{fixture.ui.event({kind:'key',code:'Escape'});fixture.draw();});
  await page.waitForFunction(()=>{fixture.draw();return document.querySelector('[data-ui-id="hud-menu"]');});
  await page.locator('[data-ui-id="hud-menu"]').click();await page.evaluate(()=>fixture.draw());
  const before=await page.evaluate(()=>{fixture.draw();return fixture.stats();});await page.evaluate(()=>{for(let i=0;i<240;i++)fixture.draw();});assert.deepEqual(await page.evaluate(()=>fixture.stats()),before);
  await page.getByRole('button',{name:'Inventory',exact:true}).click();await page.waitForFunction(()=>{fixture.draw();return document.querySelector('[data-ui-id="slot:13"]');});await page.evaluate(()=>fixture.settle());
  await page.locator('[data-ui-id="slot:13"]').click();await page.evaluate(()=>fixture.draw());await page.locator('[data-ui-id="slot:14"]').click();await page.evaluate(()=>fixture.draw());
  assert.deepEqual(await page.evaluate(()=>fixture.commands.at(-1)),{kind:'gameplay',command:{kind:'inventory-move',source:13,destination:14,quantity:2}});
  assert.equal(await page.evaluate(()=>fixture.state.gameplay.inventory[0].slot),13);
  await page.evaluate(()=>{fixture.commands.length=0;});
  const sourceSlot=await page.locator('[data-ui-id="slot:13"]').boundingBox(),destinationSlot=await page.locator('[data-ui-id="slot:15"]').boundingBox();
  await page.mouse.move(sourceSlot.x+16,sourceSlot.y+16);await page.mouse.down();await page.mouse.move(destinationSlot.x+16,destinationSlot.y+16,{steps:5});await page.evaluate(()=>fixture.draw());await page.mouse.up();
  assert.deepEqual(await page.evaluate(()=>fixture.commands),[{kind:'gameplay',command:{kind:'inventory-move',source:13,destination:15,quantity:2}}]);

  await page.evaluate(()=>{fixture.ui.event({kind:'activate',id:'open-window:Alchemy'});fixture.draw();});
  assert.deepEqual(await page.evaluate(()=>fixture.commands.at(-1)),{kind:'gameplay',command:{kind:'alchemy-open'}});
  await page.evaluate(()=>{fixture.state.gameplay={...fixture.state.gameplay,inventory:[{slot:13,refObjId:1,typeFlags:0x2c,name:'Fixture sword',quantity:1},{slot:14,refObjId:2,typeFlags:0xf6c,name:'Fixture ticket',quantity:1}]};fixture.draw();});
  await page.locator('[data-ui-id="alchemy-slot:13"]').click();await page.evaluate(()=>fixture.draw());await page.locator('[data-ui-id="alchemy-slot:14"]').click();await page.evaluate(()=>fixture.draw());await page.locator('[data-ui-id="alchemy-start"]').click();
  assert.deepEqual(await page.evaluate(()=>fixture.commands.at(-1)),{kind:'gameplay',command:{kind:'alchemy-start',mode:'reinforce',slots:[13,14]}});
  await page.locator('[data-ui-id="alchemy-mode:compound"]').click();await page.evaluate(()=>fixture.draw());
  await page.locator('[data-ui-id="alchemy-slot:13"]').click();await page.evaluate(()=>fixture.draw());await page.locator('[data-ui-id="alchemy-slot:14"]').click();await page.evaluate(()=>fixture.draw());await page.locator('[data-ui-id="alchemy-quantity"]').fill('1');await page.evaluate(()=>fixture.draw());await page.locator('[data-ui-id="alchemy-start"]').click();
  assert.deepEqual(await page.evaluate(()=>fixture.commands.at(-1)),{kind:'gameplay',command:{kind:'alchemy-start',mode:'compound',slots:[13,14],quantity:1}});
  await page.evaluate(()=>{fixture.state.gameplay={...fixture.state.gameplay,inventoryPending:true,alchemy:{pending:true,mode:'compound',visible:true}};fixture.draw();});await page.locator('[data-ui-id="alchemy-cancel"]').click();assert.equal(await page.evaluate(()=>fixture.commands.at(-1).command.kind),'alchemy-cancel');
  await page.evaluate(()=>{fixture.state.gameplay={...fixture.state.gameplay,inventoryPending:false};fixture.draw();});
  await page.evaluate(()=>{fixture.state.gameplay={...fixture.state.gameplay,gacha:{visible:true,phase:'idle'}};fixture.draw();});await page.locator('[data-ui-id="gacha-card:14"]').click();await page.evaluate(()=>fixture.draw());await page.locator('[data-ui-id="gacha-roll"]').click();
  assert.deepEqual(await page.evaluate(()=>fixture.commands.at(-1)),{kind:'gameplay',command:{kind:'gacha-roll',entry:1,slot:14}});
  await page.getByRole('button',{name:'Inventory',exact:true}).click();await page.waitForFunction(()=>{fixture.draw();return document.querySelector('[data-ui-id="slot:13"]');});await page.evaluate(()=>fixture.settle());assert.equal(await page.locator('[data-ui-id="gacha-roll"]').count(),0,'Closing must not reopen from a stale visible projection');
  await mkdir('temp/artifacts/dialog-audit',{recursive:true});
  for(const type of [1,5]){
   await page.evaluate(type=>{fixture.state.entities=[{gid:1,regionId:0x271b,x:900,y:0,z:900,kind:'player',name:'Fixture',guildName:'Silkroad'}];fixture.state.gameplay={...fixture.state.gameplay,social:{invitation:{type,gid:1}}};fixture.draw();},type);
   await page.evaluate(()=>fixture.settle());const drag=page.locator('[data-ui-id="invite-drag"]'),before=await drag.boundingBox();
   await page.mouse.move(before.x+40,before.y+16);await page.mouse.down();await page.mouse.move(before.x+110,before.y+46,{steps:5});await page.evaluate(()=>fixture.draw());await page.mouse.up();
   const after=await drag.boundingBox();assert.equal(after.x,before.x+70);assert.equal(after.y,before.y+30);
   await page.screenshot({path:'temp/artifacts/dialog-audit/'+(type===1?'party':'guild')+'.png'});
   await page.locator('[data-ui-id="invite-refuse"]').click();assert.deepEqual(await page.evaluate(()=>fixture.commands.at(-1)),{kind:'gameplay',command:{kind:'social-consent',accept:false}});
   await page.evaluate(()=>{fixture.state.gameplay={...fixture.state.gameplay,social:{invitation:null}};fixture.draw();});
  }
  await page.evaluate(()=>{fixture.state.entities=[{gid:1,regionId:0x271b,x:900,y:0,z:900,kind:'player',name:'Fixture',appearanceState:[2,0,0]}];fixture.state.gameplay={...fixture.state.gameplay,progression:{level:10,masteries:[]}};fixture.draw();});
  await page.evaluate(()=>fixture.settle());
  await mkdir('temp/artifacts/death-dialog',{recursive:true});await page.screenshot({path:'temp/artifacts/death-dialog/fixed.png'});
  const title=page.locator('[data-ui-id="rebirth-drag"]'),start=await title.boundingBox();
  await page.mouse.move(start.x+80,start.y+16);await page.mouse.down();await page.mouse.move(start.x+180,start.y+56,{steps:5});await page.evaluate(()=>fixture.draw());await page.mouse.up();
  const moved=await title.boundingBox();assert.equal(moved.x,start.x+100);assert.equal(moved.y,start.y+40);
  await page.screenshot({path:'temp/artifacts/death-dialog/dragged.png'});
  await page.evaluate(()=>{fixture.inputs.length=0;});await page.mouse.move(500,300);await page.mouse.down({button:'right'});await page.mouse.move(530,320,{steps:3});await page.mouse.up({button:'right'});
  assert.ok(await page.evaluate(()=>fixture.inputs.some(e=>e.kind==='pointer'&&e.buttons===2)),'RMB camera input reaches the world while death prompt is open');
  await page.locator('[data-ui-id="chat-text"]').fill('Still here');await page.evaluate(()=>fixture.draw());
  assert.equal(await page.locator('[data-ui-id="chat-text"]').inputValue(),'Still here');
  await page.locator('[data-ui-id="rebirth-point"]').click();assert.deepEqual(await page.evaluate(()=>fixture.commands.at(-1)),{kind:'gameplay',command:{kind:'rebirth',choice:1}});
  await page.evaluate(()=>{fixture.state.session={phase:'disconnected',revision:4};fixture.draw();});
  const disconnectDrag=page.locator('[data-ui-id="disconnect-drag"], [data-ui-id="quest-abandon-yes"], [data-ui-id="skill-confirm-ok"]'),disconnectStart=await disconnectDrag.boundingBox();
  await page.mouse.move(disconnectStart.x+40,disconnectStart.y+16);await page.mouse.down();await page.mouse.move(disconnectStart.x+90,disconnectStart.y+46,{steps:3});await page.evaluate(()=>fixture.draw());await page.mouse.up();
  assert.equal((await disconnectDrag.boundingBox()).x,disconnectStart.x+50);
  await page.screenshot({path:'temp/artifacts/dialog-audit/disconnect.png'});
  await page.locator('[data-ui-id="disconnect-confirm"]').click();assert.equal(await page.evaluate(()=>fixture.commands.at(-1).kind),'logout');
  await page.evaluate(()=>{fixture.ui.dispose();fixture.platform.dispose();fixture.renderer.dispose();fixture.assets.dispose();});assert.equal(await page.locator('[data-gpu-ui]').count(),0);assert.deepEqual(errors,[]);
 }finally{try{await verifyAdmissionReference();}finally{await browser.close();}}
});
test("GPU UI alpha and clipping draw after the scene",{timeout:30000},async()=>{
 const {browser,page}=await launchProbeBrowser();const verifyAdmissionReference=await uiAdmissionReference(page);try{
  await holdProbeRuntime(page);await page.goto(CLIENT_NEXT_BASE_URL);const pixels=await page.evaluate(async()=>{
   const {runtime}=await import(Array.from(document.scripts).find(s=>s.src&&new URL(s.src).pathname==='/src/bootstrap.ts').src);runtime.dispose();const {createRenderer}=await import('/src/engine/runtime/renderer/renderer.ts');const canvas=document.querySelector('canvas'),renderer=createRenderer(canvas);
   const {defaultVideoOptions}=await import('/src/engine/foundation/rendering/video-options.ts');const video=defaultVideoOptions();video.records[0][8]=0;video.records[0][9]=0;renderer.videoOptions(video);
   const uiScene={revision:1,width:100,height:100,quads:[{rect:[10,10,80,80],uv:[0,0,1,1],color:[1,0,0,1],texture:'',clip:[10,10,30,80]},{rect:[20,20,10,10],uv:[0,0,1,1],color:[0,1,0,.5],texture:'',clip:[0,0,100,100]}]};renderer.setUi(uiScene);
   const started=performance.now();while(renderer.phase()==="starting"&&performance.now()-started<15000)await new Promise(resolve=>requestAnimationFrame(resolve));if(renderer.phase()!=="running")throw new Error(renderer.error()??"Renderer did not start");renderer.frame({width:100,height:100});
   if(renderer.error())throw new Error(renderer.error());const copy=new OffscreenCanvas(100,100),context=copy.getContext('2d');const image=await createImageBitmap(canvas);context.drawImage(image,0,0);image.close();const points=[[15,15],[25,25],[50,50]].map(([x,y])=>[...context.getImageData(x,y,1,1).data]);uiScene.revision=2;uiScene.quads[1].rect[0]=50;renderer.setUi(uiScene);renderer.frame({width:100,height:100});
   const movedImage=await createImageBitmap(canvas);context.drawImage(movedImage,0,0);movedImage.close();const moved=[[25,25],[55,25]].map(([x,y])=>[...context.getImageData(x,y,1,1).data]);
   renderer.setUi(null);renderer.frame({width:100,height:100});const clearedImage=await createImageBitmap(canvas);context.drawImage(clearedImage,0,0);clearedImage.close();const cleared=[...context.getImageData(15,15,1,1).data];renderer.dispose();return {points,moved,cleared};
  });assert.deepEqual(pixels.points[0],[255,0,0,255]);assert.ok(pixels.points[1][0]>=126&&pixels.points[1][0]<=129&&pixels.points[1][1]>=126&&pixels.points[1][1]<=129);assert.ok(pixels.points[2][0]<20);assert.deepEqual(pixels.moved[0],[255,0,0,255]);assert.ok(pixels.moved[1][1]>100);assert.ok(pixels.cleared[0]<20);
 }finally{try{await verifyAdmissionReference();}finally{await browser.close();}}
});
