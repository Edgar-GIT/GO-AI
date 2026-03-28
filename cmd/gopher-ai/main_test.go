package main

import "testing"

func TestUIURLFromAddress(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                 "http://127.0.0.1:8080",
		":8080":            "http://127.0.0.1:8080",
		"0.0.0.0:8080":     "http://127.0.0.1:8080",
		"[::]:8080":        "http://127.0.0.1:8080",
		"127.0.0.1:8080":   "http://127.0.0.1:8080",
		"localhost:8080":   "http://localhost:8080",
		"192.168.1.8:9000": "http://192.168.1.8:9000",
	}

	for input, want := range cases {
		if got := uiURLFromAddress(input); got != want {
			t.Fatalf("uiURLFromAddress(%q) = %q, want %q", input, got, want)
		}
	}
}
