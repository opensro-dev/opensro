"""Tests for the phantom-ledger defects (AGENT-AI, wave 6).

WHY THIS FILE EXISTS. Two commands reported work they had not done, in the language
reserved for work they had:

  1. `xref "+ 0x88"` printed "not a known symbol", then charged investigation debt
     AGAINST that string, then told the agent to run `revtool seen "+ 0x88"` - demanding
     a label for a thing the same command had just called unknown. The row can never be
     discharged by labelling a symbol because there is no symbol, and it counts against
     a per-author budget that BLOCKS work at 40. 404 such rows accumulated in the live
     store; one agent was carrying 22 phantom units out of 27.

  2. `rename global 0x861300 Foo` stored the address as the key and printed a confident
     success line for a rename that apply_renames can never fire. The guard added for
     this accepted any well-formed sub_/data_ token ON SHAPE, which silently passed 85
     more keys that are well-formed and simply absent from the dump.

THE TRAP IN FIXING (2), which these tests pin down: `functions` indexes FUNCTIONS ONLY,
so all 33,193 data_ tokens are legitimately missing from it. A guard that tests that
table cries wolf on 749 valid data_ renames. AGENT-AD hit this and corrected it; the
correct test is neither the table nor the shape but whether the token OCCURS IN THE
DUMP, which is precisely what apply_renames requires.
"""
import contextlib
import importlib.util
import io
import os
import tempfile
import unittest
from pathlib import Path

MODULE_PATH = Path(__file__).resolve().parents[1] / "revtool.py"
SPEC = importlib.util.spec_from_file_location("revtool_phantom_under_test", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
revtool = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(revtool)

# A miniature dump. sub_401000 and data_bc7420 OCCUR here; data_bd516c and sub_540710
# are well-formed tokens that deliberately do NOT, which is the 85-key case.
DUMP_TEXT = """\
00401000  void __fastcall sub_401000(void* arg1)
00401004  {
00401008      sub_401020((char*)arg1 + 0x88);
0040100c      g_thing = data_bc7420;
00401010      return data_bc7420_extra;
00401014  }
00401020  void __fastcall sub_401020(void* arg1)
00401024  {
00401028      return;
0040102c  }
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
        self.__dict__.update(kw)


class PhantomLedgerTestCase(unittest.TestCase):
    def setUp(self):
        fd, self.db = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(self.db)
        fd, self.dump = tempfile.mkstemp(suffix=".txt")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write(DUMP_TEXT)
        self.con = revtool.open_db(self.db)
        for addr, name in (("00401000", "sub_401000"), ("00401020", "sub_401020")):
            self.con.execute(
                "INSERT INTO functions(addr,name,signature,start_byte,end_byte,"
                "start_line,end_line) VALUES(?,?,?,0,%d,1,10)" % len(DUMP_TEXT),
                (addr, name, f"void {name}(void*)"))
        self.con.execute("INSERT INTO meta(key,value) VALUES('dump',?)", (self.dump,))
        self.con.commit()

    def tearDown(self):
        self.con.close()
        for p in (self.db, self.dump):
            try:
                os.unlink(p)
            except OSError:
                pass

    def xref(self, target):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            rc = revtool.cmd_xref(Args(symbol=target, dump=self.dump), self.con)
        return rc, out.getvalue(), err.getvalue()

    def rename(self, key, new):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            rc = revtool.cmd_rename(Args(a=key, b=new, dump=self.dump), self.con)
        return rc, out.getvalue(), err.getvalue()

    def charged(self, symbol):
        return self.con.execute(
            "SELECT count FROM access_log WHERE symbol=? AND kind='xref'",
            (symbol,)).fetchone()


# --------------------------------------------------------------------------- #
# 1. xref must not charge debt against something that is not a symbol
# --------------------------------------------------------------------------- #
class XrefDoesNotChargePhantomDebt(PhantomLedgerTestCase):

    def test_offset_expression_is_not_charged(self):
        """`+ 0x88` is the single most common phantom in the live store."""
        self.xref("+ 0x88")
        self.assertIsNone(self.charged("+ 0x88"),
                          "xref charged investigation debt against an offset expression")

    def test_bare_identifier_that_is_not_a_symbol_is_not_charged(self):
        self.xref("CBufRender")
        self.assertIsNone(self.charged("CBufRender"))

    def test_unresolvable_address_is_not_charged(self):
        """In-image address with no function and no data_ token: nothing to label."""
        self.xref("0x00544000")
        self.assertIsNone(self.charged("0x00544000"))

    def test_c_expression_fragment_is_not_charged(self):
        self.xref("(char*)arg1 + 0x98")
        self.assertIsNone(self.charged("(char*)arg1 + 0x98"))

    def test_says_plainly_that_nothing_was_charged(self):
        """The agent must be told WHY no debt appeared, or the tool looks broken."""
        _rc, _out, err = self.xref("+ 0x88")
        self.assertIn("no investigation debt charged", err)
        self.assertIn("nothing to label", err)

    def test_does_not_instruct_the_agent_to_name_a_non_symbol(self):
        """The footer used to print `revtool seen + 0x88 "<what it does>"`."""
        _rc, out, _err = self.xref("+ 0x88")
        self.assertNotIn("revtool seen + 0x88", out)
        self.assertNotIn("UNLABELED", out)

    def test_phantom_target_does_not_consume_the_exploration_budget(self):
        """Three phantom xrefs used to fill a budget of 3 and block real work."""
        for t in ("+ 0x88", "CBufRender", "0x00544000"):
            self.xref(t)
        self.assertEqual(revtool.compute_debt(self.con, author="tester"), [],
                         "phantom xrefs consumed the per-author exploration budget")


# --------------------------------------------------------------------------- #
# 2. ...while still charging for everything that IS a symbol (must-stay-working)
# --------------------------------------------------------------------------- #
class XrefStillChargesRealSymbols(PhantomLedgerTestCase):

    def test_real_function_is_still_charged(self):
        self.xref("sub_401000")
        self.assertIsNotNone(self.charged("sub_401000"),
                             "the fix stopped charging debt for genuine investigations")

    def test_indexed_function_with_zero_references_is_still_charged(self):
        """Indexed but unreferenced is a REAL symbol; 0 hits must not excuse the debt."""
        self.con.execute(
            "INSERT INTO functions(addr,name,signature,start_byte,end_byte,"
            "start_line,end_line) VALUES('00499999','sub_499999','void f()',0,0,1,1)")
        self.con.commit()
        self.xref("sub_499999")
        self.assertIsNotNone(self.charged("sub_499999"))

    def test_data_symbol_present_in_dump_is_still_charged(self):
        """data_ tokens are real symbols even though `functions` never lists them."""
        self.xref("data_bc7420")
        self.assertIsNotNone(self.charged("data_bc7420"))

    def test_address_that_resolves_charges_the_resolved_symbol(self):
        """AGENT-AD's fix: the charge lands on sub_401000, not on the address string."""
        self.xref("0x401000")
        self.assertIsNotNone(self.charged("sub_401000"))
        self.assertIsNone(self.charged("0x401000"))

    def test_applied_name_charges_the_original_token(self):
        self.con.execute("INSERT INTO globals(orig_name,new_name,kind) "
                         "VALUES('sub_401000','MyNiceName','func')")
        self.con.commit()
        self.xref("MyNiceName")
        self.assertIsNotNone(self.charged("sub_401000"))
        self.assertIsNone(self.charged("MyNiceName"))

    def test_unknown_symbol_still_gets_the_loud_warning(self):
        """Not charging must not become not telling."""
        _rc, _out, err = self.xref("+ 0x88")
        self.assertIn("not a known symbol", err)
        self.assertIn("NOT evidence", err)


# --------------------------------------------------------------------------- #
# 3. token_occurs_in_dump - the ground truth both other guards were missing
# --------------------------------------------------------------------------- #
class TokenOccursInDump(PhantomLedgerTestCase):

    def test_finds_a_function_token(self):
        self.assertIs(revtool.token_occurs_in_dump(self.dump, "sub_401000"), True)

    def test_finds_a_data_token(self):
        self.assertIs(revtool.token_occurs_in_dump(self.dump, "data_bc7420"), True)

    def test_absent_token_is_absent(self):
        self.assertIs(revtool.token_occurs_in_dump(self.dump, "data_bd516c"), False)
        self.assertIs(revtool.token_occurs_in_dump(self.dump, "sub_540710"), False)

    def test_respects_word_boundaries(self):
        """`data_bc7420_extra` occurs in the dump; it must not vouch for a prefix."""
        self.assertIs(revtool.token_occurs_in_dump(self.dump, "data_bc742"), False)

    def test_missing_dump_returns_none_not_false(self):
        """Unknown is not the same fact as absent, and must not trigger a warning."""
        self.assertIsNone(revtool.token_occurs_in_dump("/no/such/dump.txt", "sub_401000"))


# --------------------------------------------------------------------------- #
# 4. the rename guard: catches the dead keys, stays silent on valid data_ renames
# --------------------------------------------------------------------------- #
class RenameGuard(PhantomLedgerTestCase):

    def test_warns_on_address_keyed_rename(self):
        _rc, out, err = self.rename("0x401000", "Foo")
        self.assertIn("stored for address-based dossiers/planners", err)
        self.assertIn("cannot rewrite dump text", err)
        self.assertIn("did you mean `sub_401000`", err)
        self.assertIn("global: 0x401000 -> Foo", out)

    def test_silent_for_data_token_that_occurs_in_the_dump(self):
        """AGENT-AD's regression: a functions-table guard fires on 749 valid renames."""
        _rc, _out, err = self.rename("data_bc7420", "g_fogStartNear")
        self.assertEqual(err, "",
                         "warned about a data_ symbol that is a real dump token")

    def test_silent_for_indexed_function(self):
        _rc, _out, err = self.rename("sub_401000", "Nice_Name")
        self.assertEqual(err, "")

    def test_warns_for_well_formed_token_absent_from_the_dump(self):
        """The 85 keys the shape-only guard let through."""
        _rc, _out, err = self.rename("data_bd516c", "g_something")
        self.assertIn("does not occur in the dump", err)
        self.assertIn("well-formed token", err)

    def test_warns_for_applied_name_chain(self):
        self.con.execute("INSERT INTO globals(orig_name,new_name,kind) "
                         "VALUES('sub_401000','AppliedName','func')")
        self.con.commit()
        _rc, _out, err = self.rename("AppliedName", "SecondName")
        self.assertIn("APPLIED name", err)
        self.assertIn("chains a second rename", err)

    def test_stays_silent_when_the_dump_is_unavailable(self):
        """A guard that cannot check must not guess; unknown is not 'absent'.

        get_dump() prefers args.dump over the meta row, so the missing path has to be
        passed the way the CLI would pass it - the first version of this test only
        changed meta and silently kept reading the real fixture dump.
        """
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            revtool.cmd_rename(
                Args(a="data_bd516c", b="g_something", dump="/no/such/dump.txt"),
                self.con)
        self.assertEqual(err.getvalue(), "")


# --------------------------------------------------------------------------- #
# 5. the migration hazard, pinned so the next agent does not rediscover it
# --------------------------------------------------------------------------- #
class DeletingADeadRenameUnmasksPhantomDebt(PhantomLedgerTestCase):
    """symbol_is_annotated() counts ANY globals key as a label, including a dead one.

    So a rename keyed on an address was silently DISCHARGING an access_log row keyed on
    the same address string. Deleting 21 such dead renames un-masked 20 hidden phantom
    rows and pushed one author from 29 to 45 against a cap of 40. Whoever cleans dead
    renames next must re-key the ledger rows in the same pass; this test states why.
    """

    def test_dead_rename_discharges_a_phantom_row_and_deleting_it_restores_the_debt(self):
        self.con.execute("INSERT INTO access_log(symbol,kind,author,first_ts,last_ts,"
                         "count) VALUES('0x401000','xref','tester','t','t',1)")
        self.con.execute("INSERT INTO globals(orig_name,new_name,kind) "
                         "VALUES('0x401000','SomeName','func')")
        self.con.commit()
        self.assertEqual(revtool.compute_debt(self.con, author="tester"), [],
                         "precondition: the dead rename discharges the phantom row")

        self.con.execute("DELETE FROM globals WHERE orig_name='0x401000'")
        self.con.commit()
        debt = revtool.compute_debt(self.con, author="tester")
        self.assertEqual([d[0] for d in debt], ["0x401000"],
                         "deleting the dead rename must expose the phantom debt")

    def test_rekeying_the_ledger_row_onto_the_real_token_discharges_it_honestly(self):
        """The correct repair: move the row to the token the WORKING rename covers."""
        self.con.execute("INSERT INTO access_log(symbol,kind,author,first_ts,last_ts,"
                         "count) VALUES('0x401000','xref','tester','t','t',1)")
        self.con.execute("INSERT INTO globals(orig_name,new_name,kind) "
                         "VALUES('sub_401000','SomeName','func')")
        self.con.commit()
        self.assertEqual([d[0] for d in revtool.compute_debt(self.con, author="tester")],
                         ["0x401000"])
        self.con.execute("UPDATE access_log SET symbol='sub_401000' "
                         "WHERE symbol='0x401000'")
        self.con.commit()
        self.assertEqual(revtool.compute_debt(self.con, author="tester"), [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
