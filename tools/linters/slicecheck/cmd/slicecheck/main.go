// Copyright 2026 Democratized Data Foundation
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

// slicecheck is a command-line tool for detecting append to slices
// initialized with non-zero length.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"

	"github.com/sourcenetwork/defradb/tools/linters/slicecheck"
)

func main() {
	strict := flag.Bool("strict", false, "ban all make([]T, n) where n > 0")
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "Usage: slicecheck [flags] <packages...>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	cfg := slicecheck.Config{
		StrictMode: *strict,
	}
	linter := slicecheck.NewLinter(cfg)

	hasIssues := false
	for _, path := range flag.Args() {
		if err := processPath(linter, path); err != nil {
			fmt.Fprintf(os.Stderr, "Error processing %s: %v\n", path, err)
			os.Exit(1)
		}
	}

	if hasIssues {
		os.Exit(1)
	}
}

func processPath(linter *slicecheck.Linter, path string) error {
	return filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(filePath) != ".go" {
			return nil
		}
		return processFile(linter, filePath)
	})
}

func processFile(linter *slicecheck.Linter, filePath string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return errors.New("parsing file: " + err.Error())
	}

	issues, err := linter.Run(fset, nil, file)
	if err != nil {
		return errors.New("running linter: " + err.Error())
	}

	for _, issue := range issues {
		_, _ = os.Stdout.WriteString(issue.String() + "\n")
	}
	return nil
}
