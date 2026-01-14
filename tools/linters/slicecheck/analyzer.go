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
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer is the go/analysis compatible analyzer for slicecheck.
// This enables integration with golangci-lint and other go/analysis tools.
var Analyzer = &analysis.Analyzer{
	Name:     "slicecheck",
	Doc:      Doc,
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

// Doc is the documentation string for the analyzer.
const Doc = `check for append to slices initialized with non-zero length

This linter detects a common Go bug where a slice is created with
make([]T, n) (non-zero length) and then append() is called on it.

Example of the bug:
	values := make([]int, len(nums))
	for _, n := range nums {
	    values = append(values, n) // BUG: appends after the zeros!
	}

The correct patterns are:
	// Use zero length with capacity:
	values := make([]int, 0, len(nums))

	// Or use index assignment:
	values := make([]int, len(nums))
	for i, n := range nums {
	    values[i] = n
	}`

// StrictMode controls whether the analyzer should flag all make([]T, n)
// calls where n > 0, or only those that are followed by append.
var StrictMode bool

func run(pass *analysis.Pass) (any, error) {
	insp, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return nil, nil
	}

	// Track slices initialized with non-zero length
	nonZeroSlices := make(map[*ast.Object]bool)

	// First pass: find all make calls with non-zero length
	nodeFilter := []ast.Node{
		(*ast.AssignStmt)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return
		}

		for i, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				continue
			}

			fun, ok := call.Fun.(*ast.Ident)
			if !ok || fun.Name != "make" {
				continue
			}

			if len(call.Args) < 2 {
				continue
			}

			// Check if it's a slice type
			if !isSliceType(call.Args[0], pass) {
				continue
			}

			// Check if length is explicitly zero
			if isZeroLiteral(call.Args[1]) {
				continue
			}

			// Get the left-hand side identifier
			if i >= len(assign.Lhs) {
				continue
			}
			lhs, ok := assign.Lhs[i].(*ast.Ident)
			if !ok || lhs.Obj == nil {
				continue
			}

			// In strict mode, report immediately
			if StrictMode {
				pass.Reportf(assign.Pos(), "slice `%s` initialized with non-zero length", lhs.Name)
			}

			// Record for append checking
			nonZeroSlices[lhs.Obj] = true
		}
	})

	// Second pass: find appends to non-zero slices
	callFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	insp.Preorder(callFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}

		fun, ok := call.Fun.(*ast.Ident)
		if !ok || fun.Name != "append" {
			return
		}

		if len(call.Args) == 0 {
			return
		}

		sliceArg, ok := call.Args[0].(*ast.Ident)
		if !ok || sliceArg.Obj == nil {
			return
		}

		if nonZeroSlices[sliceArg.Obj] {
			pass.Reportf(call.Pos(), "append to slice `%s` with non-zero initialized length", sliceArg.Name)
		}
	})

	return nil, nil
}

// isSliceType checks if an expression represents a slice type.
func isSliceType(expr ast.Expr, pass *analysis.Pass) bool {
	switch e := expr.(type) {
	case *ast.ArrayType:
		return e.Len == nil // Slice has no length
	case *ast.Ident:
		// Could be a type alias - check type info
		if pass.TypesInfo != nil {
			obj := pass.TypesInfo.ObjectOf(e)
			if obj != nil {
				if _, ok := obj.Type().Underlying().(*types.Slice); ok {
					return true
				}
			}
		}
	}
	return false
}

// isZeroLiteral checks if an expression is the literal 0.
func isZeroLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Value == "0"
}
