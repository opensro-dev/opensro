"""Tests for the mis-parsed function index and the unpayable-debt ledger (AGENT-AK).

WHY THIS FILE EXISTS. Two things reported a fact they had not established:

  1. THE FUNCTION INDEX ANSWERED WITH A KEYWORD. `header_decl_name` scanned to the
     FIRST '(' on a header line. In
         009c68f0  int32_t __convention("regparm") __allshr(int32_t, int32_t, char)
     that '(' opens the ANNOTATION's argument list, not the parameter list, so the
     stored name was `__convention` - for 29 distinct addresses at once. Anything
     resolving by name across them was ambiguous, and an agent looking one up got a
     calling-convention keyword back and could reasonably believe that was the name.

  2. THE DEBT LEDGER BILLED FOR THINGS THAT ARE NOT SYMBOLS. `compute_debt` charged
     every unlabeled access_log row, including rows whose target is `+ 0x88` or a hex
     literal outside the image. No label can ever discharge those, because there is
     nothing to label, yet they counted toward the cap that blocks work at 40.

THE TRAP IN FIXING (2), which several tests here pin down: `xref`'s "is this a real
symbol?" test and `token_occurs_in_dump`'s "can this rename key ever fire?" test look
interchangeable and are NOT. For a needle beginning with a non-word byte the `\\b`
anchors and the neighbouring-byte test are exact OPPOSITES, so building the ledger on
the wrong one silently reclassifies targets. The rule is therefore consulted from ONE
function by both callers rather than copied into each.
"""
import importlib.util
import os
import sqlite3
import tempfile
import unittest
from pathlib import Path

MODULE_PATH = Path(__file__).resolve().parent / "revtool.py"
SPEC = importlib.util.spec_from_file_location("revtool_index_under_test", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
revtool = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(revtool)

# A miniature dump exercising all three index rules plus the annotation shape.
#   sub_401000  rule (a), explicit calling convention
#   sub_402000  rule (b), self-referential, with a __convention annotation in the way
#   __allshr    rule (c), structural `<addr> {`, annotation before the real name
#   j_sub_403500 rule (c), a thunk whose name is NOT its own address
DUMP_TEXT = """\
00401000  void __fastcall sub_401000(void* arg1)
00401004  {
00401008      sub_402000((char*)arg1 + 0x88);
0040100c      g_thing = data_bc7420;
0040100e      trace("snd_activate", UIIT_CTL_NO, 0x61a4);
00401010  }
00402000  int32_t __convention("regparm") sub_402000(void* arg1)
00402004  {
00402008      return __allshr(1, 2, 3);
0040200c  }
009c68f0  int32_t __convention("regparm") __allshr(int32_t arg1, int32_t arg2, char arg3) __pure

009c68f0  {
009c68f4      return arg1;
009c68f8  }
00403400  int32_t* __convention("regparm") j_sub_403500(int32_t arg1)

00403400  {
00403404      return 0;
00403408  }
"""


class Args:
    """Minimal stand-in for the argparse namespace the cmd_* functions read."""

    def __init__(self, **kw):
        self.limit = 20
        self.author = "tester"
        self.no_strict = True
        self.strict = False
        self.force = False
        self.tier = "guess"
        self.evidence = None
        self.basis = None
        self.method = None
        self.blindspots = None
        self.scope = "global"
        self.dump = None
        self.__dict__.update(kw)


# --------------------------------------------------------------------------- #
# 1. the header parser
# --------------------------------------------------------------------------- #
class HeaderParserTestCase(unittest.TestCase):
    def test_annotation_does_not_become_the_function_name(self):
        """The bug itself: `__convention("regparm")` must not be read as the name."""
        code = (b'int32_t __convention("regparm") __allshr(int32_t arg1, '
                b'int32_t arg2, char arg3) __pure')
        self.assertEqual(revtool.header_decl_name(code), b"__allshr")

    def test_annotation_before_a_thunk_name(self):
        code = (b'int32_t* __convention("regparm") j_sub_406f50(int32_t arg1, '
                b'int32_t arg2, int128_t* arg3)')
        self.assertEqual(revtool.header_decl_name(code), b"j_sub_406f50")

    def test_every_recovered_crt_helper_name(self):
        """All five non-thunk casualties, verbatim from the 115MB dump."""
        cases = [
            (b'int32_t __convention("regparm") __allshr(int32_t arg1, int32_t arg2, '
             b'char arg3) __pure', b"__allshr"),
            (b'int32_t __convention("regparm") __allshl(int32_t arg1, int32_t arg2, '
             b'char arg3) __pure', b"__allshl"),
            (b'uint32_t __convention("regparm") __aullshr(int32_t arg1, int32_t arg2, '
             b'char arg3) __pure', b"__aullshr"),
            (b'int32_t __convention("regparm") __NLG_Notify1(int32_t arg1, int32_t, '
             b'int32_t arg3, int32_t arg4 @ ebp)', b"__NLG_Notify1"),
            (b'int32_t __convention("regparm") __alloca_probe_16(void* arg1)',
             b"__alloca_probe_16"),
        ]
        self.assertTrue(cases, "precondition: the case list is not empty")
        for code, want in cases:
            with self.subTest(want=want):
                self.assertEqual(revtool.header_decl_name(code), want)

    def test_plain_definition_is_unchanged(self):
        self.assertEqual(revtool.header_decl_name(b"int80_t sub_9c6920(void* arg1)"),
                         b"sub_9c6920")

    def test_control_flow_is_still_rejected(self):
        for code in (b"if (arg1 == 3)", b"while (x)", b"switch (v)", b"return (a)"):
            with self.subTest(code=code):
                self.assertIsNone(revtool.header_decl_name(code))

    def test_call_statement_is_still_rejected(self):
        """A bare `name(...)` has no return type, so it is a call, not a definition."""
        self.assertIsNone(revtool.header_decl_name(b"sub_402000(arg1);"))

    def test_expression_continuation_is_still_rejected(self):
        self.assertIsNone(revtool.header_decl_name(b"&& sub_401000(arg))"))

    def test_function_pointer_return_type_is_still_rejected_by_rule_c(self):
        """Rule (a) owns these; rule (c) must not start claiming them."""
        code = b"float (*)[0x4] __stdcall sub_96069b(float (* arg1)[0x4], int64_t* arg2)"
        self.assertIsNone(revtool.header_decl_name(code))

    def test_depth0_ident_calls_ignores_parameters(self):
        """Parameters live at depth >= 1, so only the declarator yields candidates."""
        code = b'int32_t __convention("regparm") __allshr(int32_t a, char (*f)(int))'
        got = [c[0] for c in revtool.depth0_ident_calls(code)]
        self.assertEqual(got, [b"__convention", b"__allshr"])

    def test_rule_a_takes_the_name_after_the_calling_convention(self):
        self.assertEqual(
            revtool.header_name_for_line(b"00401000",
                                         b"void __fastcall sub_401000(void* arg1)"),
            b"sub_401000")

    def test_rule_b_takes_a_self_referential_definition(self):
        self.assertEqual(
            revtool.header_name_for_line(
                b"00402000", b'int32_t __convention("regparm") sub_402000(void* arg1)'),
            b"sub_402000")

    def test_rule_ab_returns_none_for_a_thunk_it_cannot_name(self):
        """Which is exactly why rule (c) - and its bug - was reached for these."""
        self.assertIsNone(revtool.header_name_for_line(
            b"00403400",
            b'int32_t* __convention("regparm") j_sub_403500(int32_t arg1)'))


# --------------------------------------------------------------------------- #
# 2. end-to-end: `index` stores the real names
# --------------------------------------------------------------------------- #
class IndexEndToEndTestCase(unittest.TestCase):
    def setUp(self):
        fd, self.db = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(self.db)
        fd, self.dump = tempfile.mkstemp(suffix=".txt")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write(DUMP_TEXT)
        self.con = revtool.open_db(self.db)

    def tearDown(self):
        self.con.close()
        for p in (self.db, self.dump):
            try:
                os.unlink(p)
            except OSError:
                pass

    def test_index_does_not_store_the_annotation_keyword_as_a_name(self):
        revtool.cmd_index(Args(dump=self.dump), self.con)
        names = {r[0]: r[1] for r in self.con.execute(
            "SELECT addr,name FROM functions")}
        self.assertTrue(names, "precondition: the index is not empty")
        self.assertNotIn("__convention", names.values(),
                         "the calling-convention keyword must never be a function name")
        self.assertEqual(names.get("009c68f0"), "__allshr")
        self.assertEqual(names.get("00403400"), "j_sub_403500")

    def test_index_still_finds_rule_a_and_rule_b_headers(self):
        revtool.cmd_index(Args(dump=self.dump), self.con)
        names = {r[0]: r[1] for r in self.con.execute(
            "SELECT addr,name FROM functions")}
        self.assertEqual(names.get("00401000"), "sub_401000")
        self.assertEqual(names.get("00402000"), "sub_402000")

    def test_no_address_is_indexed_under_a_shared_keyword(self):
        """The real damage was 29 addresses sharing one name; assert none do."""
        revtool.cmd_index(Args(dump=self.dump), self.con)
        rows = self.con.execute(
            "SELECT name,COUNT(*) c FROM functions GROUP BY name HAVING c>1").fetchall()
        self.assertEqual(rows, [], f"names shared by several addresses: {rows}")


# --------------------------------------------------------------------------- #
# 3. the unpayable-debt rule
# --------------------------------------------------------------------------- #
class UnpayableDebtTestCase(unittest.TestCase):
    def setUp(self):
        fd, self.db = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(self.db)
        fd, self.dump = tempfile.mkstemp(suffix=".txt")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write(DUMP_TEXT)
        self.con = revtool.open_db(self.db)
        for addr, name in (("00401000", "sub_401000"), ("00402000", "sub_402000")):
            self.con.execute(
                "INSERT INTO functions(addr,name,signature,start_byte,end_byte,"
                "start_line,end_line) VALUES(?,?,?,0,?,1,10)",
                (addr, name, f"void {name}(void*)", len(DUMP_TEXT)))
        self.con.execute("INSERT INTO meta(key,value) VALUES('dump',?)", (self.dump,))
        self.con.commit()
        revtool._DUMP_HIT_MEMO.clear()

    def tearDown(self):
        self.con.close()
        revtool._DUMP_HIT_MEMO.clear()
        for p in (self.db, self.dump):
            try:
                os.unlink(p)
            except OSError:
                pass

    def _log(self, symbol, author="tester", kind="xref"):
        self.con.execute(
            "INSERT INTO access_log(symbol,kind,author,first_ts,last_ts,count) "
            "VALUES(?,?,?,'t','t',1)", (symbol, kind, author))
        self.con.commit()

    def test_a_target_that_is_not_a_symbol_is_not_billed(self):
        self._log("0x00544000")
        self.assertEqual(revtool.compute_debt(self.con, author="tester"), [],
                         "a hex literal that is not in the dump cannot be labeled, "
                         "so it must not be charged")

    def test_a_real_dump_token_is_still_billed(self):
        """The rule must not become an amnesty for genuine unlabeled work."""
        self._log("data_bc7420")
        debt = revtool.compute_debt(self.con, author="tester")
        self.assertEqual([d[0] for d in debt], ["data_bc7420"])

    def test_readable_search_terms_with_dump_hits_are_not_billed(self):
        """A text hit is useful evidence, but not proof the query needs a rename."""
        for symbol in ("snd_activate", "UIIT_CTL_NO", "0x61a4"):
            with self.subTest(symbol=symbol):
                self._log(symbol)
        self.assertEqual(revtool.compute_debt(self.con, author="tester"), [])

    def test_found_in_dump_does_not_promote_a_search_term_to_a_symbol(self):
        self.assertFalse(revtool.symbol_is_identifiable(
            self.con, "snd_activate", self.dump,
            found_in_dump=True, consult_dump=False))

    def test_a_function_in_the_index_is_still_billed(self):
        self._log("sub_401000")
        debt = revtool.compute_debt(self.con, author="tester")
        self.assertEqual([d[0] for d in debt], ["sub_401000"])

    def test_an_indexed_readable_import_is_not_billed(self):
        self.con.execute(
            "INSERT INTO functions(addr,name,signature,start_byte,end_byte,"
            "start_line,end_line) VALUES('00770000','GetGlyphOutlineW',"
            "'int GetGlyphOutlineW()',0,0,1,1)")
        self.con.commit()
        self._log("GetGlyphOutlineW")
        self.assertEqual(revtool.compute_debt(self.con, author="tester"), [])

    def test_an_address_that_resolves_to_a_function_is_still_billed(self):
        """`xref 0x401000` denotes sub_401000, so a row keyed that way is real debt.

        Skipping it would ERASE debt somebody genuinely owes, which is the one outcome
        worse than keeping noise.
        """
        self._log("0x401000")
        debt = revtool.compute_debt(self.con, author="tester")
        self.assertEqual([d[0] for d in debt], ["0x401000"])

    def test_billing_continues_when_the_dump_cannot_be_consulted(self):
        """An unanswerable question must not silently discharge debt."""
        os.unlink(self.dump)
        self._log("SomethingUnknowable")
        debt = revtool.compute_debt(self.con, author="tester")
        self.assertEqual([d[0] for d in debt], ["SomethingUnknowable"])

    def test_the_unpayable_row_is_kept_as_history(self):
        """Excluding it from the ledger must NOT delete the audit trail."""
        self._log("0x00544000")
        self.assertEqual(revtool.compute_debt(self.con, author="tester"), [])
        rows = self.con.execute(
            "SELECT symbol,kind,count FROM access_log WHERE symbol='0x00544000'"
        ).fetchall()
        self.assertEqual(rows, [("0x00544000", "xref", 1)],
                         "the row is false as debt but true as history")

    def test_identifiable_accepts_each_source_on_its_own(self):
        self.con.execute("INSERT INTO globals(orig_name,new_name,kind) "
                         "VALUES('data_deadbeef','Renamed','data')")
        self.con.commit()
        for sym, why in (("data_deadbeef", "a globals key"),
                         ("sub_401000", "a functions-index name"),
                         ("data_bc7420", "a dump token")):
            with self.subTest(why=why):
                self.assertTrue(revtool.symbol_is_identifiable(
                    self.con, sym, self.dump), why)

    def test_identifiable_rejects_an_expression(self):
        self.assertFalse(revtool.symbol_is_identifiable(self.con, "+ 0x88", self.dump))

    def test_identifiable_rejects_readable_dump_text(self):
        for symbol in ("snd_activate", "UIIT_CTL_NO", "0x61a4"):
            with self.subTest(symbol=symbol):
                self.assertFalse(
                    revtool.symbol_is_identifiable(self.con, symbol, self.dump))


# --------------------------------------------------------------------------- #
# 4. the two predicates that must not be swapped, and the one that must be shared
# --------------------------------------------------------------------------- #
class PredicateSharingTestCase(unittest.TestCase):
    def setUp(self):
        fd, self.dump = tempfile.mkstemp(suffix=".txt")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write(DUMP_TEXT)
        fd, self.db = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(self.db)
        self.con = revtool.open_db(self.db)
        revtool._DUMP_HIT_MEMO.clear()

    def tearDown(self):
        self.con.close()
        revtool._DUMP_HIT_MEMO.clear()
        for p in (self.db, self.dump):
            try:
                os.unlink(p)
            except OSError:
                pass

    def test_xref_scan_and_token_occurs_are_not_interchangeable(self):
        """`(char*)arg1 + 0x88` is IN the dump text, but `xref` finds no hit for it.

        token_occurs_in_dump wants non-word bytes on both sides; the `\\b` anchors want
        a word-char transition. For a needle starting with `+` those are opposites.
        Pinned so nobody substitutes one for the other again.
        """
        needle = "+ 0x88"
        self.assertIn(needle, DUMP_TEXT, "precondition: the text really is present")
        self.assertTrue(revtool.token_occurs_in_dump(self.dump, needle))
        self.assertIsNone(next(revtool.iter_dump_symbol_hits(self.dump, needle), None))

    def test_the_two_predicates_agree_on_ordinary_identifiers(self):
        for sym in ("sub_401000", "data_bc7420"):
            with self.subTest(sym=sym):
                self.assertTrue(revtool.token_occurs_in_dump(self.dump, sym))
                self.assertIsNotNone(
                    next(revtool.iter_dump_symbol_hits(self.dump, sym), None))

    def test_xref_and_compute_debt_consult_the_same_function(self):
        """Anti-drift: replacing the ONE predicate must move BOTH callers.

        The defect this guards against is a second implementation that agrees today and
        silently diverges the moment the first is fixed.
        """
        self.con.execute(
            "INSERT INTO functions(addr,name,signature,start_byte,end_byte,start_line,"
            "end_line) VALUES('00401000','sub_401000','void sub_401000(void*)',0,?,1,5)",
            (len(DUMP_TEXT),))
        self.con.execute("INSERT INTO meta(key,value) VALUES('dump',?)", (self.dump,))
        self.con.execute(
            "INSERT INTO access_log(symbol,kind,author,first_ts,last_ts,count) "
            "VALUES('sub_401000','xref','tester','t','t',1)")
        self.con.commit()

        self.assertEqual([d[0] for d in revtool.compute_debt(self.con, author="tester")],
                         ["sub_401000"], "precondition: this really is billed")

        # The predicate lives in revtool_dump; compute_debt and cmd_xref
        # both dereference it there at call time, so replacing it in its
        # home module (not a per-caller copy) is what must move both.
        import revtool_dump
        real = revtool_dump.symbol_is_identifiable
        try:
            revtool_dump.symbol_is_identifiable = lambda *a, **k: False
            self.assertEqual(revtool.compute_debt(self.con, author="tester"), [],
                             "compute_debt must go through symbol_is_identifiable")
            import io
            import contextlib
            buf = io.StringIO()
            with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(buf):
                revtool.cmd_xref(Args(symbol="sub_401000", dump=self.dump), self.con)
            self.assertIn("no investigation debt charged", buf.getvalue(),
                          "cmd_xref must go through the SAME symbol_is_identifiable")
        finally:
            revtool_dump.symbol_is_identifiable = real

    def test_resolve_scan_symbol_handles_all_three_spellings(self):
        self.con.execute(
            "INSERT INTO functions(addr,name,signature,start_byte,end_byte,start_line,"
            "end_line) VALUES('00401000','sub_401000','void sub_401000(void*)',0,10,1,5)")
        self.con.execute("INSERT INTO globals(orig_name,new_name,kind) "
                         "VALUES('sub_401000','MyName','func')")
        self.con.commit()
        self.assertEqual(revtool.resolve_scan_symbol(self.con, "MyName"), "sub_401000")
        self.assertEqual(revtool.resolve_scan_symbol(self.con, "0x401000"), "sub_401000")
        self.assertEqual(revtool.resolve_scan_symbol(self.con, "sub_401000"),
                         "sub_401000")


if __name__ == "__main__":
    unittest.main()
