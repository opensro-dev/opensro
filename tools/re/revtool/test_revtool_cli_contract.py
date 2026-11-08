"""User-facing Revtool CLI contracts."""

from __future__ import annotations

import sys
import re
import unittest
from pathlib import Path


TOOLS = Path(__file__).parent
sys.path.insert(0, str(TOOLS))

import revtool  # noqa: E402


class RevtoolHelpContractTests(unittest.TestCase):
    def test_top_level_help_renders_literal_coverage_percentage(self) -> None:
        rendered = revtool.build_parser().format_help()
        normalized = re.sub(r"\s+", " ", rendered)
        self.assertIn("(% refs labeled + depth)", normalized)
        self.assertNotIn("'option_strings'", rendered)


if __name__ == "__main__":
    unittest.main()
