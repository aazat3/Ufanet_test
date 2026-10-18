package main

import "testing"

func TestValidUsername(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid", "viewer_01", true},
		{"too short", "ab", false},
		{"space", "bad user", false},
		{"unicode", "пользователь", false},
		{"too long", "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijkabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijk", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validUsername(tt.input); got != tt.want {
				t.Fatalf("validUsername(%q)=%v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidStreamURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://streams.example.com/live/index.m3u8", true},
		{"http://streams.example.com/live/index.m3u8", true},
		{"file:///etc/passwd", false},
		{"javascript:alert(1)", false},
		{"not a url", false},
		{"https:///missing-host", false},
	}
	for _, tt := range tests {
		if got := validStreamURL(tt.input); got != tt.want {
			t.Errorf("validStreamURL(%q)=%v, want %v", tt.input, got, tt.want)
		}
	}
}
