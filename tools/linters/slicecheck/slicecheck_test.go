// Copyright 2026 Democratized Data Foundation
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package slicecheck

import (
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

func TestLinter_AppendDetection(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected []string
	}{
		{
			name: "detects append to non-zero length slice",
			code: `
package test

func foo() {
	x := make([]int, 5)
	x = append(x, 1)
}`,
			expected: []string{
				"append to slice `x` with non-zero initialized length at test.go:6:6",
			},
		},
		{
			name: "ignores append to zero length slice",
			code: `
package test

func foo() {
	x := make([]int, 0, 5)
	x = append(x, 1)
}`,
			expected: nil,
		},
		{
			name: "ignores index assignment",
			code: `
package test

func foo() {
	x := make([]int, 5)
	x[0] = 1
}`,
			expected: nil,
		},
		{
			name: "detects with custom slice type",
			code: `
package test

type intSlice []int

func foo() {
	x := make(intSlice, 5)
	x = append(x, 1)
}`,
			expected: []string{
				"append to slice `x` with non-zero initialized length at test.go:8:6",
			},
		},
		{
			name: "ignores maps",
			code: `
package test

func foo() {
	x := make(map[string]int, 5)
	x["key"] = 1
}`,
			expected: nil,
		},
		{
			name: "handles multi-declaration",
			code: `
package test

func foo() {
	a, b := make([]int, 10), make([]int, 0)
	a = append(a, 1)
	b = append(b, 1)
}`,
			expected: []string{
				"append to slice `a` with non-zero initialized length at test.go:6:6",
			},
		},
		{
			name: "handles multiple violations",
			code: `
package test

func foo() {
	a := make([]int, 5)
	b := make([]int, 10)
	a = append(a, 1)
	b = append(b, 2)
}`,
			expected: []string{
				"append to slice `a` with non-zero initialized length at test.go:7:6",
				"append to slice `b` with non-zero initialized length at test.go:8:6",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linter := NewLinter(Config{StrictMode: false})
			issues := parseAndRun(t, linter, tt.code)

			var issueStrs []string
			for _, i := range issues {
				issueStrs = append(issueStrs, i.String())
			}

			if !reflect.DeepEqual(tt.expected, issueStrs) {
				t.Errorf("\nExpected: %v\nGot:      %v", tt.expected, issueStrs)
			}
		})
	}
}

func TestLinter_StrictMode(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected []string
	}{
		{
			name: "reports non-zero initialization in strict mode",
			code: `
package test

func foo() {
	x := make([]int, 5)
}`,
			expected: []string{
				"slice `x` initialized with non-zero length at test.go:5:2",
			},
		},
		{
			name: "ignores zero initialization in strict mode",
			code: `
package test

func foo() {
	x := make([]int, 0, 5)
	x = append(x, 1)
}`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linter := NewLinter(Config{StrictMode: true})
			issues := parseAndRun(t, linter, tt.code)

			var issueStrs []string
			for _, i := range issues {
				issueStrs = append(issueStrs, i.String())
			}

			if !reflect.DeepEqual(tt.expected, issueStrs) {
				t.Errorf("\nExpected: %v\nGot:      %v", tt.expected, issueStrs)
			}
		})
	}
}

func TestLinter_NoLintDirective(t *testing.T) {
	// Note: nolint comments on the append line should suppress the warning
	code := `
package test

func foo() {
	x := make([]int, 5)
	x = append(x, 1) // nolint:slicecheck
	y := make([]int, 5)
	y = append(y, 1)
}`

	linter := NewLinter(Config{StrictMode: false})
	issues := parseAndRun(t, linter, code)

	// Only y should be reported (x's append has nolint)
	if len(issues) != 1 {
		t.Errorf("Expected 1 issue, got %d: %v", len(issues), issues)
		return
	}
	appendIssue, ok := issues[0].(AppendIssue)
	if !ok {
		t.Errorf("Expected AppendIssue, got %T", issues[0])
		return
	}
	if appendIssue.name != "y" {
		t.Errorf("Expected issue for 'y', got '%s'", appendIssue.name)
	}
}

func parseAndRun(t *testing.T, linter *Linter, code string) []Issue {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", code, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse code: %v", err)
	}

	issues, err := linter.Run(fset, nil, file)
	if err != nil {
		t.Fatalf("Linter failed: %v", err)
	}
	return issues
}
