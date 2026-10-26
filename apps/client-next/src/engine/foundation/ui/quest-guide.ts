import type {GuideArticle} from './guide-catalog';
import {guideTokens} from './guide-content';
import {NATIVE_LEVEL_CAP} from '../gameplay/level-cap';
import type {UiQuad} from '@/engine/contracts/ui';

interface QuestRecord {readonly id:number;readonly symbol:string;readonly level:number;readonly contentKey:string;readonly prerequisites:readonly number[];}
interface QuestArticle extends GuideArticle {readonly contentKey:string;}
export interface QuestGuideCatalog {readonly records:readonly QuestRecord[];readonly articles:readonly QuestArticle[];}
export interface QuestGuideRow extends GuideArticle {readonly color:UiQuad['color'];}
function record(v:unknown):v is Record<string,unknown>{return typeof v==='object'&&v!==null&&!Array.isArray(v);}
function id(v:unknown):v is number{return typeof v==='number'&&Number.isSafeInteger(v)&&v>0;}

export function decodeQuestGuide(guide:unknown,quests:unknown,levelCap:number=NATIVE_LEVEL_CAP):QuestGuideCatalog{
 if(!record(guide)||!Array.isArray(guide.rows)||!record(quests)||!Array.isArray(quests.guideRecords)||!record(quests.guideTextEntries))throw Error('Missing quest guide metadata');
 const records:QuestRecord[]=[],articles:QuestArticle[]=[],ids=new Set<number>();
 for(const row of quests.guideRecords){
  if(!record(row)||!id(row.id)||ids.has(row.id)||typeof row.symbol!=='string'||!row.symbol||typeof row.contentKey!=='string'||typeof row.level!=='number'||!Number.isInteger(row.level)||row.level<0||row.level>levelCap||!Array.isArray(row.prerequisites)||!row.prerequisites.every(id))throw Error('Invalid quest guide record');
  records.push({id:row.id,symbol:row.symbol,contentKey:row.contentKey,level:row.level,prerequisites:[...row.prerequisites]});ids.add(row.id);
 }
 for(const row of records)if(row.prerequisites.some(p=>!ids.has(p)))throw Error('Missing prerequisite quest record');
 const entries=quests.guideTextEntries;
 const text=(key:string)=>{const value=entries[key];if(value!==undefined&&typeof value!=='string')throw Error('Invalid quest guide text');return value;};
 for(const row of guide.rows){
  if(!record(row))throw Error('Invalid quest guide article');
  if(!row.enabled||typeof row.id==='number'&&row.id<100000)continue;
  if(!id(row.id)||row.id<100000||(row.depth!==0&&row.depth!==1)||typeof row.parentOrCount!=='number'||!Number.isSafeInteger(row.parentOrCount)||typeof row.menuKey!=='string'||typeof row.contentKey!=='string')throw Error('Invalid quest guide article');
  const title=text(row.menuKey),content=text(row.contentKey);
  // Retail category article symbols are absent; native lookup produces empty
  // content. Every quest child must have the published authored text.
  if(title===undefined||row.depth===1&&content===undefined)throw Error('Missing quest article text: '+row.id);
  articles.push({id:row.id,parent:row.parentOrCount,depth:row.depth,title,contentKey:row.contentKey,tokens:guideTokens(content??'')});
 }
 if(new Set(articles.map(a=>a.id)).size!==articles.length)throw Error('Duplicate quest guide article');
 const parents=new Set(articles.filter(a=>a.depth===0).map(a=>a.id));
 for(const row of articles)if(row.depth===1&&!parents.has(row.parent))throw Error('Orphan quest guide article');
 return {records:records.sort((a,b)=>a.id-b.id),articles};
}

// 7E6190 -> 669CF0. Indexed joins replace nested native list walks while
// preserving first-match order and all level/prerequisite/completion branches.
export function questGuideRows(catalog:QuestGuideCatalog,level:number,active:readonly number[],completed:readonly number[]):readonly QuestGuideRow[]{
 const specialTrades=new Set(['QNO_TRADE_CH_SPECIAL2_1','QNO_TRADE_WC_SPECIAL2_1','QNO_TRADE_TK_SPECIAL_1','QNO_TRADE_RM_SPECIAL_1','QNO_TRADE_AM_SPECIAL_1']);
 const white=[1,1,1,1] as const;
  if(!Number.isInteger(level)||level<1||level>255)throw Error('Invalid quest guide player level');
 const maximum=(level+10)&255,byId=new Map(catalog.records.map(r=>[r.id,r])),selected=new Map<number,QuestRecord>(),done=new Set(completed);
 for(const r of catalog.records)if(r.level<=maximum&&(!(r.symbol.startsWith('QSP')||specialTrades.has(r.symbol))||r.level<=level))selected.set(r.id,r);
 for(const key of [...active].sort((a,b)=>a-b)){const r=byId.get(key);if(r&&!selected.has(key))selected.set(key,r);}
 const content=new Map<string,QuestRecord>();
 for(const r of selected.values()){
  const admitted=r.symbol==='QSP_KT_EXINVENTORY_3'&&r.prerequisites.length?r.prerequisites.some(p=>done.has(p)):r.prerequisites.every(p=>done.has(p));
  if(admitted&&!content.has(r.contentKey))content.set(r.contentKey,r);
 }
 const rows:QuestGuideRow[]=[];
 for(const article of catalog.articles){
  if(article.depth===0){rows.push({...article,color:white});continue;}
  const r=content.get(article.contentKey);if(!r)continue;
  const color:UiQuad['color']=done.has(r.id)?[153/255,153/255,153/255,1]:r.level===level||r.level===0&&level===1?[1,217/255,83/255,1]:r.level>level?[253/255,59/255,59/255,1]:white;
  rows.push({...article,color});
 }
 return rows;
}
