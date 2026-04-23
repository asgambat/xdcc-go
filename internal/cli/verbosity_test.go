package cli

import "testing"

func TestVerbosityLevel(t *testing.T) {
	tests := []struct {
		verbose, quiet int
		want           int
	}{
		{0, 0, 0},   // default
		{1, 0, 1},   // -v
		{2, 0, 2},   // -vv
		{0, 1, -1},  // -q
		{0, 2, -2},  // -qq
		{1, 1, -1},  // -v -q → -q wins
		{2, 1, -1},  // -vv -q → -q wins
		{1, 2, -2},  // -v -qq → -qq wins
		{2, 2, -2},  // -vv -qq → -qq wins
		{0, 3, -2},  // extra quiet still -2
	}
	for _, tt := range tests {
		got := VerbosityLevel(tt.verbose, tt.quiet)
		if got != tt.want {
			t.Errorf("VerbosityLevel(%d, %d) = %d, want %d",
				tt.verbose, tt.quiet, got, tt.want)
		}
	}
}
