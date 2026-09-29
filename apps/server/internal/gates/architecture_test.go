/*
===========================================================================

architecture_test.go - package boundary gates

architecture_test.go pins dependency direction at the filesystem boundary.

===========================================================================
*/
package gates

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

/*
================================================================================
Core package boundaries

	domain and worldarea are dependency-free record/policy leaves. Character
	vitals derivation is isolated in a semantic domain subpackage instead of
	turning persisted records into a gameplay-rule container. Auth owns
account catalogs and player/session tokens; workload identity is an isolated
control-plane concern. Store owns persistence and may consume domain records,
but never gameplay implementations. The item wire package may encode domain
values but never depend on gameplay implementations. Transport is protocol
infrastructure and never knows game lanes; its worldsession subpackage is the
outer adapter that joins transport to simulation. Agent API is an outer adapter
over auth, domain, store, and the dependency-free readiness gate. The
privatepath leaf is shared only by the two packages that own service-private
files: store and transport.
================================================================================
*/

func TestCorePackageBoundaries(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatalf("architecture gate: locate module root: %v", err)
	}
	modulePath := modulePathFromGoMod(t, root)

	checkPackageImports(t, root, modulePath, "internal/domain", nil, false)
	checkPackageImports(t, root, modulePath, "internal/domain/charactervitals", map[string]bool{
		modulePath + "/internal/domain": true,
	}, false)
	checkPackageImports(t, root, modulePath, "internal/config", nil, true)
	checkPackageImports(t, root, modulePath, "internal/game/world/worldarea", nil, false)
	// Leaf rule engines monster instances carry: the textdata cell splitter,
	// the paramkeeper channel model and the abnormal-status engine over it.
	checkPackageImports(t, root, modulePath, "internal/data/texttable", nil, false)
	checkPackageImports(t, root, modulePath, "internal/game/paramkeeper", nil, false)
	checkPackageImports(t, root, modulePath, "internal/game/abnormal", map[string]bool{
		modulePath + "/internal/game/paramkeeper": true,
	}, false)
	checkPackageImports(t, root, modulePath, "internal/game/world/monster", map[string]bool{
		modulePath + "/internal/domain":           true,
		modulePath + "/internal/game/world":       true,
		modulePath + "/internal/data/texttable":   true,
		modulePath + "/internal/game/abnormal":    true,
		modulePath + "/internal/game/paramkeeper": true,
	}, false)
	checkPackageImports(t, root, modulePath, "internal/game/item/wire", map[string]bool{
		modulePath + "/internal/domain": true,
	}, true)
	checkPackageImports(t, root, modulePath, "internal/security/auth", map[string]bool{
		modulePath + "/internal/domain": true,
	}, true)
	checkPackageImports(t, root, modulePath, "internal/security/workload", nil, true)
	checkPackageImports(t, root, modulePath, "internal/platform/readiness", nil, false)
	checkPackageImports(t, root, modulePath, "internal/data/store", map[string]bool{
		modulePath + "/internal/domain":                 true,
		modulePath + "/internal/domain/charactervitals": true,
		modulePath + "/internal/platform/privatepath":   true,
	}, true)
	checkPackageImports(t, root, modulePath, "internal/transport", map[string]bool{
		modulePath + "/internal/platform/privatepath": true,
	}, true)
	checkPackageImports(t, root, modulePath, "internal/agent/api", map[string]bool{
		modulePath + "/internal/domain":                 true,
		modulePath + "/internal/domain/charactervitals": true,
		modulePath + "/internal/security/auth":          true,
		modulePath + "/internal/platform/readiness":     true,
		modulePath + "/internal/data/store":             true,
		modulePath + "/internal/game/world/worldarea":   true,
		modulePath + "/internal/releaseprotocol":        true,
	}, true)
	// The release protocol owns the browser wire contract versions; every
	// owner imports it, so it imports nothing from the module.
	checkPackageImports(t, root, modulePath, "internal/releaseprotocol", nil, false)
}

/*
==================
TestMonsterPolicyDoesNotReclaimWorldState

TestMonsterPolicyDoesNotReclaimWorldState keeps the ownership split
executable: monster is immutable catalog/policy, while simulation owns live
identities, HP, respawns, and mover commits.
==================
*/
func TestMonsterPolicyDoesNotReclaimWorldState(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatalf("architecture gate: locate module root: %v", err)
	}
	dir := filepath.Join(root, "internal", "game", "world", "monster")
	forbiddenTypes := map[string]bool{
		"Registry": true, "MonsterState": true, "divisionPopulation": true,
	}
	forbiddenFuncs := map[string]bool{
		"ApplyDamage": true, "Defeat": true, "CommitMover": true,
		"ArmRetaliation": true, "InstancesInRegions": true,
		"MaterializedInstances": true,
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("architecture gate: read monster package: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			t.Fatalf("architecture gate: parse %s: %v", path, parseErr)
		}
		for _, declaration := range file.Decls {
			switch node := declaration.(type) {
			case *ast.GenDecl:
				if node.Tok != token.TYPE {
					continue
				}
				for _, spec := range node.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if ok && forbiddenTypes[typeSpec.Name.Name] {
						t.Errorf("%s declares %s; mutable monster runtime state belongs in simulation", entry.Name(), typeSpec.Name.Name)
					}
				}
			case *ast.FuncDecl:
				if forbiddenFuncs[node.Name.Name] {
					t.Errorf("%s declares %s; live monster mutation belongs on simulation.MonsterState", entry.Name(), node.Name.Name)
				}
			}
		}
	}
}

/*
==================
TestGameplayNeverImportsInfrastructure

TestGameplayNeverImportsInfrastructure keeps store and HTTP adapters behind
consumer-owned ports. The composition root is the only place allowed to
join gameplay to concrete infrastructure.
==================
*/
func TestGameplayNeverImportsInfrastructure(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatalf("architecture gate: locate module root: %v", err)
	}
	modulePath := modulePathFromGoMod(t, root)
	forbidden := []string{
		modulePath + "/internal/agent/api",
		modulePath + "/internal/config",
		modulePath + "/internal/platform/logging",
		modulePath + "/internal/data/store",
	}

	err = filepath.WalkDir(filepath.Join(root, "internal", "game"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, importPath := range parsedImports(t, path) {
			for _, prefix := range forbidden {
				if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
					relative, _ := filepath.Rel(root, path)
					t.Errorf("%s imports infrastructure package %s; inject a consumer-owned port from the composition root", relative, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("architecture gate: walk game packages: %v", err)
	}
}

/*
==================
TestGameplayConsumesEnterWorldThroughPorts

TestGameplayConsumesEnterWorldThroughPorts prevents enterworld.Deps from
becoming a cross-package service locator again. EnterWorld owns the concrete
initial-state composition object; every other game lane declares the narrow
interface it consumes and the root supplies the implementation.
==================
*/
func TestGameplayConsumesEnterWorldThroughPorts(t *testing.T) {
	root, err := ModuleRoot()
	if err != nil {
		t.Fatalf("architecture gate: locate module root: %v", err)
	}
	modulePath := modulePathFromGoMod(t, root)
	enterWorldPath := modulePath + "/internal/game/enterworld"
	gameRoot := filepath.Join(root, "internal", "game")

	err = filepath.WalkDir(gameRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == filepath.Join(gameRoot, "enterworld") {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		aliases := make(map[string]bool)
		for _, spec := range file.Imports {
			importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil || importPath != enterWorldPath {
				continue
			}
			alias := "enterworld"
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias == "." {
				relative, _ := filepath.Rel(root, path)
				t.Errorf("%s dot-imports enterworld; declare a consumer-owned port", relative)
				continue
			}
			aliases[alias] = true
		}

		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Deps" {
				return true
			}
			qualifier, ok := selector.X.(*ast.Ident)
			if !ok || !aliases[qualifier.Name] {
				return true
			}
			relative, _ := filepath.Rel(root, path)
			t.Errorf("%s uses concrete enterworld.Deps; consume a package-owned interface", relative)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("architecture gate: walk gameplay packages: %v", err)
	}
}

func checkPackageImports(
	t *testing.T,
	root string,
	modulePath string,
	relativeDir string,
	allowedInternal map[string]bool,
	allowThirdParty bool,
) {
	t.Helper()
	dir := filepath.Join(root, relativeDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("architecture gate: read %s: %v", relativeDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		for _, importPath := range parsedImports(t, path) {
			if importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/") {
				if !allowedInternal[importPath] {
					t.Errorf("%s imports internal package %s outside its allowed boundary", filepath.Join(relativeDir, entry.Name()), importPath)
				}
				continue
			}
			if !allowThirdParty && isThirdPartyImport(importPath) {
				t.Errorf("%s imports third-party package %s; this package must remain standard-library only", filepath.Join(relativeDir, entry.Name()), importPath)
			}
		}
	}
}

func parsedImports(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("architecture gate: parse %s: %v", path, err)
	}
	imports := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("architecture gate: unquote import %s in %s: %v", spec.Path.Value, path, err)
		}
		imports = append(imports, importPath)
	}
	return imports
}

func isThirdPartyImport(importPath string) bool {
	first := importPath
	if slash := strings.IndexByte(first, '/'); slash >= 0 {
		first = first[:slash]
	}
	return strings.Contains(first, ".")
}
