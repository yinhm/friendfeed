package server

import "testing"

func TestExtractNextPath(t *testing.T) {
	tests := map[string]string{
		"":                                  "/",
		"/":                                 "/",
		"/feed/yinhm?page=2":                "/feed/yinhm?page=2",
		"hw-y778egVoO5g6pkV4z4tYZhSvivZkig": "/",
		"https://evil.example/auth/google":  "/",
		"//evil.example/auth/google":        "/",
	}
	for input, want := range tests {
		if got := extractNextPath(input); got != want {
			t.Errorf("extractNextPath(%q) = %q; want %q", input, got, want)
		}
	}
}
