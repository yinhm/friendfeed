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

func TestShowShareForUser(t *testing.T) {
	tests := []struct {
		name      string
		userUuid  string
		requested bool
		want      bool
	}{
		{name: "anonymous", requested: true, want: false},
		{name: "authenticated and writable", userUuid: "user-uuid", requested: true, want: true},
		{name: "authenticated but read-only", userUuid: "user-uuid", requested: false, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := showShareForUser(test.userUuid, test.requested); got != test.want {
				t.Fatalf("showShareForUser(%q, %t) = %t; want %t", test.userUuid, test.requested, got, test.want)
			}
		})
	}
}
