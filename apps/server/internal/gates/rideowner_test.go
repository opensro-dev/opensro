/*
===========================================================================

rideowner_test.go - a companion's ride state has one writer

pet.Mounted decides which world poses a vehicle: the rider's while it
is set, the companion's own mover (simulation.CompanionMover) while it
is clear. Flipping it is therefore a change of pose owner, and every
flip that did not carry the pose across left the vehicle at a stale
spot (PR #3464: a ridden transport's cargo dropped at the specialty
shop). action/cosmount.go is the one place that flips it, handing the
pose over; this gate refuses an assignment to a Mounted field anywhere
else in the module's production code. Tests may set it to build state.

===========================================================================
*/
package gates

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// rideStateOwner is the module-relative file allowed to write Mounted.
const rideStateOwner = "internal/game/action/cosmount.go"

/*
================
TestRideStateHasOneWriter
================
*/
func TestRideStateHasOneWriter(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatalf("ride owner: locate module root: %v", err)
	}
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		if relative == rideStateOwner {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Errorf("ride owner: parse %s: %v", relative, parseErr)
			return nil
		}
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, target := range assign.Lhs {
				if field, ok := target.(*ast.SelectorExpr); ok && field.Sel.Name == "Mounted" {
					t.Errorf("%s: assigns Mounted; flip the ride through mountCompanion / unmountCompanion (%s)",
						fset.Position(assign.Pos()), rideStateOwner)
				}
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("ride owner: walk: %v", walkErr)
	}
}
