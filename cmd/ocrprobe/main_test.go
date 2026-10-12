package main

import "testing"

func TestWordAccuracyCountsEditsAndReadingOrder(t *testing.T) {
	for _, test := range []struct {
		expected, actual string
		want             float64
	}{
		{"one two", "one\ntwo", 1}, {"one two", "two one", 0}, {"one two", "one", .5}, {"", "", 1}, {"", "noise", 0},
	} {
		if got := accuracy(test.expected, test.actual); got != test.want {
			t.Fatalf("%q vs %q: %v", test.expected, test.actual, got)
		}
	}
}
