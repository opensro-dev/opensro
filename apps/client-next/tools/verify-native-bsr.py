"""
===========================================================================

verify-native-bsr.py - Verify original BSR machine evidence and the bounded EFP admission branches

Unicorn executes original B1F270/AFF730 bytes. Allocator, archive I/O and parser
are explicit boundary stubs; this is not whole-client equivalence.

SRO_Client.exe is read from the game root (scripts/sro_paths.py).

===========================================================================
"""
import hashlib, json, struct
from pathlib import Path
import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[3] / 'scripts'))
from sro_paths import GAME_ROOT  # noqa: E402  (SRO_GAME_ROOT or beside the main checkout)
import pefile
from unicorn import Uc, UC_ARCH_X86, UC_MODE_32, UC_HOOK_CODE
from unicorn.x86_const import UC_X86_REG_ESP, UC_X86_REG_EAX, UC_X86_REG_ECX, UC_X86_REG_EIP

ROOT=Path(__file__).resolve().parents[1]
raw=(GAME_ROOT/'SRO_Client.exe').read_bytes()
evidence=json.loads((ROOT/'tests/fixtures/native/bsr-native.json').read_text())
assert hashlib.sha256(raw).hexdigest()==evidence['binarySha256']
pe=pefile.PE(data=raw);base=pe.OPTIONAL_HEADER.ImageBase
verified_blocks=0
for filename in ['bsr-native.json','bsr-activation-native.json','bsr-visibility-native.json','bsr-query-native.json','bsr-material-clock-native.json','bsr-modifier-delta-native.json','bsr-video-native.json','bsr-route-native.json','bsr-animation-native.json','bsr-night-native.json','bsr-firework-lod-native.json','bsr-pose-lod-native.json','bsr-blended-modifiers-native.json']:
 contract=json.loads((ROOT/'tests/fixtures/native'/filename).read_text())
 assert contract['binarySha256']==evidence['binarySha256'], filename
 for fn in contract['functions']:
  for block in fn['blocks']:
   assert pe.get_data(block['start']-base,block['end']-block['start']).hex()==block['bytes'], (filename,hex(fn['va']))
   verified_blocks+=1
 for v in contract['vtables']:
  assert v['slotOffset']==v['slotIndex']*4 and v['entryVa']==v['tableVa']+v['slotOffset']
  assert struct.unpack('<I',pe.get_data(v['entryVa']-base,4))[0]==v['targetVa']
candidate=hashlib.sha256((ROOT/'src/engine/runtime/assets/worker/effects/program/program.ts').read_bytes()).hexdigest()
results=[]
for kind in ['absent','wrong-header','valid-header','cache-retains-failed-load']:
 m=Uc(UC_ARCH_X86,UC_MODE_32);size=(pe.OPTIONAL_HEADER.SizeOfImage+4095)&~4095
 m.mem_map(base,size);m.mem_write(base,pe.get_memory_mapped_image());m.mem_map(0,4096)
 arena=0x10000000;m.mem_map(arena,0x40000);sp=arena+0x30000;stop=arena+0x3f000
 obj=arena+0x1000;name=arena+0x2000;vt=arena+0x3000;io=arena+0x4000;out=arena+0x5000
 def write(at,n):m.mem_write(at,struct.pack('<I',n))
 def read(at):return struct.unpack('<I',m.mem_read(at,4))[0]
 def ret(n=0,value=None):
  stack=m.reg_read(UC_X86_REG_ESP);address=read(stack);m.reg_write(UC_X86_REG_ESP,stack+4+n)
  if value is not None:m.reg_write(UC_X86_REG_EAX,value)
  m.reg_write(UC_X86_REG_EIP,address)
 write(obj,vt);write(vt+8,arena+0x6000);write(io,vt+0x100);write(vt+0x128,arena+0x6010);write(0xf16fd0,io)
 write(name+0x18,15);m.mem_write(name+4,b'missing.efp\0');write(0xf16fd4,0)
 calls=[]
 def hook(machine,address,size,user):
  stack=m.reg_read(UC_X86_REG_ESP)
  if address==0x9c5180:
   target=read(stack);m.reg_write(UC_X86_REG_ESP,stack+4-m.reg_read(UC_X86_REG_EAX));m.reg_write(UC_X86_REG_EIP,target)
  elif address==0x9c366a:ret()
  elif address==arena+0x6000:calls.append('reset');ret()
  elif address==arena+0x6010:calls.append('open');ret(16)
  elif address==0x410b50:
   calls.append('read');target=read(stack+4)
   if kind=='valid-header':m.mem_write(target,b'JMXVEFF 1000')
   elif kind=='wrong-header':m.mem_write(target,b'JMXVBAD 1000')
   ret(8,0 if kind=='absent' else 12)
  elif address==0x40eaa0:ret()
  elif address==0xb29e30:calls.append('parse');ret(4,1)
  elif kind=='cache-retains-failed-load' and address==0xafdc50:calls.append('allocate');ret(value=obj)
  elif kind=='cache-retains-failed-load' and address==0xb1f270:calls.append('load-false');ret(4,0)
 m.hook_add(UC_HOOK_CODE,hook)
 if kind=='cache-retains-failed-load':
  m.mem_write(sp,struct.pack('<III',stop,name,out));entry=0xaff730;cleanup=12
 else:
  m.mem_write(sp,struct.pack('<II',stop,name));entry=0xb1f270;cleanup=8;m.reg_write(UC_X86_REG_ECX,obj)
 m.reg_write(UC_X86_REG_ESP,sp);m.emu_start(entry,stop,count=5000)
 assert m.reg_read(UC_X86_REG_EIP)==stop and m.reg_read(UC_X86_REG_ESP)==sp+cleanup
 success=m.reg_read(UC_X86_REG_EAX)&255
 assert success==(1 if kind in ['valid-header','cache-retains-failed-load'] else 0)
 if kind=='cache-retains-failed-load':assert read(out)==obj and calls==['allocate','load-false']
 else:assert calls==['reset','open','read']+(['parse'] if kind=='valid-header' else [])
 results.append({'case':kind,'return':success,'calls':calls})
out=ROOT/'temp/artifacts/bsr-parity/native-validation.json';out.parent.mkdir(parents=True,exist_ok=True)
out.write_text(json.dumps({'candidateSha256':candidate,'binarySha256':evidence['binarySha256'],'verifiedBlocks':verified_blocks,'cases':results},indent=2)+'\n')
print(out)
