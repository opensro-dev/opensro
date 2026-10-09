from pathlib import Path
import json,hashlib,sys
# The published tree comes from scripts/sro_paths.py (SRO_GENERATED_ROOT aware).
sys.path.insert(0,str(Path(__file__).resolve().parents[3]/'scripts'))
from sro_paths import PUBLIC_ROOT
base=PUBLIC_ROOT/'assets'/'data'
source=base/'skillData.json';mastery=base/'skillMasteryData.json'
a=json.loads(source.read_text(encoding='utf-8'));b=json.loads(mastery.read_text(encoding='utf-8'))
chain_targets=set()
for line in a['rows']:
 c=line.split('\t')
 if len(c)>=21 and c[0].isdigit():
  nxt=int(c[20]) if c[20].isdigit() else 0
  if nxt!=0:chain_targets.add(nxt)
changed=True
while changed:
 changed=False
 for line in a['rows']:
  c=line.split('\t')
  if len(c)>=21 and c[0].isdigit():
   sid=int(c[0])
   if sid in chain_targets:
    nxt=int(c[20]) if c[20].isdigit() else 0
    if nxt!=0 and nxt not in chain_targets:
     chain_targets.add(nxt);changed=True
masters=[];groups=[];skills=[]
for line in b['masteryRows']:
 c=line.split('\t')
 if len(c)>=13 and c[0].isdigit() and c[6].isdigit() and int(c[6])!=255:
  masters.append({'id':int(c[0]),'name':c[2],'count':int(c[3]),'tab':int(c[6]),'tabName':c[5],'icon':c[11]})
for line in b['groupRows']:
 c=line.split('\t')
 if len(c)>=7 and c[2].isdigit() and c[4].isdigit():groups.append({'mastery':int(c[2]),'row':int(c[4]),'name':c[5],'icon':'icon/'+c[6]})
for line in a['rows']:
 c=line.split('\t')
 if len(c)>=20 and c[0].isdigit() and int(c[16])<255 and int(c[17])<8:
  sid=int(c[0])
  if sid not in chain_targets:
   skills.append({'id':sid,'group':int(c[1]),'level':int(c[2]),'mastery':int(c[4]),'row':int(c[16]),'column':int(c[17]),'icon':'icon/'+c[18] if c[18].endswith('.ddj') else '', 'name':c[19], 'study':c[28] if len(c)>28 and c[28]!='xxx' else ''})
out={'version':1,'sources':{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in [source,mastery]},'masteries':masters,'groups':groups,'skills':skills}
p=base/'skillUi.json';text=json.dumps(out,separators=(',',':'))+'\n'
# An unchanged projection keeps its mtime, so the build's stat fingerprints and sidecar checks stay fresh.
if not p.exists() or p.read_text(encoding='utf-8')!=text: p.write_text(text,encoding='utf-8')
print(len(masters),len(groups),len(skills),p.stat().st_size)
