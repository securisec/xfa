package cmd

import "testing"

func TestClampLimit(t *testing.T) {
	for in, want := range map[int]int{0: defaultReadLimit, -1: defaultReadLimit, 1: 1, 50: 50} {
		if got := clampLimit(in, defaultReadLimit); got != want {
			t.Errorf("clampLimit(%d, %d) = %d, want %d", in, defaultReadLimit, got, want)
		}
	}
	if got := clampLimit(0, defaultThreadsLimit); got != defaultThreadsLimit {
		t.Errorf("clampLimit(0, %d) = %d; the fallback is the caller's own default", defaultThreadsLimit, got)
	}
}
