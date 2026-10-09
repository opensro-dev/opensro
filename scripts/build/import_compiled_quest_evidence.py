"""
===========================================================================

import_compiled_quest_evidence.py - decode the v1.188 compiled quest classes

Ninety-one v1.150 quests are hand-written C++ classes in SR_GameServer
(v1.188), not Lua scripts. Each class's initializer (vtable +0xDC) fills
the CBasicQuest object and allocates its mission objects. This importer
reads a Binary Ninja HLIL dump of those initializers and writes the
normalized field writes to scripts/data/quest/compiled-quests-source.json;
generate_compiled_quests.py compiles that snapshot. Neither the build nor
the server reads the dump or the binary.

The dumps are JSON objects: --hlil maps each codename to {"init": <HLIL
text>}, and --methods each "CODE@slot" to one overriding vtable method,
both produced from the registrations in research
investigations/quest/quest-implementation-batches.json.

Two HLIL shapes need care before the line parser sees them:
	- a mission loop (do ... while (i s< arg3[0x121])) whose body switches
	  on the index: it is expanded into one copy per index, each keeping
	  the shared lines and its own case;
	- an x87 temporary (x87_rN = fconvert.t(3f)) later stored with
	  fconvert.s(x87_rN): the store takes the temporary's literal.

===========================================================================
"""

import argparse
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "scripts/data/quest/compiled-quests-source.json"
FORMAT = "sro-compiled-quest-evidence-v1"

VALUE = r'(0x[0-9a-f]+|-?\d+(?:\.\d+)?(?:e[-+]?\d+)?f|-?\d+|"[^"]*")'
MISSION_COUNT_WORD = "0x121"


# ================
# literal
# ================
def literal(text):
	if text.startswith('"'):
		return text[1:-1]
	if text.startswith("0x"):
		return int(text, 16)
	if text.endswith("f"):
		return round(float(text[:-1]), 4)
	return int(text)


# ================
# indent_of
# ================
def indent_of(line):
	return len(line) - len(line.lstrip(" "))


# ================
# substitute_x87
#
# The x87 temporaries hold a float literal for the stores that follow;
# run after loop expansion, as each branch loads its own.
# ================
def substitute_x87(lines):
	temporaries = {}
	out = []
	for line in lines:
		m = re.match(r'\s*(?:long double )?(x87_r\d+(?:_\d+)?) = (?:fconvert|float)\.t\((-?[\d.e+-]+f?)\)\s*$', line)
		if m:
			value = m.group(2)
			temporaries[m.group(1)] = value if value.endswith("f") else value + "f"
			continue
		line = re.sub(r'fconvert\.s\((x87_r\d+(?:_\d+)?)\)', lambda g: temporaries.get(g.group(1), g.group(0)), line)
		out.append(line)
	return out


# ================
# case_blocks
#
# Split a loop body into the lines every iteration runs and, per index,
# the lines only that index runs: a switch's cases, or an if/else-if
# chain testing the index variable.
# ================
def case_blocks(body, index):
	shared, cases = [], {}
	current, case_indent = None, None
	for line in body:
		stripped = line.strip()
		m = re.match(r'case ([\d, ]+)$', stripped) or re.match(r'(?:else )?if \(' + re.escape(index) + r' == (\d+)\)$', stripped)
		if m:
			current = [int(k) for k in m.group(1).split(",")]
			case_indent = indent_of(line)
			for k in current:
				cases.setdefault(k, [])
			continue
		if current is not None and (stripped == "" or indent_of(line) > case_indent):
			for k in current:
				cases[k].append(line)
			continue
		if current is not None and stripped == "else":
			# The chain's fallthrough is the native's MiniDump guard.
			current = []
			continue
		current = None
		if re.match(r'(if \(' + re.escape(index) + r' u<= \d+\)|switch \(' + re.escape(index) + r'\))$', stripped):
			continue
		shared.append(line)
	return shared, cases


# ================
# expand_loops
#
# Replace each mission loop with one copy of its body per index. The
# index variable is the one the body stores at mission +8.
# ================
def expand_loops(lines, count):
	out, i = [], 0
	while i < len(lines):
		line = lines[i]
		if line.strip() == "do" and i + 1 < len(lines) and "CRT_operator_new" in lines[i + 1]:
			depth = indent_of(line)
			j = i + 1
			while j < len(lines) and not (indent_of(lines[j]) == depth and lines[j].strip().startswith("while")):
				j += 1
			body = lines[i + 1:j]
			index = "i"
			for row in body:
				m = re.match(r'\s*\*\(\w+ \+ 8\) = (\w+)\.b$', row)
				if m:
					index = m.group(1)
			shared, cases = case_blocks(body, index)
			iterations = count if count else (max(cases) + 1 if cases else 1)
			for k in range(iterations):
				for row in shared:
					out.append(re.sub(r'\b' + re.escape(index) + r'\.b\b', str(k), row))
				out.extend(cases.get(k, []))
			i = j + 1
			continue
		out.append(line)
		i += 1
	return out


# ================
# mission_count
# ================
def mission_count(lines):
	for line in lines:
		m = re.match(r'\s*arg\d\[' + MISSION_COUNT_WORD + r'\] = ' + VALUE + r'\s*$', line)
		if m:
			return literal(m.group(1))
	return 0


# ================
# parse_initializer
#
# The quest object's word writes, its string lists and every mission
# object's field writes, in allocation order.
# ================
def parse_initializer(text):
	lines = text.splitlines()
	lines = substitute_x87(expand_loops(lines, mission_count(lines)))
	quest = {"words": {}, "tables": {}, "lists": {}, "missions": []}
	last_string = {}
	mission, mission_vars = None, set()
	# A table's flag word is set through a pointer copy:
	# "int32_t* eax_8 = arg3[0xc2]" then "*eax_8 |= 1". The bits are
	# recorded OR-ed together as the table's "flags".
	table_aliases = {}
	for raw in lines:
		line = raw.strip()
		m = re.match(r'(?:int32_t\* )?(\w+) = arg\d\[(0x[0-9a-f]+)\]$', line)
		if m:
			table_aliases[m.group(1)] = m.group(2)
			continue
		m = re.match(r'\*(\w+) \|= (0x[0-9a-f]+|\d+)$', line)
		if m and m.group(1) in table_aliases:
			table = quest["tables"].setdefault(table_aliases[m.group(1)], {})
			table["flags"] = table.get("flags", 0) | literal(m.group(2))
			continue
		m = re.match(r'std_string_assign_cstr_n\(&(var_\w+), "([^"]*)"', line)
		if m:
			last_string[m.group(1)] = m.group(2)
			continue
		m = re.match(r'QuestStringVector_PushBack\(&arg\d\[(0x[0-9a-f]+)\], &(var_\w+)\)', line)
		if m:
			quest["lists"].setdefault(m.group(1), []).append(last_string.get(m.group(2)))
			continue
		m = re.match(r'(?:void\* )?(\w+) = CRT_operator_new\((0x[0-9a-f]+|\d+)\)', line)
		if m:
			mission, mission_vars = {"size": literal(m.group(2)), "fields": {}}, {m.group(1)}
			quest["missions"].append(mission)
			continue
		if mission_vars:
			m = re.match(r'(\w+) = (\w+)$', line)
			if m and m.group(2) in mission_vars:
				mission_vars.add(m.group(1))
				continue
			m = re.match(r'\*\((\w+) \+ (0x[0-9a-f]+|\d+)\) = ' + VALUE + r'$', line)
			if m and m.group(1) in mission_vars:
				m = re.match(r'\*\(\w+ \+ (0x[0-9a-f]+|\d+)\) = ' + VALUE + r'$', line)
				mission["fields"][hex(literal(m.group(1)))] = literal(m.group(2))
				continue
		m = re.match(r'\*\(arg\d\[(0x[0-9a-f]+)\] \+ (0x[0-9a-f]+|\d+)\) = ' + VALUE + r'$', line)
		if m:
			quest["tables"].setdefault(m.group(1), {})[hex(literal(m.group(2)))] = literal(m.group(3))
			continue
		m = re.match(r'arg\d\[(0x[0-9a-f]+)\] = ' + VALUE + r'$', line)
		if m:
			quest["words"][m.group(1)] = literal(m.group(2))
			continue
		# A byte or word inside a dword slot ("0x106.b": the first
		# prerequisite's completion count). A value read at run time is
		# kept as null so the generator cannot mistake it for a default.
		m = re.match(r'arg\d\[(0x[0-9a-f]+)\]\.([bw]) = (.+)$', line)
		if m:
			value = literal(m.group(3)) if re.fullmatch(VALUE, m.group(3)) else None
			quest["words"][m.group(1) + "." + m.group(2)] = value
			continue
		m = re.match(r'\*\(arg\d \+ (0x[0-9a-f]+)\) = ' + VALUE + r'$', line)
		if m:
			quest["words"]["+" + m.group(1)] = literal(m.group(2))
	return quest


# ================
# main
# ================
def main():
	parser = argparse.ArgumentParser(description=__doc__.splitlines()[3])
	parser.add_argument("--hlil", required=True, help="Binary Ninja HLIL dump of the compiled initializers")
	parser.add_argument("--methods", required=True, help="dump of each class's overriding vtable methods, keyed CODE@slot")
	parser.add_argument("--output", default=str(OUTPUT))
	args = parser.parse_args()
	raw = Path(args.hlil).read_bytes()
	dump = json.loads(raw)
	overrides = {}
	for key in json.loads(Path(args.methods).read_bytes()):
		code, slot = key.split("@")
		# +0x50 is the deleting destructor and +0xDC the initializer itself.
		if slot not in ("0x50", "0xdc"):
			overrides.setdefault(code, []).append(slot)
	quests = {}
	for code in sorted(dump):
		entry = dump[code]
		if not entry.get("init"):
			raise ValueError(code + ": no initializer text")
		quest = parse_initializer(entry["init"])
		# The vtable slots this class overrides beyond its destructor and
		# initializer: each is custom behaviour the generator must not drop.
		quest["overrides"] = sorted(overrides.get(code, []), key=lambda slot: int(slot, 16))
		quests[code] = quest
	snapshot = {"format": FORMAT, "dumpSHA256": hashlib.sha256(raw).hexdigest(), "quests": quests}
	Path(args.output).parent.mkdir(parents=True, exist_ok=True)
	Path(args.output).write_text(json.dumps(snapshot, indent="\t", ensure_ascii=False) + "\n", encoding="utf-8", newline="\n")


if __name__ == "__main__":
	main()
