package cleanup

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/avivsinai/bitbucket-cli/internal/config"
	"github.com/avivsinai/bitbucket-cli/pkg/bbcloud"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
	"github.com/avivsinai/bitbucket-cli/pkg/iostreams"
)

func newCleanupFactory(cfg *config.Config) (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	ios := &iostreams.IOStreams{
		In:     io.NopCloser(bytes.NewReader(nil)),
		Out:    stdout,
		ErrOut: stderr,
	}

	f := &cmdutil.Factory{
		AppVersion:     "test",
		ExecutableName: "bkt",
		IOStreams:      ios,
		Config: func() (*config.Config, error) {
			return cfg, nil
		},
	}
	return f, stdout, stderr
}

func runCleanupCmd(t *testing.T, f *cmdutil.Factory, args ...string) error {
	t.Helper()

	cmd := NewCmdCleanup(f)
	cmd.PersistentFlags().String("context", "", "Named context to use")
	cmd.PersistentFlags().Bool("json", false, "Output in JSON format")
	cmd.PersistentFlags().Bool("yaml", false, "Output in YAML format")
	cmd.PersistentFlags().String("format", "", "Output format")
	cmd.PersistentFlags().String("jq", "", "Apply jq expression")
	cmd.PersistentFlags().String("template", "", "Render template")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	cmd.SetArgs(args)
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)

	return cmd.ExecuteContext(context.Background())
}

func cloudCleanupConfig(baseURL string) *config.Config {
	return &config.Config{
		ActiveContext: "test",
		Contexts: map[string]*config.Context{
			"test": {Host: "mock", Workspace: "ws"},
		},
		Hosts: map[string]*config.Host{
			"mock": {Kind: "cloud", BaseURL: baseURL, Username: "admin", Token: "token"},
		},
	}
}

func TestCleanupReposRequiresWorkspace(t *testing.T) {
	cfg := &config.Config{
		ActiveContext: "test",
		Contexts:      map[string]*config.Context{"test": {Host: "mock"}},
		Hosts:         map[string]*config.Host{"mock": {Kind: "cloud", BaseURL: "http://localhost", Username: "admin", Token: "token"}},
	}
	f, _, _ := newCleanupFactory(cfg)
	err := runCleanupCmd(t, f, "repos", "--inactive-for", "180d")
	if err == nil {
		t.Fatal("expected error when workspace is missing")
	}
	if !strings.Contains(err.Error(), "workspace required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCleanupReposRequiresHeuristicFlag(t *testing.T) {
	f, _, _ := newCleanupFactory(cloudCleanupConfig("http://localhost"))
	err := runCleanupCmd(t, f, "repos", "--workspace", "ws")
	if err == nil {
		t.Fatal("expected error when no heuristic flag is provided")
	}
	if !strings.Contains(err.Error(), "at least one heuristic flag") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCleanupReposResolvesWorkspaceFromContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/ws" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"slug": "ctx-repo", "name": "Ctx Repo", "is_private": true, "updated_on": "2026-01-01T00:00:00Z", "mainbranch": map[string]any{}, "project": map[string]any{"key": "WEB"}, "workspace": map[string]any{"slug": "ws"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupCmd(t, f, "repos", "--empty", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var raw []repoCandidate
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if len(raw) != 1 || raw[0].Repo != "ctx-repo" {
		t.Fatalf("expected ctx-repo from context workspace, got %v", raw)
	}
}

func TestCleanupReposInactiveFor(t *testing.T) {
	oldDate := time.Now().UTC().Add(-200 * 24 * time.Hour).Format(time.RFC3339)
	recentDate := time.Now().UTC().Add(-10 * 24 * time.Hour).Format(time.RFC3339)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/ws" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{
					"slug":       "old-repo",
					"name":       "Old Repo",
					"is_private": true,
					"updated_on": oldDate,
					"mainbranch": map[string]any{"name": "main"},
					"project":    map[string]any{"key": "WEB"},
					"workspace":  map[string]any{"slug": "ws"},
				},
				{
					"slug":       "new-repo",
					"name":       "New Repo",
					"is_private": true,
					"updated_on": recentDate,
					"mainbranch": map[string]any{"name": "main"},
					"project":    map[string]any{"key": "WEB"},
					"workspace":  map[string]any{"slug": "ws"},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupCmd(t, f, "repos", "--workspace", "ws", "--inactive-for", "180d", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		Candidates []repoCandidate `json:"candidates"`
	}
	// The command writes a list of repoCandidate directly when structured output is enabled.
	var raw []repoCandidate
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	_ = result

	if len(raw) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(raw))
	}
	if raw[0].Repo != "old-repo" {
		t.Fatalf("expected old-repo, got %s", raw[0].Repo)
	}
	if !strings.Contains(raw[0].Reason[0], "inactive") {
		t.Fatalf("expected inactive reason, got %v", raw[0].Reason)
	}
}

func TestCleanupReposEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/ws" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{
					"slug":       "empty-repo",
					"name":       "Empty Repo",
					"is_private": true,
					"updated_on": "2026-01-01T00:00:00Z",
					"mainbranch": map[string]any{},
					"project":    map[string]any{"key": "WEB"},
					"workspace":  map[string]any{"slug": "ws"},
				},
				{
					"slug":       "has-commits",
					"name":       "Has Commits",
					"is_private": true,
					"updated_on": "2026-01-01T00:00:00Z",
					"mainbranch": map[string]any{"name": "main"},
					"project":    map[string]any{"key": "WEB"},
					"workspace":  map[string]any{"slug": "ws"},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupCmd(t, f, "repos", "--workspace", "ws", "--empty", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw []repoCandidate
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if len(raw) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(raw))
	}
	if raw[0].Repo != "empty-repo" {
		t.Fatalf("expected empty-repo, got %s", raw[0].Repo)
	}
	if raw[0].Reason[0] != "empty" {
		t.Fatalf("expected empty reason, got %v", raw[0].Reason)
	}
}

func TestCleanupReposVisibility(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/ws" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"slug": "public-repo", "name": "Public", "is_private": false, "updated_on": "2026-01-01T00:00:00Z", "mainbranch": map[string]any{"name": "main"}, "project": map[string]any{"key": "WEB"}, "workspace": map[string]any{"slug": "ws"}},
				{"slug": "private-repo", "name": "Private", "is_private": true, "updated_on": "2026-01-01T00:00:00Z", "mainbranch": map[string]any{"name": "main"}, "project": map[string]any{"key": "WEB"}, "workspace": map[string]any{"slug": "ws"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupCmd(t, f, "repos", "--workspace", "ws", "--public", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw []repoCandidate
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if len(raw) != 1 || raw[0].Repo != "public-repo" {
		t.Fatalf("expected public-repo only, got %v", raw)
	}

	f2, stdout2, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupCmd(t, f2, "repos", "--workspace", "ws", "--private", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var raw2 []repoCandidate
	if err := json.Unmarshal(stdout2.Bytes(), &raw2); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout2.String())
	}
	if len(raw2) != 1 || raw2[0].Repo != "private-repo" {
		t.Fatalf("expected private-repo only, got %v", raw2)
	}
}

func TestCleanupReposProjectScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/ws" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query().Get("q")
		if q != `project.key="WEB"` {
			t.Fatalf("expected q=project.key=\"WEB\", got %q", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"slug": "scoped-repo", "name": "Scoped", "is_private": true, "updated_on": "2026-01-01T00:00:00Z", "mainbranch": map[string]any{}, "project": map[string]any{"key": "WEB"}, "workspace": map[string]any{"slug": "ws"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupCmd(t, f, "repos", "--workspace", "ws", "--project", "WEB", "--empty", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var raw []repoCandidate
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if len(raw) != 1 || raw[0].ProjectKey != "WEB" {
		t.Fatalf("expected WEB project, got %v", raw)
	}
}

func TestCleanupReposExcludePattern(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/ws" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"slug": "service-legacy", "name": "Legacy", "is_private": true, "updated_on": "2026-01-01T00:00:00Z", "mainbranch": map[string]any{}, "project": map[string]any{"key": "WEB"}, "workspace": map[string]any{"slug": "ws"}},
				{"slug": "service-new", "name": "New", "is_private": true, "updated_on": "2026-01-01T00:00:00Z", "mainbranch": map[string]any{}, "project": map[string]any{"key": "WEB"}, "workspace": map[string]any{"slug": "ws"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupCmd(t, f, "repos", "--workspace", "ws", "--empty", "--exclude-pattern", "*-legacy", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var raw []repoCandidate
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if len(raw) != 1 || raw[0].Repo != "service-new" {
		t.Fatalf("expected service-new only, got %v", raw)
	}
}

func TestCleanupReposEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{}})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newCleanupFactory(cloudCleanupConfig(srv.URL))
	if err := runCleanupCmd(t, f, "repos", "--workspace", "ws", "--empty"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No repository cleanup candidates") {
		t.Fatalf("expected empty result message, got: %s", stdout.String())
	}
}

func repoWith(name, slug, updated, branch string, private bool) bbcloud.Repository {
	return bbcloud.Repository{
		Slug:      slug,
		Name:      name,
		UpdatedOn: updated,
		Mainbranch: struct {
			Name string `json:"name"`
		}{Name: branch},
		Workspace: struct {
			Slug string `json:"slug"`
		}{Slug: "ws"},
		Project: struct {
			Key string `json:"key"`
		}{Key: "WEB"},
		IsPrivate: private,
	}
}

func TestEvaluateRepositoriesInactive(t *testing.T) {
	oldDate := time.Now().UTC().Add(-200 * 24 * time.Hour).Format(time.RFC3339)
	recentDate := time.Now().UTC().Add(-10 * 24 * time.Hour).Format(time.RFC3339)

	repos := []bbcloud.Repository{
		repoWith("old", "old", oldDate, "main", true),
		repoWith("new", "new", recentDate, "main", true),
	}

	filter := repoFilter{InactiveDays: 180}
	candidates := evaluateRepositories(repos, filter)
	if len(candidates) != 1 || candidates[0].Repo != "old" {
		t.Fatalf("expected old repo candidate, got %v", candidates)
	}
}

func TestEvaluateRepositoriesEmpty(t *testing.T) {
	repos := []bbcloud.Repository{
		repoWith("empty", "empty", "", "", false),
		repoWith("not-empty", "not-empty", "", "main", false),
	}
	filter := repoFilter{Empty: true}
	candidates := evaluateRepositories(repos, filter)
	if len(candidates) != 1 || candidates[0].Repo != "empty" {
		t.Fatalf("expected empty repo candidate, got %v", candidates)
	}
}

func TestEvaluateRepositoriesVisibility(t *testing.T) {
	repos := []bbcloud.Repository{
		repoWith("pub", "pub", "", "main", false),
		repoWith("priv", "priv", "", "main", true),
	}
	publicCandidates := evaluateRepositories(repos, repoFilter{Public: true})
	if len(publicCandidates) != 1 || publicCandidates[0].Repo != "pub" {
		t.Fatalf("expected public candidate, got %v", publicCandidates)
	}
	privateCandidates := evaluateRepositories(repos, repoFilter{Private: true})
	if len(privateCandidates) != 1 || privateCandidates[0].Repo != "priv" {
		t.Fatalf("expected private candidate, got %v", privateCandidates)
	}
}

func TestEvaluateRepositoriesORLogic(t *testing.T) {
	repos := []bbcloud.Repository{
		repoWith("inactive", "inactive", time.Now().UTC().Add(-200*24*time.Hour).Format(time.RFC3339), "main", true),
		repoWith("public", "public", time.Now().UTC().Format(time.RFC3339), "main", false),
	}
	filter := repoFilter{InactiveDays: 180, Public: true}
	candidates := evaluateRepositories(repos, filter)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates from OR logic, got %v", candidates)
	}
}

func TestIsExcluded(t *testing.T) {
	cases := []struct {
		pattern string
		slug    string
		want    bool
	}{
		{"*-legacy", "service-legacy", true},
		{"*-legacy", "service-new", false},
		{"*", "anything", true},
		{"", "anything", false},
		{"test-*", "test-repo", true},
		{"test-*", "TEST-REPO", true},
	}
	for _, c := range cases {
		got := isExcluded(c.slug, c.pattern)
		if got != c.want {
			t.Errorf("isExcluded(%q, %q) = %v, want %v", c.slug, c.pattern, got, c.want)
		}
	}
}

func TestParseInactiveFor(t *testing.T) {
	cases := []struct {
		input string
		want  int
		err   bool
	}{
		{"180d", 180, false},
		{"30d", 30, false},
		{"0d", 0, false},
		{"180", 0, true},
		{"", 0, true},
		{"-1d", 0, true},
	}
	for _, c := range cases {
		got, err := parseInactiveFor(c.input)
		if (err != nil) != c.err {
			t.Errorf("parseInactiveFor(%q) error = %v, wantErr %v", c.input, err, c.err)
			continue
		}
		if got != c.want {
			t.Errorf("parseInactiveFor(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestCleanupReposRejectsDataCenter(t *testing.T) {
	cfg := &config.Config{
		ActiveContext: "test",
		Contexts:      map[string]*config.Context{"test": {Host: "mock"}},
		Hosts:         map[string]*config.Host{"mock": {Kind: "dc", BaseURL: "http://localhost", Username: "admin", Token: "token"}},
	}
	f, _, _ := newCleanupFactory(cfg)
	err := runCleanupCmd(t, f, "repos", "--workspace", "ws", "--empty")
	if err == nil {
		t.Fatal("expected error for DC context")
	}
	if !strings.Contains(err.Error(), "Cloud contexts only") {
		t.Fatalf("unexpected error: %v", err)
	}
}
