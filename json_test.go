package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name, body string
		wantErr    bool
	}{
		{"valid", `{"name":"Azat"}`, false},
		{"unknown field", `{"name":"Azat","unexpected":true}`, true},
		{"multiple values", `{"name":"Azat"} {"name":"other"}`, true},
		{"invalid json", `{"name":`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			var dst struct {
				Name string `json:"name"`
			}
			err := decodeJSON(w, req, 1024, &dst)
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeJSON() error=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
