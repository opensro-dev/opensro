package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestStoreDoesNotImportGameBootstrap(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, spec := range file.Decls {
			decl, ok := spec.(*ast.GenDecl)
			if !ok || decl.Tok != token.IMPORT {
				continue
			}
			for _, raw := range decl.Specs {
				importSpec := raw.(*ast.ImportSpec)
				path, err := strconv.Unquote(importSpec.Path.Value)
				if err != nil {
					t.Fatalf("unquote import in %s: %v", entry.Name(), err)
				}
				if path == "opensro.online/server/internal/game/enterworld" {
					t.Fatalf("%s imports %s; persisted records and store ports belong in domain", entry.Name(), path)
				}
			}
		}
	}
}
