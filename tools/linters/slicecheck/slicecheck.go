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
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"regexp"
)

// uniqDecl uniquely identifies a variable declaration.
// A single declaration might include multiple variables,
// so we combine the variable name with its declaration.
type uniqDecl struct {
	varName string
	decl    any
}

// Issue represents a linting issue found by the checker.
// This interface allows for easy extension with new issue types.
type Issue interface {
	Details() string
	Pos() token.Pos
	Position() token.Position
	String() string
}

// baseIssue contains common fields for all issue types.
// Embed this in concrete issue types to reduce boilerplate.
type baseIssue struct {
	name     string
	pos      token.Pos
	position token.Position
}

// Pos returns the token position of the issue.
func (b baseIssue) Pos() token.Pos {
	return b.pos
}

// Position returns the file position of the issue.
func (b baseIssue) Position() token.Position {
	return b.position
}

// AppendIssue is reported when append is called on a slice
// that was initialized with non-zero length.
type AppendIssue struct {
	baseIssue
}

// Details returns a human-readable description of the issue.
func (a AppendIssue) Details() string {
	return fmt.Sprintf(errAppendToNonZeroSlice, a.name)
}

// String returns the full issue description with location.
func (a AppendIssue) String() string {
	return toString(a)
}

// NonZeroInitIssue is reported when a slice is initialized with
// non-zero length (only when strict mode is enabled).
type NonZeroInitIssue struct {
	baseIssue
}

// Details returns a human-readable description of the issue.
func (i NonZeroInitIssue) Details() string {
	return fmt.Sprintf(errNonZeroSliceInit, i.name)
}

// String returns the full issue description with location.
func (i NonZeroInitIssue) String() string {
	return toString(i)
}

func toString(i Issue) string {
	return fmt.Sprintf("%s at %s", i.Details(), i.Position())
}

// Config holds the linter configuration.
// Add new configuration options here as needed.
type Config struct {
	// StrictMode when true bans ALL make([]T, n) where n > 0.
	// When false, only flags slices that are later appended to.
	StrictMode bool
}

// Linter is the main entry point for the slice checker.
// It provides a clean API for running the linter programmatically.
type Linter struct {
	config Config
}

// NewLinter creates a new Linter with the given configuration.
func NewLinter(cfg Config) *Linter {
	return &Linter{
		config: cfg,
	}
}

// Run executes the linter on the given AST nodes.
// It returns all issues found, or an error if the linting process failed.
func (l *Linter) Run(fset *token.FileSet, info *types.Info, nodes ...ast.Node) ([]Issue, error) {
	var issues []Issue
	for _, node := range nodes {
		v := newVisitor(fset, info, l.config.StrictMode)
		if file, ok := node.(*ast.File); ok {
			v.comments = file.Comments
		}
		ast.Walk(v, node)
		issues = append(issues, v.issues...)
	}
	return issues, nil
}

// visitor walks the AST and collects issues.
type visitor struct {
	strictMode bool
	comments   []*ast.CommentGroup
	info       *types.Info
	fset       *token.FileSet

	// nonZeroLengthSliceDecls tracks slices initialized with non-zero length.
	// This is used to detect when append is called on such slices.
	nonZeroLengthSliceDecls map[uniqDecl]struct{}

	// issues collects all linting issues found during the walk.
	issues []Issue
}

// newVisitor creates a new visitor with initialized state.
func newVisitor(fset *token.FileSet, info *types.Info, strictMode bool) *visitor {
	return &visitor{
		nonZeroLengthSliceDecls: make(map[uniqDecl]struct{}),
		strictMode:              strictMode,
		info:                    info,
		fset:                    fset,
	}
}

// Visit implements ast.Visitor.
// It dispatches to specialized check functions based on node type.
func (v *visitor) Visit(node ast.Node) ast.Visitor {
	switch node := node.(type) {
	case *ast.CallExpr:
		v.checkAppendCall(node)
	case *ast.AssignStmt:
		v.checkMakeCall(node)
	}
	return v
}

// Built-in function names that this linter checks.
const (
	builtinAppend = "append"
	builtinMake   = "make"
)

// checkAppendCall checks if append is called on a slice with non-zero initial length.
func (v *visitor) checkAppendCall(node *ast.CallExpr) {
	fun, ok := node.Fun.(*ast.Ident)
	if !ok || fun.Name != builtinAppend {
		return
	}
	if len(node.Args) == 0 {
		return
	}

	sliceIdent, ok := node.Args[0].(*ast.Ident)
	if !ok {
		return
	}

	if v.hasNonZeroInitialLength(sliceIdent) && !v.hasNoLintComment(fun) {
		v.issues = append(v.issues, AppendIssue{
			baseIssue: baseIssue{
				name:     sliceIdent.Name,
				pos:      fun.Pos(),
				position: v.fset.Position(fun.Pos()),
			},
		})
	}
}

// checkMakeCall checks for make calls that create slices with non-zero length.
func (v *visitor) checkMakeCall(node *ast.AssignStmt) {
	for i, right := range node.Rhs {
		call, ok := right.(*ast.CallExpr)
		if !ok {
			continue
		}

		fun, ok := call.Fun.(*ast.Ident)
		if !ok || fun.Name != builtinMake {
			continue
		}

		if len(call.Args) < 2 {
			continue
		}

		if i >= len(node.Lhs) {
			continue
		}
		left := node.Lhs[i]

		// Check if it's a slice (not a map or channel)
		if !v.isSlice(call.Args[0]) {
			continue
		}

		// Check if length is explicitly zero
		if v.isZeroLiteral(call.Args[1]) {
			continue
		}

		// In strict mode, report any non-zero initialization
		if v.strictMode && !v.hasNoLintComment(fun) {
			v.issues = append(v.issues, NonZeroInitIssue{
				baseIssue: baseIssue{
					name:     v.textFor(left),
					pos:      node.Pos(),
					position: v.fset.Position(node.Pos()),
				},
			})
		}

		// Always record for append checking
		v.recordNonZeroLengthSlice(left)
	}
}

// textFor returns the source text for an AST node.
func (v *visitor) textFor(node ast.Node) string {
	buf := new(bytes.Buffer)
	if err := printer.Fprint(buf, v.fset, node); err != nil {
		return "<unknown>"
	}
	return buf.String()
}

// hasNonZeroInitialLength checks if an identifier refers to a slice
// that was initialized with non-zero length.
func (v *visitor) hasNonZeroInitialLength(ident *ast.Ident) bool {
	if ident.Obj == nil {
		return false
	}
	_, exists := v.nonZeroLengthSliceDecls[uniqDecl{
		varName: ident.Obj.Name,
		decl:    ident.Obj.Decl,
	}]
	return exists
}

// recordNonZeroLengthSlice records a slice that was initialized with non-zero length.
func (v *visitor) recordNonZeroLengthSlice(node ast.Node) {
	ident, ok := node.(*ast.Ident)
	if !ok || ident.Obj == nil {
		return
	}
	v.nonZeroLengthSliceDecls[uniqDecl{
		varName: ident.Obj.Name,
		decl:    ident.Obj.Decl,
	}] = struct{}{}
}

// isSlice determines if an AST node represents a slice type.
func (v *visitor) isSlice(node ast.Node) bool {
	// Check if it's a user-defined type that resolves to a slice
	if ident, ok := node.(*ast.Ident); ok {
		obj := ident.Obj
		if obj == nil {
			if v.info != nil {
				_, ok := v.info.ObjectOf(ident).Type().(*types.Slice)
				return ok
			}
			return false
		}
		spec, ok := obj.Decl.(*ast.TypeSpec)
		if !ok {
			return false
		}
		node = spec.Type
	}

	// Check if it's an array type with no length (i.e., a slice)
	if arr, ok := node.(*ast.ArrayType); ok {
		return arr.Len == nil
	}
	return false
}

// isZeroLiteral checks if an expression is the literal 0.
func (v *visitor) isZeroLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "0"
}

// noLintPattern matches the slicecheck ignore directive.
// Supports: nolint:slicecheck and slicecheck:ignore
var noLintPattern = regexp.MustCompile(`^\s*(nolint:slicecheck|slicecheck:ignore)\b`)

// hasNoLintComment checks if there's a nolint directive on the same line.
func (v *visitor) hasNoLintComment(node ast.Node) bool {
	nodePos := v.fset.Position(node.Pos())
	for _, c := range v.comments {
		commentPos := v.fset.Position(c.Pos())
		if commentPos.Line == nodePos.Line && noLintPattern.MatchString(c.Text()) {
			return true
		}
	}
	return false
}
