package project

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/avivsinai/bitbucket-cli/internal/config"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
	"github.com/avivsinai/bitbucket-cli/pkg/iostreams"
	"github.com/avivsinai/bitbucket-cli/pkg/prompter"
)

type testPrompter struct {
	confirmed bool
	called    bool
	prompt    string
}

func (p *testPrompter) Input(string, string) (string, error) { return "", nil }
func (p *testPrompter) Password(string) (string, error)      { return "", nil }
func (p *testPrompter) Confirm(prompt string, _ bool) (bool, error) {
	p.called = true
	p.prompt = prompt
	return p.confirmed, nil
}

var _ prompter.Interface = (*testPrompter)(nil)

func newTestFactory(cfg *config.Config) (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer) {
	return newTestFactoryWithPrompt(cfg, nil)
}

func newTestFactoryWithPrompt(cfg *config.Config, prompt prompter.Interface) (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer) {
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
		Prompter: prompt,
	}
	return f, stdout, stderr
}

func runProjectCmd(t *testing.T, f *cmdutil.Factory, args ...string) error {
	t.Helper()

	cmd := NewCmdProject(f)
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

func dcConfig(baseURL string) *config.Config {
	return &config.Config{
		ActiveContext: "test",
		Contexts: map[string]*config.Context{
			"test": {Host: "mock", ProjectKey: "PROJ"},
		},
		Hosts: map[string]*config.Host{
			"mock": {Kind: "dc", BaseURL: baseURL, Username: "admin", Token: "token"},
		},
	}
}

func cloudConfig(baseURL string) *config.Config {
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

func TestProjectList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/rest/api/1.0/projects") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"size":       2,
			"limit":      25,
			"isLastPage": true,
			"start":      0,
			"values": []map[string]any{
				{
					"id":          1,
					"key":         "team",
					"name":        "Team Project",
					"description": "  Team space  ",
					"public":      true,
					"type":        "NORMAL",
				},
				{
					"id":     2,
					"key":    "ops",
					"name":   "Operations",
					"public": false,
					"type":   "NORMAL",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, stderr := newTestFactory(dcConfig(srv.URL))
	if err := runProjectCmd(t, f, "list"); err != nil {
		t.Fatalf("unexpected error: %v (stderr=%s)", err, stderr.String())
	}

	out := stdout.String()
	// Key is uppercased in the formatter.
	if !strings.Contains(out, "TEAM\tTeam Project") {
		t.Errorf("expected TEAM row, got: %s", out)
	}
	if !strings.Contains(out, "OPS\tOperations") {
		t.Errorf("expected OPS row, got: %s", out)
	}
	// Web URL uses the uppercased key.
	if !strings.Contains(out, srv.URL+"/projects/TEAM") {
		t.Errorf("expected TEAM project link, got: %s", out)
	}
	// Description should be trimmed.
	if !strings.Contains(out, "desc: Team space") {
		t.Errorf("expected trimmed description, got: %s", out)
	}
	// Visibility only printed for public projects.
	if !strings.Contains(out, "visibility: public") {
		t.Errorf("expected 'visibility: public' for TEAM, got: %s", out)
	}
	// Private projects omit the visibility line.
	if strings.Count(out, "visibility: public") != 1 {
		t.Errorf("expected exactly one 'visibility: public' line, got: %s", out)
	}
}

func TestProjectListEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"size":       0,
			"limit":      25,
			"isLastPage": true,
			"start":      0,
			"values":     []any{},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(dcConfig(srv.URL))
	if err := runProjectCmd(t, f, "list"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No projects visible") {
		t.Errorf("expected empty message, got: %s", stdout.String())
	}
}

func TestProjectListCloud(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspaces/ws/projects" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{
					"uuid":        "{abc}",
					"key":         "WEB",
					"name":        "Web Platform",
					"description": "  Frontend services  ",
					"is_private":  false,
					"links": map[string]any{
						"html": map[string]any{"href": "https://bitbucket.org/ws/workspace/projects/WEB"},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, stderr := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "list"); err != nil {
		t.Fatalf("unexpected error: %v (stderr=%s)", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Projects in workspace ws", "WEB\tWeb Platform", "desc: Frontend services", "visibility: public"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestProjectViewCloud(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/workspaces/ws/projects/WEB" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"uuid":        "{abc}",
			"key":         "WEB",
			"name":        "Web Platform",
			"description": "Frontend services",
			"is_private":  true,
			"created_on":  "2026-01-01T00:00:00Z",
			"updated_on":  "2026-06-01T00:00:00Z",
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "view", "web"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"WEB\tWeb Platform", "desc: Frontend services", "visibility: private", "created: 2026-01-01T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestProjectReposCloudFiltersByProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repositories/ws" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("q"); got != `project.key="WEB"` {
			t.Fatalf("q = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"slug": "web-api", "name": "Web API", "uuid": "{1}", "project": map[string]any{"key": "WEB"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "repos", "WEB", "--limit", "0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "ws/web-api\tWeb API") {
		t.Fatalf("expected web-api row, got:\n%s", out)
	}
	if strings.Contains(out, "data-api") {
		t.Fatalf("did not expect data-api row, got:\n%s", out)
	}
}

func TestProjectCreateCloud(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/workspaces/ws/projects" {
			t.Fatalf("path = %q", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["key"] != "WEB" || body["name"] != "Web Platform" || body["description"] != "Frontend services" || body["is_private"] != true {
			t.Fatalf("unexpected body: %v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":        "WEB",
			"name":       "Web Platform",
			"is_private": true,
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "create", "WEB", "--workspace", "ws", "--name", "Web Platform", "--description", "Frontend services"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Created project ws/WEB (Web Platform).") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestProjectCreateCloudDefaultsNameToKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "WEB" {
			t.Fatalf("expected name to default to key, got %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "WEB", "name": "WEB"})
	}))
	t.Cleanup(srv.Close)

	f, _, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "create", "WEB"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectCreateCloudRejectsDataCenter(t *testing.T) {
	f, _, _ := newTestFactory(dcConfig("http://localhost"))
	err := runProjectCmd(t, f, "create", "WEB")
	if err == nil {
		t.Fatal("expected error on DC host")
	}
	if !strings.Contains(err.Error(), "Cloud contexts only") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectDeleteCloudRejectsNonEmptyProject(t *testing.T) {
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/WEB":
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "WEB", "name": "Web Platform"})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws":
			if got := r.URL.Query().Get("q"); got != `project.key="WEB"` {
				t.Fatalf("q = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{{"slug": "web-api", "project": map[string]any{"key": "WEB"}}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/workspaces/ws/projects/WEB":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, _, _ := newTestFactoryWithPrompt(cloudConfig(srv.URL), &testPrompter{confirmed: true})
	err := runProjectCmd(t, f, "delete", "WEB", "--yes")
	if err == nil {
		t.Fatal("expected non-empty project error")
	}
	if !strings.Contains(err.Error(), "use --cascade") {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted {
		t.Fatal("did not expect delete request")
	}
}

func TestProjectDeleteCloudDeletesEmptyProjectWithYes(t *testing.T) {
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/WEB":
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "WEB", "name": "Web Platform"})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws":
			if got := r.URL.Query().Get("q"); got != `project.key="WEB"` {
				t.Fatalf("q = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/workspaces/ws/projects/WEB":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	prompt := &testPrompter{confirmed: false}
	f, stdout, _ := newTestFactoryWithPrompt(cloudConfig(srv.URL), prompt)
	if err := runProjectCmd(t, f, "delete", "WEB", "--yes"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt.called {
		t.Fatal("expected --yes to skip prompt")
	}
	if !deleted {
		t.Fatal("expected delete request")
	}
	if !strings.Contains(stdout.String(), `Deleted project "WEB" from workspace "ws".`) {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestProjectDeleteCloudCascadeDeletesRepositories(t *testing.T) {
	var projectDeleted bool
	deletedRepos := make(map[string]bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/WEB":
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "WEB", "name": "Web Platform"})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws":
			if got := r.URL.Query().Get("q"); got != `project.key="WEB"` {
				t.Fatalf("q = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{
				{"slug": "web-api", "name": "Web API", "project": map[string]any{"key": "WEB"}},
				{"slug": "web-ui", "name": "Web UI", "project": map[string]any{"key": "WEB"}},
			}})
		case r.Method == http.MethodDelete && r.URL.Path == "/repositories/ws/web-api":
			deletedRepos["web-api"] = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/repositories/ws/web-ui":
			deletedRepos["web-ui"] = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/workspaces/ws/projects/WEB":
			projectDeleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	prompt := &testPrompter{confirmed: true}
	f, stdout, _ := newTestFactoryWithPrompt(cloudConfig(srv.URL), prompt)
	if err := runProjectCmd(t, f, "delete", "WEB", "--cascade"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !prompt.called {
		t.Fatal("expected prompt")
	}
	if !deletedRepos["web-api"] || !deletedRepos["web-ui"] {
		t.Fatalf("expected repositories to be deleted, got: %v", deletedRepos)
	}
	if !projectDeleted {
		t.Fatal("expected project delete request")
	}
	out := stdout.String()
	if !strings.Contains(out, "This will also delete 2 repositories") {
		t.Fatalf("expected cascade warning, got:\n%s", out)
	}
	if !strings.Contains(out, "Deleted project \"WEB\" and 2 repositories") {
		t.Fatalf("expected cascade summary, got:\n%s", out)
	}
}

func TestProjectDeleteCloudCascadeStopsOnRepoError(t *testing.T) {
	var projectDeleted bool
	var repoDeleteCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/WEB":
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "WEB", "name": "Web Platform"})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws":
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{
				{"slug": "web-api", "name": "Web API", "project": map[string]any{"key": "WEB"}},
				{"slug": "web-ui", "name": "Web UI", "project": map[string]any{"key": "WEB"}},
			}})
		case r.Method == http.MethodDelete && r.URL.Path == "/repositories/ws/web-api":
			repoDeleteCount++
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/workspaces/ws/projects/WEB":
			projectDeleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, _, _ := newTestFactoryWithPrompt(cloudConfig(srv.URL), &testPrompter{confirmed: true})
	err := runProjectCmd(t, f, "delete", "WEB", "--cascade", "--yes")
	if err == nil {
		t.Fatal("expected error when repo delete fails")
	}
	if !strings.Contains(err.Error(), "failed to delete repository \"web-ui\"") {
		t.Fatalf("unexpected error: %v", err)
	}
	if projectDeleted {
		t.Fatal("did not expect project delete after repo failure")
	}
	if repoDeleteCount != 1 {
		t.Fatalf("expected one repo deleted before failure, got %d", repoDeleteCount)
	}
}

func TestProjectDeleteCloudAbortsOnDecline(t *testing.T) {
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/WEB":
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "WEB", "name": "Web Platform"})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws":
			if got := r.URL.Query().Get("q"); got != `project.key="WEB"` {
				t.Fatalf("q = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/workspaces/ws/projects/WEB":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	prompt := &testPrompter{confirmed: false}
	f, stdout, _ := newTestFactoryWithPrompt(cloudConfig(srv.URL), prompt)
	if err := runProjectCmd(t, f, "delete", "WEB"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !prompt.called {
		t.Fatal("expected prompt")
	}
	if deleted {
		t.Fatal("did not expect delete request")
	}
	if !strings.Contains(stdout.String(), "Aborted.") {
		t.Fatalf("expected abort output, got: %s", stdout.String())
	}
}

func TestProjectRenameCloud(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW":
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "DFW Airport"})
		case r.Method == http.MethodPut && r.URL.Path == "/workspaces/ws/projects/DFW":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "ZZ - Archived - DFW Airport"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "rename", "DFW", "--name", "ZZ - Archived - DFW Airport", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBody["name"] != "ZZ - Archived - DFW Airport" {
		t.Fatalf("expected rename body, got %v", gotBody)
	}
	if gotBody["description"] != nil {
		t.Fatalf("did not expect description in body, got %v", gotBody)
	}

	var result struct {
		Workspace string `json:"workspace"`
		Project   string `json:"project_key"`
		OldName   string `json:"old_name"`
		NewName   string `json:"new_name"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if result.Project != "DFW" || result.OldName != "DFW Airport" || result.NewName != "ZZ - Archived - DFW Airport" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProjectRenameCloudUpdatesDescription(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW" {
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "DFW Airport"})
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/workspaces/ws/projects/DFW" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "DFW Airport v2", "description": "legacy"})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	f, _, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "rename", "DFW", "--name", "DFW Airport v2", "--description", "legacy"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["description"] != "legacy" {
		t.Fatalf("expected description in body, got %v", gotBody)
	}
}

func TestProjectRenameCloudRejectsDataCenter(t *testing.T) {
	f, _, _ := newTestFactory(dcConfig("http://localhost"))
	err := runProjectCmd(t, f, "rename", "DFW", "--name", "Archived")
	if err == nil {
		t.Fatal("expected error on DC host")
	}
	if !strings.Contains(err.Error(), "Cloud contexts only") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectRenameCloudRequiresName(t *testing.T) {
	f, _, _ := newTestFactory(cloudConfig("http://localhost"))
	err := runProjectCmd(t, f, "rename", "DFW")
	if err == nil {
		t.Fatal("expected error when name is missing")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectArchiveCloud(t *testing.T) {
	var gotBody map[string]any
	var gotPermissionBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW":
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "DFW Airport"})
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW/permissions-config/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{
				{"permission": "write", "group": map[string]any{"slug": "contributors"}},
				{"permission": "admin", "group": map[string]any{"slug": "administrators"}},
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/workspaces/ws/projects/DFW":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "ZZ - Archived - DFW Airport"})
		case r.Method == http.MethodPut && r.URL.Path == "/workspaces/ws/projects/DFW/permissions-config/groups/contributors":
			if err := json.NewDecoder(r.Body).Decode(&gotPermissionBody); err != nil {
				t.Fatalf("decode permission body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"permission": "read", "group": map[string]any{"slug": "contributors"}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "archive", "DFW", "--json"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBody["name"] != "ZZ - Archived - DFW Airport" {
		t.Fatalf("expected archive name, got %v", gotBody)
	}

	var result struct {
		Workspace        string   `json:"workspace"`
		Project          string   `json:"project_key"`
		OldName          string   `json:"old_name"`
		NewName          string   `json:"new_name"`
		Prefix           string   `json:"prefix"`
		DowngradedGroups []string `json:"downgraded_groups"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, stdout.String())
	}
	if result.Project != "DFW" || result.OldName != "DFW Airport" || result.NewName != "ZZ - Archived - DFW Airport" || result.Prefix != "ZZ - Archived - " {
		t.Fatalf("unexpected result: %+v", result)
	}
	if gotPermissionBody["permission"] != "read" || !reflect.DeepEqual(result.DowngradedGroups, []string{"contributors"}) {
		t.Fatalf("unexpected permission downgrade: body=%v groups=%v", gotPermissionBody, result.DowngradedGroups)
	}
}

func TestProjectArchiveCloudCustomPrefix(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW" {
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "DFW Airport"})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW/permissions-config/groups" {
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{}})
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/workspaces/ws/projects/DFW" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "Legacy - DFW Airport"})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	f, _, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "archive", "DFW", "--prefix", "Legacy - "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["name"] != "Legacy - DFW Airport" {
		t.Fatalf("expected custom prefix name, got %v", gotBody)
	}
}

func TestProjectArchiveCloudUsesKeyWhenNameEmpty(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW" {
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": ""})
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW/permissions-config/groups" {
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{}})
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/workspaces/ws/projects/DFW" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "ZZ - Archived - DFW"})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	f, _, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "archive", "DFW"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["name"] != "ZZ - Archived - DFW" {
		t.Fatalf("expected key fallback name, got %v", gotBody)
	}
}

func TestProjectArchiveCloudRejectsDataCenter(t *testing.T) {
	f, _, _ := newTestFactory(dcConfig("http://localhost"))
	err := runProjectCmd(t, f, "archive", "DFW")
	if err == nil {
		t.Fatal("expected error on DC host")
	}
	if !strings.Contains(err.Error(), "Cloud contexts only") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectArchiveDoesNotRenameWhenPermissionDowngradeFails(t *testing.T) {
	var renamed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW":
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "DFW Airport"})
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW/permissions-config/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{
				{"permission": "write", "group": map[string]any{"slug": "contributors"}},
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/workspaces/ws/projects/DFW/permissions-config/groups/contributors":
			http.Error(w, `{"error":{"message":"forbidden"}}`, http.StatusForbidden)
		case r.Method == http.MethodPut && r.URL.Path == "/workspaces/ws/projects/DFW":
			renamed = true
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "ZZ - Archived - DFW Airport"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, _, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runProjectCmd(t, f, "archive", "DFW"); err == nil {
		t.Fatal("expected permission downgrade error")
	}
	if renamed {
		t.Fatal("project must not be renamed when a permission downgrade fails")
	}
}

func TestProjectArchiveRejectsOAuthBeforeMutation(t *testing.T) {
	var mutated bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW":
			_ = json.NewEncoder(w).Encode(map[string]any{"key": "DFW", "name": "DFW Airport"})
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/DFW/permissions-config/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{
				{"permission": "write", "group": map[string]any{"slug": "contributors"}},
			}})
		case r.Method == http.MethodPut:
			mutated = true
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := cloudConfig(srv.URL)
	cfg.Hosts["mock"].AuthMethod = "oauth"
	f, _, _ := newTestFactory(cfg)
	err := runProjectCmd(t, f, "archive", "DFW")
	if err == nil || !strings.Contains(err.Error(), "does not support OAuth") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mutated {
		t.Fatal("did not expect a mutation request")
	}
}
