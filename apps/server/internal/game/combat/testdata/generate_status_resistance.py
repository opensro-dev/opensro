"""
===========================================================================
generate_status_resistance.py - original x86 resistance aggregate oracle

Run with --binary SR_GameServer.exe --output native-status-resistance.json.
Requires pefile and unicorn. Builds native container layouts, then executes
5999E0 and its original lookup/iterator helpers without function stubs.
This verifies lookup semantics, not insertion or deletion of the trees.
===========================================================================
"""
import argparse
import hashlib
import itertools
import json
import struct
from pathlib import Path

import pefile
from unicorn import Uc, UC_ARCH_X86, UC_MODE_32
from unicorn.x86_const import UC_X86_REG_EAX, UC_X86_REG_ESP, UC_X86_REG_EIP

BINARY_SHA256 = "bec2375e2c4c1073e3bf7761571470c430de251de74b452dbb86537348ef5290"
ENTRY = 0x5999E0
MEMORY = 0x2000000
HEAP = MEMORY + 0x1000
STACK = MEMORY + 0x30000
STOP = MEMORY + 0x3F000
MAX_INSTRUCTIONS = 10000
EDGE_WORDS = (0, 1, 48, 100, 0x7FFFFFFF, 0x80000000, 0xFFFFFFFF)

# ================
# NativeResistanceOracle
# ================
class NativeResistanceOracle:
	# ================
	# __init__
	# ================
	def __init__(self, binary):
		assert hashlib.sha256(binary.read_bytes()).hexdigest() == BINARY_SHA256
		pe = pefile.PE(str(binary))
		self.machine = Uc(UC_ARCH_X86, UC_MODE_32)
		base = pe.OPTIONAL_HEADER.ImageBase
		self.machine.mem_map(base, (pe.OPTIONAL_HEADER.SizeOfImage + 4095) & ~4095)
		self.machine.mem_write(base, pe.get_memory_mapped_image())
		self.machine.mem_map(MEMORY, 0x40000)
		self.allocated = HEAP

	# ================
	# words
	# ================
	def words(self, address, *values):
		self.machine.mem_write(address, struct.pack("<" + "I" * len(values), *values))

	# ================
	# allocate
	# ================
	def allocate(self):
		address = self.allocated
		self.allocated += 32
		assert self.allocated < STACK
		return address

	# ================
	# tree
	# Sentinel/node offsets come from 599840, 5A5010 and 5A93B0.
	# ================
	def tree(self, owner, entries):
		header = self.allocate()
		nodes = []
		for key, value in sorted(entries):
			node = self.allocate()
			nodes.append(node)
			self.words(node, header, header, header, key, value)
			self.machine.mem_write(node + 21, b"\x00")
		self.words(owner, 0, header, len(nodes))
		self.words(header, nodes[0] if nodes else header, nodes[0] if nodes else header, nodes[-1] if nodes else header)
		self.machine.mem_write(header + 21, b"\x01")
		for index, node in enumerate(nodes):
			self.words(node + 4, header if index == 0 else nodes[index - 1], nodes[index + 1] if index + 1 < len(nodes) else header)

	# ================
	# run
	# Construct two nested ordered maps; no allocation or lookup is stubbed.
	# ================
	def run(self, rows):
		self.allocated = HEAP
		groups = {}
		for grade, flat in rows:
			groups.setdefault(grade, []).append(flat)
		outer = []
		for grade, flats in groups.items():
			value = self.allocate()
			self.tree(value + 8, [(flat, index + 1) for index, flat in enumerate(flats)])
			outer.append((grade, value))
		self.tree(MEMORY, outer)
		self.tree(MEMORY + 12, [])
		output = MEMORY + 0x100
		self.words(STACK, STOP, output, output + 4, output + 8)
		self.machine.reg_write(UC_X86_REG_EAX, MEMORY)
		self.machine.reg_write(UC_X86_REG_ESP, STACK)
		self.machine.emu_start(ENTRY, STOP, count=MAX_INSTRUCTIONS)
		assert self.machine.reg_read(UC_X86_REG_EIP) == STOP, "native call did not return"
		return struct.unpack("<III", self.machine.mem_read(output, 12))

# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("--binary", type=Path, required=True)
	parser.add_argument("--output", type=Path, required=True)
	args = parser.parse_args()
	oracle = NativeResistanceOracle(args.binary)
	cases = []
	for grade1, grade2, flat1, flat2 in itertools.product(EDGE_WORDS, repeat=4):
		rows = [[grade1, flat1], [grade2, flat2]]
		percent, grade, flat = oracle.run(rows)
		cases.append({"entries": rows, "percent": percent, "grade": grade, "flat": flat})
	fixture = {"binarySha256": BINARY_SHA256, "entry": "0x5999e0", "cases": cases}
	args.output.write_bytes((json.dumps(fixture, separators=(",", ":")) + "\n").encode())
	print(f"Executed {len(cases)} original calls through 0x5999E0")

if __name__ == "__main__":
	main()
