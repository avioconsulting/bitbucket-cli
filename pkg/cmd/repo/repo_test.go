package repo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avivsinai/bitbucket-cli/internal/config"
	"github.com/avivsinai/bitbucket-cli/pkg/bbdc"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
	"github.com/avivsinai/bitbucket-cli/pkg/iostreams"
	"github.com/avivsinai/bitbucket-cli/pkg/prompter"
)

type testPrompter struct {
	confirmed bool
	err       error
	called    bool
	prompt    string
}

func (p *testPrompter) Input(string, string) (string, error) { return "", nil }
func (p *testPrompter) Password(string) (string, error)      { return "", nil }
func (p *testPrompter) Confirm(prompt string, _ bool) (bool, error) {
	p.called = true
	p.prompt = prompt
	return p.confirmed, p.err
}

var _ prompter.Interface = (*testPrompter)(nil)

func newRepoTestFactory(cfg *config.Config, prompt prompter.Interface) (*cmdutil.Factory, *strings.Builder, *strings.Builder) {
	var stdout, stderr strings.Builder
	f := &cmdutil.Factory{
		AppVersion:     "test",
		ExecutableName: "bkt",
		IOStreams: &iostreams.IOStreams{
			In:     io.NopCloser(strings.NewReader("")),
			Out:    &stdout,
			ErrOut: &stderr,
		},
		Config: func() (*config.Config, error) {
			return cfg, nil
		},
		Prompter: prompt,
	}
	return f, &stdout, &stderr
}

func TestSelectCloneURLDCPrefersHTTPS(t *testing.T) {
	var r bbdc.Repository
	r.Links.Clone = []struct {
		Href string `json:"href"`
		Name string `json:"name"`
	}{
		{Href: "ssh://git@bitbucket.example.com:7999/PROJ/repo.git", Name: "ssh"},
		{Href: "https://bitbucket.example.com/scm/PROJ/repo.git", Name: "https"},
	}

	got, err := selectCloneURLDC(r, false)
	if err != nil {
		t.Fatalf("selectCloneURLDC returned error: %v", err)
	}
	if got != "https://bitbucket.example.com/scm/PROJ/repo.git" {
		t.Fatalf("got %q, want https link", got)
	}
}

func TestSelectCloneURLDCHttpAlias(t *testing.T) {
	var r bbdc.Repository
	r.Links.Clone = []struct {
		Href string `json:"href"`
		Name string `json:"name"`
	}{
		{Href: "http://bitbucket.example.com/scm/PROJ/repo.git", Name: "http"},
	}

	got, err := selectCloneURLDC(r, false)
	if err != nil {
		t.Fatalf("selectCloneURLDC returned error: %v", err)
	}
	if got != "http://bitbucket.example.com/scm/PROJ/repo.git" {
		t.Fatalf("got %q, want http link", got)
	}
}

func TestSelectCloneURLDCSsh(t *testing.T) {
	var r bbdc.Repository
	r.Links.Clone = []struct {
		Href string `json:"href"`
		Name string `json:"name"`
	}{
		{Href: "ssh://git@bitbucket.example.com:7999/PROJ/repo.git", Name: "ssh"},
		{Href: "https://bitbucket.example.com/scm/PROJ/repo.git", Name: "https"},
	}

	got, err := selectCloneURLDC(r, true)
	if err != nil {
		t.Fatalf("selectCloneURLDC returned error: %v", err)
	}
	if !strings.HasPrefix(got, "ssh://") {
		t.Fatalf("got %q, want ssh link", got)
	}
}

func TestSelectCloneURLDCMissing(t *testing.T) {
	var r bbdc.Repository
	r.Links.Clone = []struct {
		Href string `json:"href"`
		Name string `json:"name"`
	}{
		{Href: "https://bitbucket.example.com/scm/PROJ/repo.git", Name: "https"},
	}

	_, err := selectCloneURLDC(r, true)
	if err == nil {
		t.Fatalf("expected error when ssh clone missing")
	}
}

func TestBrowseWithoutRepoDefaults(t *testing.T) {
	cfg := &config.Config{
		ActiveContext: "default",
		Contexts: map[string]*config.Context{
			"default": {
				Host:       "main",
				ProjectKey: "dev",
			},
		},
		Hosts: map[string]*config.Host{
			"main": {
				Kind:    "dc",
				BaseURL: "https://bitbucket.example.com",
				Token:   "test-token",
			},
		},
	}

	var stdout, stderr strings.Builder
	f := &cmdutil.Factory{
		AppVersion:     "test",
		ExecutableName: "bkt",
		IOStreams: &iostreams.IOStreams{
			Out:    &stdout,
			ErrOut: &stderr,
		},
		Config: func() (*config.Config, error) {
			return cfg, nil
		},
	}

	cmd := newBrowseCmd(f)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when repo not provided")
	}
	if !strings.Contains(err.Error(), "repository required") {
		t.Fatalf("expected error to mention repository requirement, got %q", err.Error())
	}
}

func TestDefaultReviewersListDataCenterRequiresRefs(t *testing.T) {
	cfg := &config.Config{
		ActiveContext: "default",
		Contexts: map[string]*config.Context{
			"default": {Host: "main", ProjectKey: "PROJ", DefaultRepo: "repo"},
		},
		Hosts: map[string]*config.Host{
			"main": {Kind: "dc", BaseURL: "https://bitbucket.example.com", Token: "test-token"},
		},
	}

	f := &cmdutil.Factory{
		AppVersion:     "test",
		ExecutableName: "bkt",
		IOStreams:      &iostreams.IOStreams{Out: &strings.Builder{}, ErrOut: &strings.Builder{}},
		Config: func() (*config.Config, error) {
			return cfg, nil
		},
	}

	cmd := newDefaultReviewersListCmd(f)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when Data Center source/target refs are missing")
	}
	if !strings.Contains(err.Error(), "data center default reviewers require --source and --target refs") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultReviewersListDataCenterUsesEffectiveReviewersEndpoint(t *testing.T) {
	var sawReviewersRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rest/api/1.0/projects/PROJ/repos/repo":
			_ = json.NewEncoder(w).Encode(bbdc.Repository{Slug: "repo", ID: 123})
		case "/rest/default-reviewers/1.0/projects/PROJ/repos/repo/reviewers":
			sawReviewersRequest = true
			query := r.URL.Query()
			if got := query.Get("sourceRepoId"); got != "123" {
				t.Fatalf("sourceRepoId = %q, want 123", got)
			}
			if got := query.Get("targetRepoId"); got != "123" {
				t.Fatalf("targetRepoId = %q, want 123", got)
			}
			if got := query.Get("sourceRefId"); got != "feature/auth" {
				t.Fatalf("sourceRefId = %q, want feature/auth", got)
			}
			if got := query.Get("targetRefId"); got != "main" {
				t.Fatalf("targetRefId = %q, want main", got)
			}
			_ = json.NewEncoder(w).Encode([]bbdc.User{
				{Name: "alice", FullName: "Alice", ID: 7},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{
		ActiveContext: "default",
		Contexts: map[string]*config.Context{
			"default": {Host: "main", ProjectKey: "PROJ", DefaultRepo: "repo"},
		},
		Hosts: map[string]*config.Host{
			"main": {Kind: "dc", BaseURL: server.URL, Token: "test-token"},
		},
	}

	stdout := &strings.Builder{}
	f := &cmdutil.Factory{
		AppVersion:     "test",
		ExecutableName: "bkt",
		IOStreams:      &iostreams.IOStreams{Out: stdout, ErrOut: &strings.Builder{}},
		Config: func() (*config.Config, error) {
			return cfg, nil
		},
	}

	cmd := newDefaultReviewersListCmd(f)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--source", "refs/heads/feature/auth", "--target", "main"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawReviewersRequest {
		t.Fatal("expected default reviewers request")
	}
	if !strings.Contains(stdout.String(), "Alice") || !strings.Contains(stdout.String(), "alice") {
		t.Fatalf("expected reviewer output, got %q", stdout.String())
	}
}

func TestDefaultReviewersListCloudRejectsRefFlags(t *testing.T) {
	cfg := &config.Config{
		ActiveContext: "default",
		Contexts: map[string]*config.Context{
			"default": {Host: "main", Workspace: "ws", DefaultRepo: "repo"},
		},
		Hosts: map[string]*config.Host{
			"main": {Kind: "cloud", BaseURL: "https://api.bitbucket.org/2.0", Token: "test-token"},
		},
	}

	f := &cmdutil.Factory{
		AppVersion:     "test",
		ExecutableName: "bkt",
		IOStreams:      &iostreams.IOStreams{Out: &strings.Builder{}, ErrOut: &strings.Builder{}},
		Config: func() (*config.Config, error) {
			return cfg, nil
		},
	}

	cmd := newDefaultReviewersListCmd(f)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--source", "feature", "--target", "main"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when Cloud source/target refs are provided")
	}
	if !strings.Contains(err.Error(), "--source and --target are only supported for Data Center") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepoCreateRejectsHostNoOpFlags(t *testing.T) {
	tests := []struct {
		name          string
		hostKind      string
		args          []string
		errorContains string
	}{
		{
			name:          "cloud rejects project",
			hostKind:      "cloud",
			args:          []string{"service", "--project", "DATA"},
			errorContains: "--project is not supported for Cloud repo create",
		},
		{
			name:          "cloud rejects forkable",
			hostKind:      "cloud",
			args:          []string{"service", "--forkable"},
			errorContains: "--forkable is not supported for Cloud repo create",
		},
		{
			name:          "cloud rejects default branch",
			hostKind:      "cloud",
			args:          []string{"service", "--default-branch", "main"},
			errorContains: "--default-branch is not supported for Cloud repo create",
		},
		{
			name:          "cloud rejects explicit scm",
			hostKind:      "cloud",
			args:          []string{"service", "--scm", "git"},
			errorContains: "--scm is not supported for Cloud repo create",
		},
		{
			name:          "data center rejects workspace",
			hostKind:      "dc",
			args:          []string{"service", "--workspace", "team"},
			errorContains: "--workspace is not supported for Data Center repo create",
		},
		{
			name:          "data center rejects cloud project",
			hostKind:      "dc",
			args:          []string{"service", "--cloud-project", "WEB"},
			errorContains: "--cloud-project is not supported for Data Center repo create",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var hits int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"slug": "service",
					"name": "service",
					"project": map[string]any{
						"key": "PROJ",
					},
				})
			}))
			t.Cleanup(server.Close)

			cfg := &config.Config{
				ActiveContext: "default",
				Contexts: map[string]*config.Context{
					"default": {
						Host:        "main",
						ProjectKey:  "PROJ",
						Workspace:   "workspace",
						DefaultRepo: "repo",
					},
				},
				Hosts: map[string]*config.Host{
					"main": {
						Kind:    tt.hostKind,
						BaseURL: server.URL,
						Token:   "test-token",
					},
				},
			}

			var stdout, stderr strings.Builder
			f := &cmdutil.Factory{
				AppVersion:     "test",
				ExecutableName: "bkt",
				IOStreams: &iostreams.IOStreams{
					Out:    &stdout,
					ErrOut: &stderr,
				},
				Config: func() (*config.Config, error) {
					return cfg, nil
				},
			}

			cmd := newCreateCmd(f)
			cmd.SetContext(context.Background())
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errorContains)
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Fatalf("error = %q, want substring %q", err, tt.errorContains)
			}
			if hits != 0 {
				t.Fatalf("expected validation to avoid HTTP requests, got %d", hits)
			}
		})
	}
}

func TestRepoDeleteRejectsDataCenterContexts(t *testing.T) {
	cfg := &config.Config{
		ActiveContext: "default",
		Contexts: map[string]*config.Context{
			"default": {Host: "main", ProjectKey: "PROJ", DefaultRepo: "repo"},
		},
		Hosts: map[string]*config.Host{
			"main": {Kind: "dc", BaseURL: "https://bitbucket.example.com", Token: "test-token"},
		},
	}

	f, _, _ := newRepoTestFactory(cfg, &testPrompter{confirmed: true})
	cmd := newDeleteCmd(f)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "repository deletion is only available for Bitbucket Cloud") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepoDeleteCloudUsesConfirmationContext(t *testing.T) {
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/old-service":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"slug":       "old-service",
				"name":       "Old Service",
				"uuid":       "{abc}",
				"mainbranch": map[string]any{"name": "main"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/old-service/commits":
			if got := r.URL.Query().Get("include"); got != "main" {
				t.Fatalf("include = %q, want main", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{{
					"hash":    "abc123",
					"date":    "2026-06-18T14:22:00Z",
					"message": "chore: archive legacy pipeline\n\nMore context",
				}},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/repositories/ws/old-service":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{
		ActiveContext: "default",
		Contexts: map[string]*config.Context{
			"default": {Host: "main", Workspace: "ws"},
		},
		Hosts: map[string]*config.Host{
			"main": {Kind: "cloud", BaseURL: server.URL, Token: "test-token"},
		},
	}
	prompt := &testPrompter{confirmed: true}
	f, stdout, _ := newRepoTestFactory(cfg, prompt)

	cmd := newDeleteCmd(f)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"old-service"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !prompt.called {
		t.Fatal("expected confirmation prompt")
	}
	if prompt.prompt != "Proceed?" {
		t.Fatalf("prompt = %q, want Proceed?", prompt.prompt)
	}
	if !deleted {
		t.Fatal("expected delete request")
	}
	out := stdout.String()
	for _, want := range []string{
		`Delete repository "Old Service" from workspace "ws"?`,
		"branch: main",
		"date:   2026-06-18T14:22:00Z",
		"title:  chore: archive legacy pipeline",
		`Deleted repository "old-service" from workspace "ws".`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRepoDeleteCloudJSONOutputWithYes(t *testing.T) {
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/context-repo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"slug":       "context-repo",
				"name":       "Context Repo",
				"uuid":       "{abc}",
				"mainbranch": map[string]any{"name": "trunk"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/context-repo/commits":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{{
					"hash":    "abc123",
					"date":    "2026-06-18T14:22:00Z",
					"message": "feat: keep me",
				}},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/repositories/ws/context-repo":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{
		ActiveContext: "default",
		Contexts: map[string]*config.Context{
			"default": {Host: "main", Workspace: "ws", DefaultRepo: "context-repo"},
		},
		Hosts: map[string]*config.Host{
			"main": {Kind: "cloud", BaseURL: server.URL, Token: "test-token"},
		},
	}
	prompt := &testPrompter{confirmed: false}
	f, stdout, _ := newRepoTestFactory(cfg, prompt)

	cmd := newDeleteCmd(f)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.PersistentFlags().Bool("json", false, "")
	cmd.SetArgs([]string{"--yes", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if prompt.called {
		t.Fatal("expected --yes to skip prompt")
	}
	if !deleted {
		t.Fatal("expected delete request")
	}

	var payload struct {
		Workspace  string `json:"workspace"`
		Repository string `json:"repository"`
		Name       string `json:"name"`
		Deleted    bool   `json:"deleted"`
		LastCommit struct {
			Branch  string `json:"branch"`
			Date    string `json:"date"`
			Message string `json:"message"`
		} `json:"last_commit"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &payload); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, stdout.String())
	}
	if !payload.Deleted || payload.Repository != "context-repo" || payload.Name != "Context Repo" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.LastCommit.Branch != "trunk" || payload.LastCommit.Message != "feat: keep me" {
		t.Fatalf("unexpected last commit payload: %+v", payload.LastCommit)
	}
}

func TestRepoDeleteCloudAbortsOnDecline(t *testing.T) {
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/repo":
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": "repo", "name": "Repo", "uuid": "{abc}"})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/repo/refs/branches":
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{{"name": "main", "default": true}}})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/repo/commits":
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{{"message": "feat: test", "date": "2026-01-01T00:00:00Z"}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/repositories/ws/repo":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{
		ActiveContext: "default",
		Contexts: map[string]*config.Context{
			"default": {Host: "main", Workspace: "ws", DefaultRepo: "repo"},
		},
		Hosts: map[string]*config.Host{
			"main": {Kind: "cloud", BaseURL: server.URL, Token: "test-token"},
		},
	}
	prompt := &testPrompter{confirmed: false}
	f, stdout, _ := newRepoTestFactory(cfg, prompt)

	cmd := newDeleteCmd(f)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !prompt.called {
		t.Fatal("expected prompt")
	}
	if deleted {
		t.Fatal("did not expect delete request")
	}
	if !strings.Contains(stdout.String(), "Aborted.") {
		t.Fatalf("expected abort output, got %q", stdout.String())
	}
}

func TestRepoDeleteCloudContinuesWhenCommitLookupFails(t *testing.T) {
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/repo":
			_ = json.NewEncoder(w).Encode(map[string]any{"slug": "repo", "name": "Repo", "uuid": "{abc}", "mainbranch": map[string]any{"name": "main"}})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/repo/commits":
			http.Error(w, "boom", http.StatusInternalServerError)
		case r.Method == http.MethodDelete && r.URL.Path == "/repositories/ws/repo":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{
		ActiveContext: "default",
		Contexts: map[string]*config.Context{
			"default": {Host: "main", Workspace: "ws", DefaultRepo: "repo"},
		},
		Hosts: map[string]*config.Host{
			"main": {Kind: "cloud", BaseURL: server.URL, Token: "test-token"},
		},
	}
	prompt := &testPrompter{confirmed: true}
	f, stdout, _ := newRepoTestFactory(cfg, prompt)

	cmd := newDeleteCmd(f)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !deleted {
		t.Fatal("expected delete request")
	}
	out := stdout.String()
	if strings.Contains(out, "Last commit:") {
		t.Fatalf("did not expect commit summary when lookup fails:\n%s", out)
	}
	if !strings.Contains(out, `Delete repository "Repo" from workspace "ws"?`) {
		t.Fatalf("expected basic confirmation output, got:\n%s", out)
	}
}
