# ===========================================================================
# capture_damage_to_mp.py - reproduce the native dgmp arithmetic corpus
# Executes original GameServer bytes and CRT_ftol. Only virtual MP accessors
# are stubs; an instruction-limit exit is a failure, never a successful case.
# ===========================================================================
import argparse
import hashlib
import json
import struct
from pathlib import Path

import pefile
from unicorn import Uc, UC_ARCH_X86, UC_MODE_32, UC_HOOK_CODE
from unicorn.x86_const import UC_X86_REG_EAX, UC_X86_REG_EBX, UC_X86_REG_EIP
from unicorn.x86_const import UC_X86_REG_ESP, UC_X86_REG_FPCW

IMAGE_BASE = 0x400000
HEAP_BASE = 0x2000000
HEAP_BYTES = 0x20000
STACK_BASE = 0x3000000
STACK_BYTES = 0x10000
STACK_TOP = STACK_BASE + 0x8000
PAGE_BYTES = 4096
DGMP_START = 0x5A13FE
DGMP_END = 0x5A14D8
FPU_CONTROL = 0x027F
GET_MP = HEAP_BASE + 0x10000
DEBIT_MP = HEAP_BASE + 0x10010
MANAGER = HEAP_BASE
EFFECT = HEAP_BASE + 0x1000
CONTEXT = HEAP_BASE + 0x2000
REFERENCE = HEAP_BASE + 0x3000
PARAMETER = HEAP_BASE + 0x4000
ACTOR = HEAP_BASE + 0x5000
VTABLE = HEAP_BASE + 0x6000
HIT = HEAP_BASE + 0x7000
INSTRUCTION_LIMIT = 1000

# ================================
# write_word
# ================================
def write_word(emulator, address, value):
	emulator.mem_write(address, struct.pack("<I", value))

# ================================
# read_word
# ================================
def read_word(emulator, address):
	return struct.unpack("<I", emulator.mem_read(address, 4))[0]

# ================================
# virtual_mp
# Model the actor's current MP and actual debit; preserve thiscall cleanup.
# ================================
def virtual_mp(emulator, address, size, state):
	if address not in (GET_MP, DEBIT_MP):
		return
	stack = emulator.reg_read(UC_X86_REG_ESP)
	if address == GET_MP:
		emulator.reg_write(UC_X86_REG_EAX, state["mp"])
		cleanup = 4
	else:
		assert read_word(emulator, stack + 4) == 0
		assert read_word(emulator, stack + 12) == 4
		state["spent"] = read_word(emulator, stack + 8)
		assert state["spent"] <= state["mp"]
		state["mp"] -= state["spent"]
		cleanup = 16
	emulator.reg_write(UC_X86_REG_EIP, read_word(emulator, stack))
	emulator.reg_write(UC_X86_REG_ESP, stack + cleanup)

# ================================
# create_emulator
# Build the native instance/context/descriptor chain read by the dgmp branch.
# ================================
def create_emulator(binary):
	pe = pefile.PE(str(binary))
	assert pe.OPTIONAL_HEADER.ImageBase == IMAGE_BASE
	image = pe.get_memory_mapped_image()
	emulator = Uc(UC_ARCH_X86, UC_MODE_32)
	emulator.mem_map(IMAGE_BASE, (len(image) + PAGE_BYTES - 1) & ~(PAGE_BYTES - 1))
	emulator.mem_write(IMAGE_BASE, image)
	emulator.mem_map(HEAP_BASE, HEAP_BYTES)
	emulator.mem_map(STACK_BASE, STACK_BYTES)
	for address, value in (
		(MANAGER, ACTOR), (MANAGER + 0x1FC, EFFECT), (EFFECT + 0x18, CONTEXT),
		(CONTEXT + 8, REFERENCE), (REFERENCE + 0x4CC, PARAMETER), (ACTOR, VTABLE),
		(VTABLE + 0x10C, GET_MP), (VTABLE + 0x308, DEBIT_MP)
	):
		write_word(emulator, address, value)
	state = {}
	emulator.hook_add(UC_HOOK_CODE, virtual_mp, state)
	return emulator, state

# ================================
# capture
# Authored ranks plus boundary percentages, small rounding cases and large hits.
# ================================
def capture(binary):
	emulator, state = create_emulator(binary)
	percentages = [0, 20, 21, 22, 23, 24, 25, 30, 31, 32, 33, 34, 35,
		40, 41, 42, 43, 44, 45, 50, 51, 52, 53, 100]
	damages = [0, 1, 2, 3, 5, 9, 10, 19, 20, 99, 100, 101, 999, 1000, 1001, 1000000]
	rows = []
	for percent in percentages:
		for damage in damages:
			for mp in [0, 1, 2, 10, 100, 1000000]:
				state.update(mp=mp, spent=0)
				write_word(emulator, PARAMETER, percent)
				write_word(emulator, HIT + 0x20, damage)
				write_word(emulator, STACK_TOP + 0x70, HIT)
				emulator.reg_write(UC_X86_REG_ESP, STACK_TOP)
				emulator.reg_write(UC_X86_REG_EBX, MANAGER)
				emulator.reg_write(UC_X86_REG_FPCW, FPU_CONTROL)
				emulator.emu_start(DGMP_START, DGMP_END, count=INSTRUCTION_LIMIT)
				assert emulator.reg_read(UC_X86_REG_EIP) == DGMP_END
				rows.append({"damage": damage, "percent": percent, "mp": mp,
					"hpDamage": read_word(emulator, HIT + 0x20), "mpDamage": state["spent"]})
	return {"native": "SR_GameServer 5A13FE..5A14D8 including CRT_ftol; only virtual MP accessors stubbed",
		"fpuControl": FPU_CONTROL, "binarySha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
		"cases": rows}

# ================================
# main
# The licensed executable is supplied explicitly and is never published.
# ================================
def main():
	parser = argparse.ArgumentParser()
	parser.add_argument("--binary", type=Path, required=True)
	parser.add_argument("--output", type=Path, required=True)
	args = parser.parse_args()
	corpus = capture(args.binary)
	args.output.write_text(json.dumps(corpus, separators=(",", ":")) + "\n", encoding="utf-8")
	print(f"Captured {len(corpus['cases'])} original-machine cases")

if __name__ == "__main__":
	main()
