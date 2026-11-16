/*
===========================================================================

rideowner_test.go - a companion's ride state has one writer

pet.Mounted decides which world poses a vehicle: the rider's while it
is set, the companion's own mover (simulation.CompanionMover) while it
is clear. Flipping it is therefore a change of pose owner, and every
flip that did not carry the pose across left the vehicle at a stale
spot (PR #3464: a ridden transport's cargo dropped at the specialty
shop). action/cosmount.go is the one place that flips it, handing the
pose over; this gate refuses, anywhere else in the module's production
code (internal and cmd), an assignment to a Mounted field and a
CharacterCOS literal that names Mounted (a record built ridden goes
through rideNewVehicle). Tests may set it to build state. Loading a saved
record decodes the persisted fact by reflection and is no flip.

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
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// testdata and dot folders hold no build's code.
			if name := entry.Name(); path != root && (name == "testdata" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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
			if literal, ok := node.(*ast.CompositeLit); ok {
				for _, key := range mountedKeys(literal) {
					t.Errorf("%s: builds a CharacterCOS with Mounted; set the ride through rideNewVehicle (%s)",
						fset.Position(key.Pos()), rideStateOwner)
				}
				return true
			}
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

/*
================
namesCharacterCOS

Whether a composite literal's type is the companion record,
CharacterCOS by itself or qualified by its package (domain,
enterworld's alias).
================
*/
func namesCharacterCOS(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return namesCharacterCOS(t.X)
	case *ast.Ident:
		return t.Name == "CharacterCOS"
	case *ast.SelectorExpr:
		return t.Sel.Name == "CharacterCOS"
	}
	return false
}

/*
================
mountedKeys

The Mounted keys a composite literal sets on a CharacterCOS: its own
when it is one, and its elements' when it is a slice, array or map of
them whose element literals leave the type out.
================
*/
func mountedKeys(literal *ast.CompositeLit) []*ast.Ident {
	var element ast.Expr
	switch t := literal.Type.(type) {
	case *ast.ArrayType:
		element = t.Elt
	case *ast.MapType:
		element = t.Value
	}
	var keys []*ast.Ident
	if namesCharacterCOS(literal.Type) {
		keys = append(keys, mountedKey(literal)...)
	}
	if element != nil && namesCharacterCOS(element) {
		for _, e := range literal.Elts {
			if pair, ok := e.(*ast.KeyValueExpr); ok {
				e = pair.Value
			}
			if unary, ok := e.(*ast.UnaryExpr); ok {
				e = unary.X
			}
			if inner, ok := e.(*ast.CompositeLit); ok && inner.Type == nil {
				keys = append(keys, mountedKey(inner)...)
			}
		}
	}
	return keys
}

/*
================
mountedKey
================
*/
func mountedKey(literal *ast.CompositeLit) []*ast.Ident {
	for _, e := range literal.Elts {
		if pair, ok := e.(*ast.KeyValueExpr); ok {
			if key, ok := pair.Key.(*ast.Ident); ok && key.Name == "Mounted" {
				return []*ast.Ident{key}
			}
		}
	}
	return nil
}
