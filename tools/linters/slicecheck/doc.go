// Copyright 2026 Democratized Data Foundation
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

// Package slicecheck provides a linter for detecting appends to slices
// initialized with non-zero length.
//
// This linter catches a common Go bug pattern where a slice is created
// with make([]T, n) (non-zero length) and then append() is called on it,
// resulting in unexpected extra elements at the beginning of the slice.
//
// Example of the bug:
//
//	values := make([]int, len(nums))  // Creates [0, 0, 0, ...]
//	for _, n := range nums {
//	    values = append(values, n)    // Appends AFTER the zeros!
//	}
//	// Result: [0, 0, 0, ..., actual values] - NOT what you wanted!
//
// Correct patterns:
//
//	// Option 1: Use zero length with capacity
//	values := make([]int, 0, len(nums))
//	for _, n := range nums {
//	    values = append(values, n)
//	}
//
//	// Option 2: Use index assignment
//	values := make([]int, len(nums))
//	for i, n := range nums {
//	    values[i] = n
//	}
package slicecheck
