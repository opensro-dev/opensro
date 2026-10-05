"""
===========================================================================
capture_trade_quotation.py - execute original trader price instructions

Maps the pinned server PE into Unicorn and executes 4C8810, including its
original CRT conversion helper. The thief leg executes 4C8D65..4C8D89 with
the real 4C81D0 and 482490 helpers. No tested instruction is replaced.
Requires pefile and unicorn only when regenerating the frozen fixture.
===========================================================================
"""

import argparse
import hashlib
import json
import pathlib
import random
import struct

import pefile
from unicorn import Uc, UC_ARCH_X86, UC_MODE_32
from unicorn.x86_const import (
	UC_X86_REG_EBP, UC_X86_REG_ECX, UC_X86_REG_EDI, UC_X86_REG_EIP,
	UC_X86_REG_ESI, UC_X86_REG_ESP, UC_X86_REG_FPCW,
)

SERVER_SHA256 = "bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290"
ENTRY = 0x2000000
RECORD = ENTRY + 0x100
ITEM = ENTRY + 0x300
VTABLE = ENTRY + 0x400
ITEM_RECORD = ENTRY + 0x900
REFERENCE = ENTRY + 0xA00
PROFIT = ENTRY + 0xC00
STACK = 0x3008000
RETURN = 0x4000000
PAGE_SIZE = 4096
INSTRUCTION_LIMIT = 10000


# ================
# float32
# ================
def float32(value):
	return struct.unpack("<f", struct.pack("<f", value))[0]


# ================
# NativeQuotation
# ================
class NativeQuotation:
	# ================
	# __init__
	# ================
	def __init__(self, path):
		if hashlib.sha256(path.read_bytes()).hexdigest() != SERVER_SHA256:
			raise ValueError("Unexpected server executable digest")
		pe = pefile.PE(str(path))
		image = pe.get_memory_mapped_image()
		self.cpu = Uc(UC_ARCH_X86, UC_MODE_32)
		self.cpu.mem_map(pe.OPTIONAL_HEADER.ImageBase, (len(image) + PAGE_SIZE - 1) & -PAGE_SIZE)
		self.cpu.mem_write(pe.OPTIONAL_HEADER.ImageBase, image)
		pe.close()
		self.cpu.mem_map(ENTRY, 0x10000)
		self.cpu.mem_map(STACK - 0x8000, 0x10000)
		self.cpu.mem_map(RETURN, PAGE_SIZE)
		self.cpu.reg_write(UC_X86_REG_FPCW, 0x27F)
		self.initial = self.cpu.context_save()

	# ================
	# run
	# ================
	def run(self, start, end):
		self.cpu.emu_start(start, end, count=INSTRUCTION_LIMIT)
		if self.cpu.reg_read(UC_X86_REG_EIP) != end:
			raise RuntimeError("Native quotation did not reach its return boundary")

	# ================
	# price
	# ================
	def price(self, base, row):
		self.cpu.context_restore(self.initial)
		self.cpu.mem_write(ENTRY, struct.pack("<5I", RECORD, base, 0, 0, 0))
		self.cpu.mem_write(RECORD + 0x28, struct.pack("<3f3i", *row))
		self.cpu.mem_write(STACK, struct.pack("<I", RETURN))
		self.cpu.reg_write(UC_X86_REG_ESP, STACK)
		self.cpu.reg_write(UC_X86_REG_ESI, ENTRY)
		self.run(0x4C8810, RETURN)
		return struct.unpack("<I", self.cpu.mem_read(ENTRY + 8, 4))[0]

	# ================
	# thief
	# ================
	def thief(self, base, quantity):
		self.cpu.context_restore(self.initial)
		self.cpu.mem_write(ITEM, struct.pack("<I", VTABLE))
		self.cpu.mem_write(VTABLE + 0x3A8, struct.pack("<I", 0x482490))
		self.cpu.mem_write(ITEM + 0x34, struct.pack("<I", ITEM_RECORD))
		self.cpu.mem_write(ITEM_RECORD + 0x18, struct.pack("<I", REFERENCE))
		self.cpu.mem_write(REFERENCE + 0x9C, struct.pack("<I", base))
		self.cpu.mem_write(STACK + 0x2C, struct.pack("<I", quantity))
		self.cpu.reg_write(UC_X86_REG_ESP, STACK)
		self.cpu.reg_write(UC_X86_REG_ECX, ITEM)
		self.cpu.reg_write(UC_X86_REG_EBP, PROFIT)
		self.run(0x4C8D65, 0x4C8D89)
		low = self.cpu.reg_read(UC_X86_REG_EDI)
		high = struct.unpack("<I", self.cpu.mem_read(STACK + 0x1C, 4))[0]
		credit = struct.unpack("<q", struct.pack("<II", low, high))[0]
		profit = struct.unpack("<q", self.cpu.mem_read(PROFIT, 8))[0]
		return credit, profit


# ================
# quotation_inputs
# ================
def quotation_inputs():
	for rate in (0.9, 1.09, 1.1, 1.2, 1.999, 2.75):
		for stock in (0, 49999, 50000, 50001, 100000):
			yield [rate, rate, rate, 50000, 250, stock]
	for step in (1, 3, 250):
		for stock in (0, 49999, 50000, 50001, 50003, 100000, 0x7FFFFFFF):
			yield [1.2, 0.25, 2.75, 50000, step, stock]
	rng = random.Random(6194)
	for _ in range(48):
		yield [rng.uniform(0.8, 1.4), 0.25, 2.75, 50000, rng.randint(1, 500), rng.randint(0, 100000)]


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("server", type=pathlib.Path)
	parser.add_argument("output", type=pathlib.Path)
	args = parser.parse_args()
	native = NativeQuotation(args.server)
	quotations = []
	fields = ("base", "lower", "upper", "baseStock", "step", "stock")
	for row in quotation_inputs():
		row[:3] = [float32(value) for value in row[:3]]
		for base in (1, 3, 99, 100, 10001, 0xFFFFFF, 0x1000001, 0x7FFFFFFF, 0xFFFFFFFF):
			quotations.append({"basePrice": base, **dict(zip(fields, row)), "price": native.price(base, row)})
	thieves = []
	for base in (0, 1, 3, 99, 10001, 0x1000001, 0x7FFFFFFF, 0x80000000, 0xFFFFFFFF):
		for quantity in (0, 1, 2, 3, 4, 99, 1000, 0xFFFF):
			credit, profit = native.thief(base, quantity)
			thieves.append({"basePrice": base, "quantity": quantity, "credit": credit, "profit": profit})
	document = {
		"generator": "scripts/tools/capture_trade_quotation.py",
		"sourceSHA256": SERVER_SHA256,
		"precision": 53,
		"quotations": quotations,
		"thieves": thieves,
	}
	args.output.write_text(json.dumps(document, indent="\t") + "\n", encoding="utf-8", newline="\n")
	print(f"Captured {len(quotations)} quotations and {len(thieves)} thief values")


if __name__ == "__main__":
	main()
