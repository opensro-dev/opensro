"""
===========================================================================
revtool_index.py - cmd_index: scan the dump and build the function index

Part of revtool, the non-destructive symbol overlay for the SRO_Client
pseudocode dump. revtool.py is the entry point; this module owns one
command group or layer and is imported by it (and by revtool_rizin.py
through revtool's re-exports). The dump is an immutable Binary Ninja
hypothesis snapshot: original PE bytes and exact proofs outrank it,
and nothing here edits it.
===========================================================================
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from revtool_dump import get_binary
from revtool_store import meta_set, record_artifact, sha256_file
from revtool_syntax import ADDR_RE, header_decl_name, header_name_for_line

def cmd_index(args, con):
	dump = args.dump
	if not os.path.exists(dump):
		print(f"error: dump not found: {dump}", file=sys.stderr)
		return 1

	print(f"indexing {dump} ...")
	headers = []  # (addr, name, signature, start_byte, start_line)
	seen_addr = set()

	def add_header(addr_b, name_b, code_b, off, ln):
		addr = addr_b.decode("ascii").lower()
		if addr in seen_addr:
			return False
		seen_addr.add(addr)
		headers.append((addr, name_b.decode("ascii"),
						code_b.decode("utf-8", "replace").rstrip(), off, ln))
		return True

	with open(dump, "rb") as f:
		offset = 0
		line_no = 0
		# Last non-blank addr line, kept so a following `<addr> {` can promote a
		# convention-less declaration into a header.
		prev = None  # (addr_b, code_b, offset, line_no)
		for raw in f:
			line_no += 1
			m = ADDR_RE.match(raw)
			if not m:
				# Blank lines keep adjacency (header/brace are separated by one);
				# any other non-addr line breaks it.
				if raw.strip():
					prev = None
				offset += len(raw)
				continue

			addr_b = m.group(1).lower()
			code = m.group(3)
			is_header = False

			# Rules (a) and (b), see header_name_for_line().
			name_b = header_name_for_line(addr_b, code)
			if name_b is not None:
				if add_header(addr_b, name_b, code, offset, line_no):
					is_header = True

			if is_header:
				prev = None
				offset += len(raw)
				continue

			# Rule (c): a `<addr> {` line whose same-address predecessor is a
			# function declaration we have not captured yet.
			if code.strip() == b"{" and prev is not None and prev[0] == addr_b:
				pname = header_decl_name(prev[1])
				if pname:
					add_header(prev[0], pname, prev[1], prev[2], prev[3])
					prev = None
					offset += len(raw)
					continue

			prev = (addr_b, code, offset, line_no)
			offset += len(raw)
		total_bytes = offset
		total_lines = line_no

	# Headers must be in file order for the end-byte (next header) computation.
	headers.sort(key=lambda h: h[3])

	# Compute end_byte/end_line as the start of the next header.
	con.execute("DELETE FROM functions")
	rows = []
	for i, (addr, name, sig, sb, sl) in enumerate(headers):
		if i + 1 < len(headers):
			eb = headers[i + 1][3]
			el = headers[i + 1][4] - 1
		else:
			eb = total_bytes
			el = total_lines
		rows.append((addr, name, sig, sb, eb, sl, el))
	con.executemany(
		"INSERT OR REPLACE INTO functions "
		"(addr,name,signature,start_byte,end_byte,start_line,end_line) "
		"VALUES (?,?,?,?,?,?,?)",
		rows,
	)
	con.execute(
		"INSERT OR REPLACE INTO meta(key,value) VALUES('dump',?)", (os.path.abspath(dump),)
	)
	con.execute(
		"INSERT OR REPLACE INTO meta(key,value) VALUES('dump_size',?)", (str(total_bytes),)
	)
	dump_hash = sha256_file(dump)
	meta_set(con, "dump_sha256", dump_hash)
	meta_set(con, "dump_line_count", total_lines)
	meta_set(con, "dump_function_count", len(rows))
	binary = get_binary(con, args)
	if binary and os.path.isfile(binary):
		target_hash = sha256_file(binary)
		meta_set(con, "target_sha256", target_hash)
		record_artifact(con, "target", binary, producer="PE file")
	record_artifact(
		con, "binary-ninja-hlil", dump, record_count=len(rows),
		producer="Binary Ninja HLIL linear-view export",
		version="legacy-unrecorded",
		settings={"format": "linear-hlil-text", "line_count": total_lines},
	)
	con.commit()
	print(f"indexed {len(rows)} functions across {total_lines} lines; "
		  f"dump sha256={dump_hash}.")
	return 0


# --------------------------------------------------------------------------- #
# overlay loading
# --------------------------------------------------------------------------- #
