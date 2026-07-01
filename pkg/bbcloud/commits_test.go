package bbcloud

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestListCommits(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/repositories/ws/repo/commits" {
			t.Fatalf("path = %q", got)
		}
		if got := r.URL.Query().Get("include"); got != "main" {
			t.Fatalf("include = %q, want main", got)
		}
		if got := r.URL.Query().Get("pagelen"); got != "1" {
			t.Fatalf("pagelen = %q, want 1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{{
				"hash":    "abc123",
				"date":    "2026-06-18T14:22:00Z",
				"message": "feat: latest",
			}},
		})
	})

	client := newTestClient(t, handler)
	commits, err := client.ListCommits(context.Background(), "ws", "repo", CommitListOptions{Include: "main", Limit: 1})
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("len(commits) = %d, want 1", len(commits))
	}
	if commits[0].Message != "feat: latest" {
		t.Fatalf("message = %q", commits[0].Message)
	}
}

func TestListCommitsTrimsToLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"hash": "1", "message": "first"},
				{"hash": "2", "message": "second"},
			},
		})
	})

	client := newTestClient(t, handler)
	commits, err := client.ListCommits(context.Background(), "ws", "repo", CommitListOptions{Limit: 1})
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 1 || commits[0].Hash != "1" {
		t.Fatalf("unexpected commits: %+v", commits)
	}
}

func TestListCommitsValidation(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	_, err := client.ListCommits(context.Background(), "", "repo", CommitListOptions{})
	if err == nil {
		t.Fatal("expected error for empty workspace")
	}
	_, err = client.ListCommits(context.Background(), "ws", "", CommitListOptions{})
	if err == nil {
		t.Fatal("expected error for empty repo slug")
	}
}
