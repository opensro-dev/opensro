"""
===========================================================================

native-client-ground-reference.py - original client navigation tick arithmetic

Executes complete 86D6D0 with real yaw binding 86C960 and distance 878CE0.
Only world lookup, region delta and collision service are synthetic.
This certifies stored-vector stepping and arrival, not world traversal.

Usage: python SCRIPT SRO_Client.exe OUTPUT_JSON

===========================================================================
"""
import hashlib
import json
import struct
import sys
from pathlib import Path

import pefile
from unicorn import Uc, UC_ARCH_X86, UC_MODE_32, UC_HOOK_CODE
from unicorn.x86_const import (
	UC_X86_REG_EAX, UC_X86_REG_ECX,
	UC_X86_REG_EIP, UC_X86_REG_ESP, UC_X86_REG_FPCW,
)

BINARY_SHA256 = "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a"
SCRATCH = 0x20000000
STACK = SCRATCH + 0x8000
STOP = SCRATCH + 0xF000
MOVER = SCRATCH + 0x1000
ACTOR = SCRATCH + 0x2000
INSTRUCTION_BUDGET = 10000
FPU_CONTROL_WORD = 0x027F


# ================
# f32
# ================
def f32(value):
	return struct.unpack("<f", struct.pack("<f", value))[0]


# ================
# words
# ================
def words(machine, address, *values):
	machine.mem_write(address, struct.pack("<" + "I" * len(values), *values))


# ================
# floats
# ================
def floats(machine, address, *values):
	machine.mem_write(address, struct.pack("<" + "f" * len(values), *values))


# ================
# run_interval
# A budget cutoff is a failure, never a truncated successful oracle.
# ================
def run_interval(machine, start, stop):
	machine.emu_start(start, stop, count=INSTRUCTION_BUDGET)
	if machine.reg_read(UC_X86_REG_EIP) != stop:
		raise RuntimeError("Native movement interval exceeded its instruction budget")


# ================
# generate
# ================
def generate(executable):
	raw = Path(executable).read_bytes()
	if hashlib.sha256(raw).hexdigest() != BINARY_SHA256:
		raise ValueError("Unexpected client executable")
	pe = pefile.PE(data=raw)
	image = pe.get_memory_mapped_image()
	base = pe.OPTIONAL_HEADER.ImageBase
	world, table, sector, collision = SCRATCH, SCRATCH + 0x100, SCRATCH + 0x300, SCRATCH + 0x310
	rows = []
	inputs = []
	for elapsed in [0, .0001, .016, .033, .15, .3, 1, 4, 20]:
		for yaw in [0, 1.5707963267948966, 2.345]:
			inputs.append(dict(name=f"elapsed-{elapsed}-yaw-{yaw}", elapsed=elapsed, yaw=yaw))
	for distance in [0, .5, 1, 4.9999995, 5, 5.0000005, 160, 200]:
		inputs.append(dict(
			name=f"arrival-{distance}",
			elapsed=.1,
			yaw=1.5707963267948966,
			goal=[100 + distance, 99, 100],
		))
	for status in [1, 0x10000000]:
		inputs.append(dict(name=f"collision-{status}", elapsed=.1, status=status))
	inputs.extend([
		dict(name="direction-stall", elapsed=20, waypoint=False),
		dict(name="scaled-channel", elapsed=.033, scale=.33333334),
		dict(name="override-channel", elapsed=.033, override=77.25),
		dict(name="negative-speed", elapsed=.033, speed=-50),
		dict(name="inactive", elapsed=1, active=False),
	])
	for row in inputs:
		machine = Uc(UC_ARCH_X86, UC_MODE_32)
		machine.mem_map(base, (len(image) + 4095) & ~4095)
		machine.mem_write(base, image)
		machine.mem_map(SCRATCH, 0x10000)
		machine.reg_write(UC_X86_REG_FPCW, FPU_CONTROL_WORD)
		status = row.get("status", 0)
		captured = []

		# ================
		# dependency
		# Real arithmetic executes; geometry is an injected service boundary.
		# ================
		def dependency(uc, address, size, data):
			sp = uc.reg_read(UC_X86_REG_ESP)

			# ================
			# u32
			# ================
			def u32(at):
				return struct.unpack("<I", uc.mem_read(at, 4))[0]

			cleanup = None
			if address == 0x879CD0:
				uc.reg_write(UC_X86_REG_EAX, world)
				cleanup = 0
			elif address == sector:
				a, b, out = u32(sp + 4), u32(sp + 8), u32(sp + 12)
				floats(uc, out, ((a & 255) - (b & 255)) * 1920, 0, ((a >> 8) - (b >> 8)) * 1920)
				cleanup = 12
			elif address == collision:
				candidate = u32(sp + 12)
				captured.append(list(struct.unpack("<3f", uc.mem_read(candidate + 12, 12))))
				words(uc, candidate, 1, 0)
				uc.reg_write(UC_X86_REG_EAX, status)
				cleanup = 20
			if cleanup is not None:
				uc.reg_write(UC_X86_REG_EIP, u32(sp))
				uc.reg_write(UC_X86_REG_ESP, sp + 4 + cleanup)

		machine.hook_add(UC_HOOK_CODE, dependency)
		words(machine, world, table)
		words(machine, table + 0x1C, sector)
		words(machine, table + 0x30, collision)
		words(machine, 0xF17124, world)
		words(machine, MOVER, ACTOR)
		speed = f32(row.get("speed", 50))
		scale = f32(row.get("scale", 1))
		override = f32(row.get("override", 0))
		elapsed = f32(row["elapsed"])
		yaw = f32(row.get("yaw", 1.5707963267948966))
		start = list(map(f32, [100, 3, 100]))
		goal = list(map(f32, row.get("goal", [1000, 99, 1000])))
		floats(machine, MOVER + 4, speed)
		floats(machine, MOVER + 0x8C, scale)
		floats(machine, ACTOR + 0x438, override)
		floats(machine, 0xF0C914, elapsed)
		words(machine, MOVER + 0x84, 1 if row.get("active", True) else 0)
		machine.mem_write(MOVER + 0x80, bytes([1 if row.get("waypoint", True) else 0]))
		words(machine, MOVER + 0x4C, 1, 0, 257)
		floats(machine, MOVER + 0x58, *start)
		words(machine, MOVER + 0x30, 1, 0, 257)
		floats(machine, MOVER + 0x3C, *goal)
		words(machine, STACK, STOP)
		floats(machine, STACK + 4, yaw)
		machine.reg_write(UC_X86_REG_ESP, STACK)
		machine.reg_write(UC_X86_REG_ECX, MOVER)
		run_interval(machine, 0x86C960, STOP)
		direction = list(struct.unpack("<3f", machine.mem_read(MOVER + 0x74, 12)))
		words(machine, STACK, STOP)
		machine.reg_write(UC_X86_REG_ESP, STACK)
		machine.reg_write(UC_X86_REG_ECX, MOVER)
		run_interval(machine, 0x86D6D0, STOP)
		result = machine.mem_read(MOVER + 0x58, 12)
		rows.append(dict(
			name=row["name"],
			speed=speed,
			scale=scale,
			overrideSpeed=override,
			elapsedSeconds=elapsed,
			yaw=yaw,
			start=start,
			goal=goal,
			direction=direction,
			waypoint=row.get("waypoint", True),
			initiallyActive=row.get("active", True),
			status=status,
			position=list(struct.unpack("<3f", result)),
			positionBits=list(struct.unpack("<3I", result)),
			active=bool(struct.unpack("<I", machine.mem_read(MOVER + 0x84, 4))[0]),
			accepted=bool(machine.reg_read(UC_X86_REG_EAX)),
			candidates=captured,
		))

	directions = []
	for delta in [[1, 0], [0, 1], [-1, 0], [0, -1], [3, 4], [-3, 4], [.0000001, 0], [123.456, -789.123], [0, 0]]:
		vector = SCRATCH + 0x4000
		angle = SCRATCH + 0x4100
		floats(machine, vector, delta[0], 0, delta[1])
		floats(machine, angle, 0)
		words(machine, STACK, STOP)
		machine.reg_write(UC_X86_REG_ESP, STACK)
		machine.reg_write(UC_X86_REG_ECX, vector)
		run_interval(machine, 0x410890, STOP)
		words(machine, STACK, STOP, vector, angle)
		machine.reg_write(UC_X86_REG_ESP, STACK)
		run_interval(machine, 0x8791A0, STOP)
		yaw = struct.unpack("<f", machine.mem_read(angle, 4))[0]
		words(machine, STACK, STOP)
		floats(machine, STACK + 4, yaw)
		machine.reg_write(UC_X86_REG_ESP, STACK)
		machine.reg_write(UC_X86_REG_ECX, MOVER)
		run_interval(machine, 0x86C960, STOP)
		data = machine.mem_read(MOVER + 0x74, 12)
		directions.append(dict(
			delta=list(map(f32, delta)),
			yaw=yaw,
			direction=list(struct.unpack("<3f", data)),
			directionBits=list(struct.unpack("<3I", data)),
		))
	return dict(
		format="sro-native-client-ground-v1",
		binarySha256=BINARY_SHA256,
		floatingControlWord="0x027f",
		intervals=[
			"86C960 complete (8788C0 yaw vector)",
			"86D6D0 complete (86D480 steering, 86CA70 no-turn branch, 878CE0 remaining distance)",
		],
		dependencies=(
			"World singleton, sector offsets and collision result only; native CRT executes. "
			"No steering input flags. No world traversal equivalence claim."
		),
		cases=rows,
		directions=directions,
	)


if __name__ == "__main__":
	Path(sys.argv[2]).write_text(
		json.dumps(generate(sys.argv[1]), indent=2) + "\n",
		encoding="utf-8",
		newline="\n",
	)
