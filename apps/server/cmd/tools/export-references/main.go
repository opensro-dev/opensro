/*
===========================================================================

main.go - exports the browser references document (refSkillSnapshot)

Tool to export the runtime public browser references document containing refSkillSnapshot.

===========================================================================
*/
package main

import (
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"

	"opensro.online/server/internal/game/enterworld"
)

func main() {
	textdataDir := flag.String("textdata", "../../../.generated/game-data/1.150/server/textdata", "Verified server textdata directory")
	outDir := flag.String("out", "../../../../../.generated/transport-references", "Output directory for reference document")
	flag.Parse()

	source := enterworld.NewTextdataSkills(*textdataDir)
	items := enterworld.NewTextdataItems(*textdataDir)
	refs, err := enterworld.NewBrowserReferences(source, items)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating browser references: %v\n", err)
		os.Exit(1)
	}

	req := httptest.NewRequest("GET", refs.Path, nil)
	rec := httptest.NewRecorder()
	refs.ServeHTTP(rec, req)

	if rec.Code != 200 {
		fmt.Fprintf(os.Stderr, "Failed to serve references HTTP: %d\n", rec.Code)
		os.Exit(1)
	}

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output dir: %v\n", err)
		os.Exit(1)
	}

	targetPath := filepath.Join(*outDir, refs.SHA256+".json")
	if err := os.WriteFile(targetPath, rec.Body.Bytes(), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing reference file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully exported runtime references document:\n")
	fmt.Printf("  Path:   %s\n", refs.Path)
	fmt.Printf("  SHA256: %s\n", refs.SHA256)
	fmt.Printf("  Bytes:  %d\n", refs.Bytes)
	fmt.Printf("  File:   %s\n", targetPath)
}
