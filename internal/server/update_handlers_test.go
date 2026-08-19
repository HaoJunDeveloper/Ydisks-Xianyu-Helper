package server

import "testing"

func TestNormalizeAndCompareVersion(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  int
		valid bool
	}{
		{name: "multi digit patch", left: "v0.1.10", right: "0.1.9", want: 1, valid: true},
		{name: "equal with prefix", left: "v1.2.3", right: "1.2.3", want: 0, valid: true},
		{name: "older minor", left: "1.9.0", right: "1.10.0", want: -1, valid: true},
		{name: "invalid suffix", left: "1.2.3-beta", right: "1.2.3", want: 0, valid: false},
		{name: "incomplete", left: "1.2", right: "1.2.0", want: 0, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isValidVersion(test.left); got != test.valid {
				t.Fatalf("isValidVersion(%q) = %v, want %v", test.left, got, test.valid)
			}
			if got := compareVersion(test.left, test.right); got != test.want {
				t.Fatalf("compareVersion(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
			}
		})
	}
}

func TestNewerVersionRejectsInvalidVersions(t *testing.T) {
	if newerVersion("v1.2.3-beta", "1.2.2") {
		t.Fatal("pre-release suffix must not be treated as a production version")
	}
	if !newerVersion("v0.1.10", "v0.1.9") {
		t.Fatal("multi-digit patch version should compare numerically")
	}
}
