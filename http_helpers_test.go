package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWriteAuthRequired(t *testing.T) {
	w := httptest.NewRecorder()
	writeAuthRequired(w)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Basic ") {
		t.Fatalf("WWW-Authenticate = %q, want Basic challenge", got)
	}
	var body APIError
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body.Error == "" {
		t.Fatal("JSON error message is empty")
	}
}

func TestChannelInternalExpiryIsNotSerialized(t *testing.T) {
	deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	data, err := json.Marshal(Channel{ID: 1, Number: 1, Name: "Первый", ValidUntil: &deadline})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ValidUntil") || strings.Contains(string(data), "2030-01-01") {
		t.Fatalf("internal cache metadata leaked to JSON: %s", data)
	}
}

func TestMapStoreErrorUsesJSONAndExpectedStatus(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{errInvalid, http.StatusBadRequest},
		{errNotFound, http.StatusNotFound},
		{errConflict, http.StatusConflict},
		{errLastAdmin, http.StatusConflict},
	}
	for _, tt := range tests {
		w := httptest.NewRecorder()
		mapStoreError(w, tt.err)
		if w.Code != tt.want {
			t.Errorf("error %v: status=%d, want %d", tt.err, w.Code, tt.want)
		}
		if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("error response Content-Type=%q, want JSON", got)
		}
	}
}
