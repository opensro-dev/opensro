/*
===========================================================================

withdrawalgold_test.go - pin restoration to the authored drop-gold minimum

The similarly named levelgold table has different values and must not price
resuscitation. Missing or malformed rows must leave the operation unavailable.

===========================================================================
*/
package enterworld

import (
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestWithdrawalGoldBasisUsesDropGoldMinimum
================
*/
func TestWithdrawalGoldBasisUsesDropGoldMinimum(t *testing.T) {
	dir := t.TempDir()
	for name, table := range map[string]string{
		"dg.txt":        "1\t28\t42\n2\t32\t47\n3\tbad\t53\n4\t39\n",
		"levelgold.txt": "1\t999\t1999\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(table), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	levels := NewTextdataLevels(dir)
	for level, expected := range map[int64]int64{1: 28, 2: 32, 3: 0, 4: 0, 5: 0} {
		basis, found := levels.WithdrawalGoldBasis(level)
		if basis != expected || found != (expected != 0) {
			t.Fatalf("level %d: got %d/%v, want %d", level, basis, found, expected)
		}
	}
}
