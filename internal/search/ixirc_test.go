package search

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIxircSearch_EmptyTerm(t *testing.T) {
	e := &IxircEngine{}
	packs, err := e.Search("")
	if err != nil {
		t.Fatal(err)
	}
	if packs != nil {
		t.Error("expected nil packs for empty term")
	}
}

func TestIxircSearch_ValidResults(t *testing.T) {
	resp := ixircResponse{
		PageCount: 1,
		Results: []ixircResult{
			{Uname: "BotA", Naddr: "irc.rizon.net", Nport: 6667, N: 10, Name: "file.mkv", Sz: 1024},
			// Empty Uname → bot offline, must be skipped.
			{Uname: "", Naddr: "irc.rizon.net", Nport: 6667, N: 11, Name: "offline.mkv", Sz: 512},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := &IxircEngine{baseURL: srv.URL}
	packs, err := e.Search("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(packs) != 1 {
		t.Fatalf("got %d packs, want 1 (offline bot must be skipped)", len(packs))
	}
	if packs[0].Bot != "BotA" || packs[0].PackNumber != 10 {
		t.Errorf("pack = bot=%q num=%d", packs[0].Bot, packs[0].PackNumber)
	}
}

func TestIxircSearch_DefaultPort(t *testing.T) {
	// When Nport is 0, server port must default to 6667.
	resp := ixircResponse{
		PageCount: 1,
		Results: []ixircResult{
			{Uname: "BotB", Naddr: "irc.rizon.net", Nport: 0, N: 5, Name: "ep.mkv", Sz: 500},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := &IxircEngine{baseURL: srv.URL}
	packs, err := e.Search("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 1 {
		t.Fatalf("got %d packs", len(packs))
	}
	if packs[0].Server.Port != 6667 {
		t.Errorf("Port = %d, want 6667", packs[0].Server.Port)
	}
}

func TestIxircSearch_Pagination(t *testing.T) {
	// Two pages: PageCount=2, server returns page 0 then page 1.
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if page == 0 {
			page++
			json.NewEncoder(w).Encode(ixircResponse{
				PageCount: 2,
				Results:   []ixircResult{{Uname: "BotA", Naddr: "irc.rizon.net", Nport: 6667, N: 1, Name: "a.mkv", Sz: 100}},
			})
		} else {
			json.NewEncoder(w).Encode(ixircResponse{
				PageCount: 2,
				Results:   []ixircResult{{Uname: "BotB", Naddr: "irc.rizon.net", Nport: 6667, N: 2, Name: "b.mkv", Sz: 200}},
			})
		}
	}))
	defer srv.Close()

	e := &IxircEngine{baseURL: srv.URL}
	packs, err := e.Search("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 2 {
		t.Fatalf("got %d packs, want 2 (one per page)", len(packs))
	}
}

func TestIxircSearch_NetworkError(t *testing.T) {
	e := &IxircEngine{baseURL: "http://127.0.0.1:1"}
	_, err := e.Search("test")
	if err == nil {
		t.Error("expected error for unreachable server, got nil")
	}
}

func TestIxircSearch_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	e := &IxircEngine{baseURL: srv.URL}
	_, err := e.Search("test")
	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}
