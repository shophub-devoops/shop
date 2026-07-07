package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewDiscordNilWhenUnconfigured(t *testing.T) {
	if NewDiscord("") != nil {
		t.Fatal("expected nil Discord for empty webhook URL")
	}
}

func TestSendPostsContent(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("invalid JSON body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	d := NewDiscord(srv.URL)
	if err := d.Send(context.Background(), "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got["content"] != "hello" {
		t.Fatalf("content = %q, want %q", got["content"], "hello")
	}
}

func TestSendErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	if err := NewDiscord(srv.URL).Send(context.Background(), "x"); err == nil {
		t.Fatal("expected error for 403 response")
	}
}
