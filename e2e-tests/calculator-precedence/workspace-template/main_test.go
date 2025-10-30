package main

import (
	"math"
	"testing"
)

func TestEvaluateRespectsPrecedence(t *testing.T) {
	t.Helper()

	cases := []struct {
		expr     string
		expected float64
	}{
		{"10 + 5 * 2", 20},
		{"8 - 2 * 3", 2},
		{"2 + 3 * 4", 14},
		{"18 / 3 + 2", 8},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			got, err := evaluate(tc.expr)
			if err != nil {
				t.Fatalf("evaluate(%q) returned unexpected error: %v", tc.expr, err)
			}
			if diff := math.Abs(got - tc.expected); diff > 1e-9 {
				t.Fatalf("evaluate(%q) = %v, want %v", tc.expr, got, tc.expected)
			}
		})
	}
}

func TestEvaluateHandlesParentheses(t *testing.T) {
	t.Helper()

	cases := []struct {
		expr     string
		expected float64
	}{
		{"(2 + 3) * 4", 20},
		{"(8 - 2) * (5 - 3)", 12},
		{"(10 + 5) / (3 + 2)", 3},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.expr, func(t *testing.T) {
			t.Parallel()
			got, err := evaluate(tc.expr)
			if err != nil {
				t.Fatalf("evaluate(%q) returned unexpected error: %v", tc.expr, err)
			}
			if diff := math.Abs(got - tc.expected); diff > 1e-9 {
				t.Fatalf("evaluate(%q) = %v, want %v", tc.expr, got, tc.expected)
			}
		})
	}
}
