package cleanup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avivsinai/bitbucket-cli/internal/config"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
)

func runCleanupProjectsCmd(t *testing.T, f *cmdutil.Factory, args ...string) error {
	t.Helper()
	cmd := NewCmdCleanup(f)
	cmd.PersistentFlags().String("context", "", "Named context to use")
	cmd.PersistentFlags().Bool("json", false, "Output in JSON format")
	cmd.PersistentFlags().String("format", "", "Output format")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(append([]string{"projects"}, args...))
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)
	return cmd.ExecuteContext(context.Background())
}

func TestCleanupProjectsRequiresWorkspace(t *testing.T) {
	cfg := &config.Config{
		ActiveContext: "test",
		Contexts:      map[string]*config.Context{"test": {Host: "mock"}},
		Hosts:         map[string]*config.Host{"mock": {Kind: "cloud", BaseURL: "http://localhost", Username: "admin", Token: "token"}},
	}
	f, _, _ := newCleanupFactory(cfg)
	err := runCleanupProjectsCmd(t, f, "--empty")
	if err == nil {
		t.Fatal("expected error when workspace is missing")
	}
	if !strings.Contains(err.Error(), "workspace required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCleanupProjectsRequiresHeuristicFlag(t *testing.T) {
	f, _, _ := newCleanupFactory(cloudCleanupConfig("http://localhost"))
	err := runCleanupProjectsCmd(t, f, "--workspace", "ws")
	if err == nil {
		t.Fatal("expected error when no heuristic flag is provided")
	}
	if !strings.Contains(err.Error(), "at least one heuristic flag") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCleanupProjectsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"key": "EMPTY", "name": "Empty Project"},
					{"key": "BUSY", "name": "Busy Project"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws":
			q := r.URL.Query().Get("q")
			switch q {
			case `project.key="EMPTY"`:
				_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{}})
			case `project.key="BUSY"`:
				_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{{"slug": "repo"}}})
			default:
				t.Fatalf("unexpected q = %q", q)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupProjectsCmd(t, f, "--workspace", "ws", "--empty", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw []projectCandidate
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if len(raw) != 1 || raw[0].ProjectKey != "EMPTY" {
		t.Fatalf("expected EMPTY project, got %v", raw)
	}
	if !raw[0].Empty || !raw[0].DeleteCandidate {
		t.Fatalf("expected empty and delete candidate, got %+v", raw[0])
	}
	if raw[0].Reason[0] != "empty" {
		t.Fatalf("expected empty reason, got %v", raw[0].Reason)
	}
}

func TestCleanupProjectsInactive(t *testing.T) {
	oldDate := time.Now().UTC().Add(-200 * 24 * time.Hour).Format(time.RFC3339)
	recentDate := time.Now().UTC().Add(-10 * 24 * time.Hour).Format(time.RFC3339)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"key": "STALE", "name": "Stale Project"},
					{"key": "ACTIVE", "name": "Active Project"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws":
			q := r.URL.Query().Get("q")
			switch q {
			case `project.key="STALE"`:
				_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{
					{"slug": "old-repo", "updated_on": oldDate},
				}})
			case `project.key="ACTIVE"`:
				_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{
					{"slug": "new-repo", "updated_on": recentDate},
				}})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupProjectsCmd(t, f, "--workspace", "ws", "--inactive-for", "180d", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw []projectCandidate
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if len(raw) != 1 || raw[0].ProjectKey != "STALE" {
		t.Fatalf("expected STALE project, got %v", raw)
	}
	if !raw[0].AllReposInactive || !raw[0].DeleteCandidate {
		t.Fatalf("expected all repos inactive and delete candidate, got %+v", raw[0])
	}
	if !strings.Contains(raw[0].Reason[0], "inactive") {
		t.Fatalf("expected inactive reason, got %v", raw[0].Reason)
	}
}

func TestCleanupProjectsMixedReposNotInactive(t *testing.T) {
	oldDate := time.Now().UTC().Add(-200 * 24 * time.Hour).Format(time.RFC3339)
	recentDate := time.Now().UTC().Add(-10 * 24 * time.Hour).Format(time.RFC3339)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"key": "MIXED", "name": "Mixed Project"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws":
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{
				{"slug": "old-repo", "updated_on": oldDate},
				{"slug": "new-repo", "updated_on": recentDate},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupProjectsCmd(t, f, "--workspace", "ws", "--inactive-for", "180d", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw []projectCandidate
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if len(raw) != 0 {
		t.Fatalf("expected no candidates because one repo is active, got %v", raw)
	}
}

func TestCleanupProjectsRejectsDataCenter(t *testing.T) {
	cfg := &config.Config{
		ActiveContext: "test",
		Contexts:      map[string]*config.Context{"test": {Host: "mock"}},
		Hosts:         map[string]*config.Host{"mock": {Kind: "dc", BaseURL: "http://localhost", Username: "admin", Token: "token"}},
	}
	f, _, _ := newCleanupFactory(cfg)
	err := runCleanupProjectsCmd(t, f, "--workspace", "ws", "--empty")
	if err == nil {
		t.Fatal("expected error for DC context")
	}
	if !strings.Contains(err.Error(), "Cloud contexts only") {
		t.Fatalf("unexpected error: %v", err)
	}
}
