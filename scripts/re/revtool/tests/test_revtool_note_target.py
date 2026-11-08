"""Tests for revtool's note-target resolution.

WHY THIS FILE EXISTS (AGENT-AD, wave 6): `note list sub_86cf50` returned EMPTY with
exit 0 while the same symbol's notes were reachable under its friendly name
(`note list PathCtl_ResteerTowardGoal` -> note 10511). Agents check for prior notes
specifically to avoid duplicating research, and they check by the sub_XXXXXX address
form because that is how functions are named everywhere else in this project. So the
tool answered "no prior art here" for a question it never actually evaluated, at the
exact moment an agent was deciding whether to write a duplicate.

The contract these tests pin down:
  * a note target may be spelled as the original sub_/data_ token, the applied
    (friendly) name, or an address - all three find the same notes;
  * notes still print to STDOUT in the original `[id] [tier] target (author): body`
    format, so anything parsing that output is unaffected;
  * every new diagnostic goes to STDERR, so stdout is byte-identical for every
    invocation that already worked;
  * "this symbol exists and has zero notes" and "I have never heard of this symbol"
    are DIFFERENT, LOUD outcomes - the second is an error with a nonzero exit.
"""
import contextlib
import importlib.util
import io
import os
import tempfile
import unittest
from pathlib import Path

MODULE_PATH = Path(__file__).resolve().parents[1] / "revtool.py"
SPEC = importlib.util.spec_from_file_location("revtool_under_test", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
revtool = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(revtool)


def add_note(con, target, body, author="tester", tier="mechanism"):
    cur = con.execute(
        "INSERT INTO notes(target,body,author,tier,updated_at) VALUES(?,?,?,?,?)",
        (target, body, author, tier, revtool.now_iso()))
    con.commit()
    return cur.lastrowid


def add_func(con, addr, name):
    con.execute(
        "INSERT OR REPLACE INTO functions"
        "(addr,name,signature,start_byte,end_byte,start_line,end_line) "
        "VALUES(?,?,?,0,0,0,0)", (addr, name, f"void {name}(void)"))
    con.commit()


def add_rename(con, orig, new, kind="func"):
    con.execute("INSERT OR REPLACE INTO globals(orig_name,new_name,kind,note) "
                "VALUES(?,?,?,NULL)", (orig, new, kind))
    con.commit()


class NoteTargetResolutionTests(unittest.TestCase):
    """The reported defect and its mirror image, driven through the real CLI parser."""

    def setUp(self):
        fd, self.db = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(self.db)
        self.con = revtool.open_db(self.db)

        # The exact shape of the reported case: an indexed function, a rename to a
        # friendly name, and the research note stored under the FRIENDLY name.
        add_func(self.con, "0086cf50", "sub_86cf50")
        add_rename(self.con, "sub_86cf50", "PathCtl_ResteerTowardGoal")
        self.note_friendly = add_note(
            self.con, "PathCtl_ResteerTowardGoal",
            "x87 SELECT GOTCHA - step = dist <= lookahead ? lookahead : dist*1.8",
            author="AGENT-Y")

        # The mirror image: note stored under the sub_ token while a friendly name
        # exists, so a friendly-name search must find it too.
        add_func(self.con, "008779b0", "sub_8779b0")
        add_rename(self.con, "sub_8779b0", "CGInterface_UpdateLocalPathSteeringOverlay")
        self.note_addrform = add_note(
            self.con, "sub_8779b0", "same inversion at 0x00877b62", author="AGENT-U")

        # A known symbol that genuinely has NO notes.
        add_func(self.con, "00111111", "sub_111111")

        # A friendly name that collides: two distinct origins share one applied name.
        add_func(self.con, "00222222", "sub_222222")
        add_func(self.con, "00333333", "sub_333333")
        add_rename(self.con, "sub_222222", "Collide_Friendly")
        add_rename(self.con, "sub_333333", "Collide_Friendly")
        self.note_c1 = add_note(self.con, "sub_222222", "first colliding claim")
        self.note_c2 = add_note(self.con, "sub_333333", "second colliding claim")

        # A line note (symbol@addr form) must remain reachable by every spelling.
        self.note_line = add_note(self.con, "sub_86cf50@0086d088",
                                  "line note at the fcom site")

        # A note target that is not a symbol at all - 603 of these exist in the live
        # store (notes attached to expressions). Free-text search must keep working
        # and must NOT become an error.
        self.note_expr = add_note(self.con, "(char*)arg1 + 0x98",
                                  "expression-scoped memo")
        self.con.close()

    def tearDown(self):
        for suffix in ("", "-wal", "-shm"):
            p = self.db + suffix
            if os.path.exists(p):
                try:
                    os.unlink(p)
                except OSError:
                    pass

    def run_cli(self, *argv):
        """Drive revtool exactly as the command line does; capture both streams."""
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            rc = revtool.main(["--db", self.db, *argv])
        return rc, out.getvalue(), err.getvalue()

    # ---------------- the reported defect ----------------

    def test_note_list_by_sub_address_name_finds_the_friendly_named_note(self):
        """THE REPORTED BUG: `note list sub_86cf50` must return note 10511's analogue."""
        rc, out, _err = self.run_cli("note", "list", "sub_86cf50")
        self.assertEqual(0, rc)
        self.assertIn(f"[{self.note_friendly}]", out)
        self.assertIn("x87 SELECT GOTCHA", out)

    def test_note_list_by_friendly_name_still_works(self):
        """Regression guard: the spelling that already worked must keep working."""
        rc, out, _err = self.run_cli("note", "list", "PathCtl_ResteerTowardGoal")
        self.assertEqual(0, rc)
        self.assertIn(f"[{self.note_friendly}]", out)

    def test_note_list_by_friendly_name_finds_note_stored_under_sub_token(self):
        """The mirror image, which is 15x more common in the live store."""
        rc, out, _err = self.run_cli(
            "note", "list", "CGInterface_UpdateLocalPathSteeringOverlay")
        self.assertEqual(0, rc)
        self.assertIn(f"[{self.note_addrform}]", out)

    def test_note_list_accepts_every_address_spelling(self):
        for spelling in ("0x86cf50", "86cf50", "0086cf50", "0x0086cf50"):
            with self.subTest(spelling=spelling):
                rc, out, _err = self.run_cli("note", "list", spelling)
                self.assertEqual(0, rc)
                self.assertIn(f"[{self.note_friendly}]", out,
                              f"{spelling} did not reach the friendly-named note")

    def test_line_notes_are_reachable_by_every_spelling(self):
        for spelling in ("sub_86cf50", "PathCtl_ResteerTowardGoal", "0x86cf50"):
            with self.subTest(spelling=spelling):
                _rc, out, _err = self.run_cli("note", "list", spelling)
                self.assertIn(f"[{self.note_line}]", out)

    # ---------------- loud failure vs genuine empty ----------------

    def test_unknown_symbol_is_a_loud_error_not_an_empty_result(self):
        """A target the overlay has never heard of must NOT look like a clean empty."""
        rc, out, err = self.run_cli("note", "list", "sub_999999")
        self.assertNotEqual(0, rc, "an unresolvable target must not exit 0")
        self.assertIn("error:", err)
        self.assertIn("sub_999999", err)
        self.assertEqual("", out.strip(), "nothing should be printed as a result")

    def test_known_symbol_with_zero_notes_is_distinct_from_unknown_symbol(self):
        """The two facts the tool currently conflates must now differ."""
        rc_known, _out_known, err_known = self.run_cli("note", "list", "sub_111111")
        rc_unknown, _out_unknown, err_unknown = self.run_cli(
            "note", "list", "sub_999999")

        self.assertEqual(0, rc_known,
                         "a known symbol with no notes is a legitimate answer")
        self.assertNotEqual(0, rc_unknown)
        self.assertNotEqual(err_known.strip(), err_unknown.strip(),
                            "known-but-empty and unknown must not read the same")
        self.assertIn("0 note", err_known.lower())
        self.assertNotIn("error:", err_known)

    def test_typo_in_friendly_name_is_caught_loudly(self):
        rc, _out, err = self.run_cli("note", "list", "PathCtl_ResteerTowardGoalX")
        self.assertNotEqual(0, rc)
        self.assertIn("error:", err)

    def test_resolution_is_reported_so_the_reader_sees_what_was_searched(self):
        """Anti-false-evidence: the tool states the question it actually evaluated."""
        _rc, _out, err = self.run_cli("note", "list", "sub_86cf50")
        self.assertIn("PathCtl_ResteerTowardGoal", err)

    # ---------------- ambiguity ----------------

    def test_colliding_friendly_name_returns_all_origins_and_says_it_is_ambiguous(self):
        rc, out, err = self.run_cli("note", "list", "Collide_Friendly")
        self.assertEqual(0, rc)
        self.assertIn(f"[{self.note_c1}]", out)
        self.assertIn(f"[{self.note_c2}]", out)
        self.assertIn("ambiguous", err.lower())
        self.assertIn("sub_222222", err)
        self.assertIn("sub_333333", err)

    # ---------------- preserved behaviour ----------------

    def test_free_text_substring_search_over_targets_still_works(self):
        """`note list X` filters on the TARGET column by substring. That is the
        existing contract (bodies are not searched) and it must not change."""
        rc, out, _err = self.run_cli("note", "list", "arg1")
        self.assertEqual(0, rc)
        self.assertIn(f"[{self.note_expr}]", out)

    def test_free_text_with_no_match_is_explicit_but_not_an_error(self):
        rc, out, err = self.run_cli("note", "list", "no such prose anywhere")
        self.assertEqual(0, rc, "a free-text search that matches nothing is not an error")
        self.assertEqual("", out.strip())
        self.assertTrue(err.strip(), "a zero-result search must still say so")

    def test_bare_note_list_lists_everything(self):
        rc, out, _err = self.run_cli("note", "list")
        self.assertEqual(0, rc)
        for nid in (self.note_friendly, self.note_addrform, self.note_expr,
                    self.note_line):
            self.assertIn(f"[{nid}]", out)

    def test_stdout_note_line_format_is_unchanged(self):
        _rc, out, _err = self.run_cli("note", "list", "PathCtl_ResteerTowardGoal")
        line = next(l for l in out.splitlines() if l.startswith(f"[{self.note_friendly}]"))
        self.assertTrue(
            line.startswith(f"[{self.note_friendly}] [mechanism] "
                            f"PathCtl_ResteerTowardGoal (AGENT-Y): "),
            f"note line format drifted: {line!r}")


class NoteAddDuplicateGuardTests(unittest.TestCase):
    """`note add`'s contradiction guard is the write-side of the same defect: it only
    saw notes spelled the same way as the new target, so adding under sub_XXXXXX gave
    NO heads-up about existing research filed under the friendly name."""

    def setUp(self):
        fd, self.db = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(self.db)
        self.con = revtool.open_db(self.db)
        add_func(self.con, "0086cf50", "sub_86cf50")
        add_rename(self.con, "sub_86cf50", "PathCtl_ResteerTowardGoal")
        self.existing = add_note(self.con, "PathCtl_ResteerTowardGoal",
                                 "prior research that must not be duplicated",
                                 author="AGENT-Y")
        add_func(self.con, "00444444", "sub_444444")
        self.con.close()

    def tearDown(self):
        for suffix in ("", "-wal", "-shm"):
            p = self.db + suffix
            if os.path.exists(p):
                try:
                    os.unlink(p)
                except OSError:
                    pass

    def run_cli(self, *argv):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            rc = revtool.main(["--db", self.db, *argv])
        return rc, out.getvalue(), err.getvalue()

    def test_adding_under_sub_name_warns_about_note_filed_under_friendly_name(self):
        rc, out, err = self.run_cli(
            "note", "add", "sub_86cf50", "a new claim about the same function")
        self.assertEqual(0, rc)
        combined = out + err
        self.assertIn(f"[{self.existing}]", combined,
                      "the pre-existing note was not surfaced, so a duplicate is invited")
        self.assertIn("prior research", combined)

    def test_adding_under_friendly_name_still_warns(self):
        rc, out, err = self.run_cli(
            "note", "add", "PathCtl_ResteerTowardGoal", "another claim")
        self.assertEqual(0, rc)
        self.assertIn(f"[{self.existing}]", out + err)

    def test_add_to_unknown_symbol_shaped_target_warns_but_still_records(self):
        """A typo'd add must not fail (603 legit non-symbol targets exist), but the
        author should be told the symbol is unknown so research is not filed nowhere."""
        rc, out, err = self.run_cli("note", "add", "sub_999999", "misfiled research")
        self.assertEqual(0, rc, "note add must keep working for any target string")
        self.assertIn("note [", out)
        self.assertIn("sub_999999", err)

    def test_add_to_expression_target_does_not_warn(self):
        """Expression targets are a legitimate existing pattern; do not cry wolf."""
        rc, _out, err = self.run_cli("note", "add", "(char*)arg1 + 0x98", "memo")
        self.assertEqual(0, rc)
        self.assertNotIn("unknown", err.lower())


class NoteRemoveTests(unittest.TestCase):
    """`note rm` claimed success for ids it never deleted - the same false-evidence
    class as the lookup: a confident answer to a question never evaluated."""

    def setUp(self):
        fd, self.db = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(self.db)
        self.con = revtool.open_db(self.db)
        self.nid = add_note(self.con, "sub_86cf50", "removable")
        self.con.close()

    def tearDown(self):
        for suffix in ("", "-wal", "-shm"):
            p = self.db + suffix
            if os.path.exists(p):
                try:
                    os.unlink(p)
                except OSError:
                    pass

    def run_cli(self, *argv):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            rc = revtool.main(["--db", self.db, *argv])
        return rc, out.getvalue(), err.getvalue()

    def test_removing_an_existing_note_reports_success(self):
        rc, out, _err = self.run_cli("note", "rm", str(self.nid))
        self.assertEqual(0, rc)
        self.assertIn(f"removed note {self.nid}", out)

    def test_removing_a_nonexistent_id_does_not_claim_success(self):
        rc, out, err = self.run_cli("note", "rm", "999999")
        self.assertNotEqual(0, rc)
        self.assertNotIn("removed note", out)
        self.assertIn("error:", err)

    def test_non_numeric_id_is_a_clean_error_not_a_traceback(self):
        rc, _out, err = self.run_cli("note", "rm", "PathCtl_ResteerTowardGoal")
        self.assertNotEqual(0, rc)
        self.assertIn("error:", err)


class SiblingSubcommandResolutionTests(unittest.TestCase):
    """The mirror-image trap: an agent who learns one spelling from `note` applies it
    to a sibling subcommand that resolved targets differently. These pin the sibling
    behaviours down so the forms cannot silently drift apart again."""

    def setUp(self):
        fd, self.db = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(self.db)
        self.dump = tempfile.NamedTemporaryFile(
            "w", suffix=".txt", delete=False, encoding="utf-8")
        self.dump.write(
            "0086cf50  void __thiscall sub_86cf50(int this)\n"
            "0086cf59  {\n"
            "0086cf70    sub_879250(this);\n"
            "0086d124  }\n"
            "00877b00  void __thiscall sub_8779b0(int this)\n"
            "00877b56    sub_86cf50(this);\n"
            "00877b7b  }\n"
            # AGENT-AI: data_f0673f added to the fixture. The rename guard now asks
            # whether the key OCCURS IN THE DUMP rather than whether it is merely
            # SHAPED like a token, because 85 keys in the live store are well-formed
            # and absent (78 data_, 7 sub_) and the shape rule passed them silently.
            # This fixture previously modelled a data_ symbol that does not exist,
            # which is the case that SHOULD warn. data_f0673f is present in the real
            # dump (verified), so putting it here makes the fixture match the
            # situation the test is actually about: a LEGITIMATE data_ rename.
            "00f0673f  float data_f0673f = 0.0\n")
        self.dump.close()
        self.con = revtool.open_db(self.db)
        add_func(self.con, "0086cf50", "sub_86cf50")
        add_rename(self.con, "sub_86cf50", "PathCtl_ResteerTowardGoal")
        add_func(self.con, "008779b0", "sub_8779b0")
        # annotated so the xref does not incur investigation debt in the ledger
        add_note(self.con, "sub_86cf50", "annotated so xref costs no debt")
        add_note(self.con, "sub_8779b0", "annotated too")
        self.con.close()

    def tearDown(self):
        for p in (self.dump.name,) + tuple(
                self.db + s for s in ("", "-wal", "-shm")):
            if os.path.exists(p):
                try:
                    os.unlink(p)
                except OSError:
                    pass

    def run_cli(self, *argv):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            rc = revtool.main(["--db", self.db, "--dump", self.dump.name, *argv])
        return rc, out.getvalue(), err.getvalue()

    def test_xref_accepts_an_address_like_view_does(self):
        """`xref 0x86cf50` scanned for the literal text and reported 0 line(s)."""
        rc, out, _err = self.run_cli("xref", "0x86cf50")
        self.assertEqual(0, rc)
        self.assertIn("sub_86cf50", out)
        self.assertNotIn("xref 0x86cf50: 0 line(s)", out)

    def test_xref_by_friendly_name_still_resolves(self):
        _rc, out, _err = self.run_cli("xref", "PathCtl_ResteerTowardGoal")
        self.assertIn("sub_86cf50", out)

    def test_xref_zero_hits_on_unknown_symbol_says_it_is_not_evidence(self):
        _rc, out, err = self.run_cli("xref", "sub_999999")
        self.assertIn("0 line(s)", out)
        self.assertIn("not evidence", err.lower())

    def test_rename_global_warns_when_the_key_is_not_a_dump_token(self):
        """An address-keyed rename can never fire, but still printed success."""
        rc, out, err = self.run_cli(
            "rename", "global", "0x86cf50", "SomeName", "--tier", "guess")
        self.assertEqual(0, rc)
        self.assertIn("global:", out)
        self.assertIn("stored for address-based dossiers/planners", err)
        self.assertIn("will not appear", err)
        self.assertIn("sub_86cf50", err, "the warning should name the real token")

    def test_rename_global_warns_when_renaming_an_already_applied_name(self):
        rc, _out, err = self.run_cli(
            "rename", "global", "PathCtl_ResteerTowardGoal", "PathCtl_Renamed",
            "--tier", "guess")
        self.assertEqual(0, rc)
        self.assertIn("APPLIED name", err)

    def test_rename_global_on_a_real_token_is_silent(self):
        rc, _out, err = self.run_cli(
            "rename", "global", "sub_8779b0", "Overlay_Name", "--tier", "guess")
        self.assertEqual(0, rc)
        self.assertNotIn("warning:", err)

    def test_rename_global_on_a_data_token_is_silent(self):
        """`functions` indexes functions only, so data_ symbols are absent from it by
        design. 727 live rename rows are data_; warning on them would be cry-wolf.

        Verified against the live store after the guard was tightened to a
        presence test: 649 data_ renames whose token is in the dump stay silent and
        ZERO are warned about despite being present (_ai_guardcheck.py)."""
        rc, _out, err = self.run_cli(
            "rename", "global", "data_f0673f", "g_fogStartNear", "--tier", "guess")
        self.assertEqual(0, rc)
        self.assertNotIn("warning:", err)

    def test_rename_global_on_a_data_token_absent_from_the_dump_warns(self):
        """The other half, which the shape-only rule could not see: a well-formed
        data_ key that does not occur in the dump can never fire either. 78 such
        rows exist in the live store and the guard was silent for every one."""
        rc, _out, err = self.run_cli(
            "rename", "global", "data_deadbe", "g_notReal", "--tier", "guess")
        self.assertEqual(0, rc)
        self.assertIn("does not occur in the dump", err)

    def test_seen_warns_on_an_unknown_symbol(self):
        rc, out, err = self.run_cli("seen", "sub_999999", "a plausible hypothesis")
        self.assertEqual(0, rc, "seen must still record; it is a debt-discharge path")
        self.assertIn("triaged", out)
        self.assertIn("sub_999999", err)

    def test_seen_on_a_known_symbol_is_silent(self):
        rc, _out, err = self.run_cli("seen", "sub_86cf50", "a plausible hypothesis")
        self.assertEqual(0, rc)
        self.assertNotIn("warning:", err)


class ClaimHistoryResolutionTests(unittest.TestCase):
    """`history` is the audit trail whose whole purpose is that a prior claim is never
    silently lost - yet an address-form target fell through to a raw ckey lookup and
    printed "(none)" with exit 0, which is the same false negative as `note list`."""

    def setUp(self):
        fd, self.db = tempfile.mkstemp(suffix=".db")
        os.close(fd)
        os.unlink(self.db)
        self.con = revtool.open_db(self.db)
        add_func(self.con, "0086cf50", "sub_86cf50")
        add_rename(self.con, "sub_86cf50", "PathCtl_ResteerTowardGoal")
        self.con.execute(
            "INSERT INTO history(ts,author,kind,ckey,action,old,new,reason) "
            "VALUES(?,?,?,?,?,?,?,NULL)",
            (revtool.now_iso(), "agent-a", "global", "global:sub_86cf50", "set",
             None, "PathCtl_ResteerTowardGoal"))
        self.con.commit()
        self.con.close()

    def tearDown(self):
        for suffix in ("", "-wal", "-shm"):
            p = self.db + suffix
            if os.path.exists(p):
                try:
                    os.unlink(p)
                except OSError:
                    pass

    def run_cli(self, *argv):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            rc = revtool.main(["--db", self.db, *argv])
        return rc, out.getvalue(), err.getvalue()

    def test_history_by_sub_token_and_friendly_name_agree(self):
        _rc, out_sub, _e = self.run_cli("history", "sub_86cf50")
        _rc, out_fri, _e = self.run_cli("history", "PathCtl_ResteerTowardGoal")
        self.assertIn("PathCtl_ResteerTowardGoal", out_sub)
        self.assertEqual(out_sub, out_fri)

    def test_history_by_address_finds_the_same_trail(self):
        """`history 0x86cf50` printed '(none)' and exited 0 for a claim WITH history."""
        rc, out, _err = self.run_cli("history", "0x86cf50")
        self.assertEqual(0, rc)
        self.assertNotIn("(none)", out)
        self.assertIn("PathCtl_ResteerTowardGoal", out)

    def test_history_for_an_unknown_symbol_is_loud_not_an_empty_trail(self):
        rc, out, err = self.run_cli("history", "sub_999999")
        self.assertNotEqual(0, rc)
        self.assertIn("error:", err)
        self.assertNotIn("PathCtl", out)

    def test_history_with_no_target_still_lists_recent_activity(self):
        rc, out, _err = self.run_cli("history")
        self.assertEqual(0, rc)
        self.assertIn("recent activity", out)

    def test_mark_accepts_an_address_like_view_does(self):
        rc, out, err = self.run_cli("mark", "0x86cf50", "verified")
        self.assertEqual(0, rc, f"mark by address failed: {err}")
        self.assertIn("sub_86cf50", out)


if __name__ == "__main__":
    unittest.main()
