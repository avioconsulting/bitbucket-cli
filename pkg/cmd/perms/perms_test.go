package perms

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avivsinai/bitbucket-cli/internal/config"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
	"github.com/avivsinai/bitbucket-cli/pkg/iostreams"
)

func newTestFactory(cfg *config.Config) (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer) {
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

func runPermsCmd(t *testing.T, f *cmdutil.Factory, args ...string) error {
	t.Helper()

	cmd := NewCommand(f)
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
			"test": {Host: "mock", ProjectKey: "MYPROJ"},
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

func TestProjectListCloud(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/WEB/permissions-config/users":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{
						"type":       "project_user_permission",
						"permission": "admin",
						"user": map[string]any{
							"type":         "user",
							"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
							"display_name": "Jane Doe",
						},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/projects/WEB/permissions-config/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{
						"type":       "project_group_permission",
						"permission": "write",
						"group": map[string]any{
							"type": "group",
							"name": "Developers",
							"slug": "developers",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, stdout, stderr := newTestFactory(cloudConfig(srv.URL))
	if err := runPermsCmd(t, f, "project", "list", "--workspace", "ws", "--project", "WEB"); err != nil {
		t.Fatalf("unexpected error: %v (stderr=%s)", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "USER\tJane Doe") {
		t.Fatalf("expected user row, got: %s", out)
	}
	if !strings.Contains(out, "GROUP\tDevelopers\tdevelopers\twrite") {
		t.Fatalf("expected group row, got: %s", out)
	}
}

func TestProjectListCloudEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{}})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runPermsCmd(t, f, "project", "list", "--workspace", "ws", "--project", "WEB"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No permissions found.") {
		t.Fatalf("expected empty message, got: %s", stdout.String())
	}
}

func TestProjectGrantCloudUser(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/members":
			q := r.URL.Query().Get("q")
			if !strings.Contains(q, "user.email IN") {
				t.Fatalf("expected email filter, got %q", q)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{
						"type": "workspace_membership",
						"user": map[string]any{
							"type":         "user",
							"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
							"display_name": "Jane Doe",
							"email":        "jane@example.com",
						},
					},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/workspaces/ws/projects/WEB/permissions-config/users/557058:12345678-1234-1234-1234-123456789abc":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":       "project_user_permission",
				"permission": "admin",
				"user": map[string]any{
					"type":         "user",
					"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
					"display_name": "Jane Doe",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runPermsCmd(t, f, "project", "grant", "--workspace", "ws", "--project", "WEB", "--user", "jane@example.com", "--perm", "admin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["permission"] != "admin" {
		t.Fatalf("expected admin permission, got %v", gotBody)
	}
	if !strings.Contains(stdout.String(), "Granted admin on project WEB to user jane@example.com") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestProjectGrantCloudGroup(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut && r.URL.Path == "/workspaces/ws/projects/WEB/permissions-config/groups/developers" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":       "project_group_permission",
				"permission": "write",
				"group": map[string]any{
					"type": "group",
					"name": "Developers",
					"slug": "developers",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runPermsCmd(t, f, "project", "grant", "--workspace", "ws", "--project", "WEB", "--group", "developers", "--perm", "write"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["permission"] != "write" {
		t.Fatalf("expected write permission, got %v", gotBody)
	}
	if !strings.Contains(stdout.String(), "Granted write on project WEB to group developers") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestProjectRevokeCloudUser(t *testing.T) {
	var deletedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/members":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{
						"type": "workspace_membership",
						"user": map[string]any{
							"type":         "user",
							"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
							"display_name": "Jane Doe",
							"email":        "jane@example.com",
						},
					},
				},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/workspaces/ws/projects/WEB/permissions-config/users/557058:12345678-1234-1234-1234-123456789abc":
			deletedPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runPermsCmd(t, f, "project", "revoke", "--workspace", "ws", "--project", "WEB", "--user", "jane@example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deletedPath == "" {
		t.Fatal("expected delete request")
	}
	if !strings.Contains(stdout.String(), "Revoked user permission for jane@example.com on project WEB") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestProjectRevokeCloudGroup(t *testing.T) {
	var deletedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/workspaces/ws/projects/WEB/permissions-config/groups/developers" {
			deletedPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runPermsCmd(t, f, "project", "revoke", "--workspace", "ws", "--project", "WEB", "--group", "developers"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deletedPath == "" {
		t.Fatal("expected delete request")
	}
	if !strings.Contains(stdout.String(), "Revoked group permission for developers on project WEB") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestRepoListCloud(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/my-service/permissions-config/users":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{
						"type":       "repository_user_permission",
						"permission": "admin",
						"user": map[string]any{
							"type":         "user",
							"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
							"display_name": "Jane Doe",
						},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/repositories/ws/my-service/permissions-config/groups":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{
						"type":       "repository_group_permission",
						"permission": "read",
						"group": map[string]any{
							"type": "group",
							"name": "Developers",
							"slug": "developers",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runPermsCmd(t, f, "repo", "list", "--workspace", "ws", "--repo", "my-service"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "USER\tJane Doe") {
		t.Fatalf("expected user row, got: %s", out)
	}
	if !strings.Contains(out, "GROUP\tDevelopers\tdevelopers\tread") {
		t.Fatalf("expected group row, got: %s", out)
	}
}

func TestRepoGrantCloudUserByAccountID(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut && r.URL.Path == "/repositories/ws/my-service/permissions-config/users/557058:12345678-1234-1234-1234-123456789abc" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":       "repository_user_permission",
				"permission": "admin",
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runPermsCmd(t, f, "repo", "grant", "--workspace", "ws", "--repo", "my-service", "--user", "557058:12345678-1234-1234-1234-123456789abc", "--perm", "admin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["permission"] != "admin" {
		t.Fatalf("expected admin permission, got %v", gotBody)
	}
	if !strings.Contains(stdout.String(), "Granted admin on repository my-service to user 557058:12345678-1234-1234-1234-123456789abc") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestRepoGrantCloudGroup(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPut && r.URL.Path == "/repositories/ws/my-service/permissions-config/groups/developers" {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":       "repository_group_permission",
				"permission": "read",
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runPermsCmd(t, f, "repo", "grant", "--workspace", "ws", "--repo", "my-service", "--group", "developers", "--perm", "read"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["permission"] != "read" {
		t.Fatalf("expected read permission, got %v", gotBody)
	}
	if !strings.Contains(stdout.String(), "Granted read on repository my-service to group developers") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestRepoRevokeCloudUser(t *testing.T) {
	var deletedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/workspaces/ws/members":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{
						"type": "workspace_membership",
						"user": map[string]any{
							"type":         "user",
							"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
							"display_name": "Jane Doe",
							"email":        "jane@example.com",
						},
					},
				},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/repositories/ws/my-service/permissions-config/users/557058:12345678-1234-1234-1234-123456789abc":
			deletedPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(cloudConfig(srv.URL))
	if err := runPermsCmd(t, f, "repo", "revoke", "--workspace", "ws", "--repo", "my-service", "--user", "jane@example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deletedPath == "" {
		t.Fatal("expected delete request")
	}
	if !strings.Contains(stdout.String(), "Revoked user permission for jane@example.com on repository my-service") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestProjectListDC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/rest/api/1.0/projects/MYPROJ/permissions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"size":       1,
			"limit":      100,
			"isLastPage": true,
			"start":      0,
			"values": []map[string]any{
				{
					"user": map[string]any{
						"name":        "jdoe",
						"displayName": "John Doe",
					},
					"permission": "PROJECT_ADMIN",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	f, stdout, _ := newTestFactory(dcConfig(srv.URL))
	if err := runPermsCmd(t, f, "project", "list", "--project", "MYPROJ"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "John Doe\tPROJECT_ADMIN") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestRepoListDCRequiresProject(t *testing.T) {
	f, _, _ := newTestFactory(dcConfig("http://localhost"))
	err := runPermsCmd(t, f, "repo", "list", "--repo", "my-service")
	if err == nil {
		t.Fatal("expected error for missing project")
	}
	if !strings.Contains(err.Error(), "--project is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectGrantCloudRequiresUserOrGroup(t *testing.T) {
	f, _, _ := newTestFactory(cloudConfig("http://localhost"))
	err := runPermsCmd(t, f, "project", "grant", "--workspace", "ws", "--project", "WEB", "--perm", "admin")
	if err == nil {
		t.Fatal("expected error for missing user/group")
	}
	if !strings.Contains(err.Error(), "specify either --user or --group") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectGrantCloudRejectsBothUserAndGroup(t *testing.T) {
	f, _, _ := newTestFactory(cloudConfig("http://localhost"))
	err := runPermsCmd(t, f, "project", "grant", "--workspace", "ws", "--project", "WEB", "--user", "a", "--group", "b", "--perm", "admin")
	if err == nil {
		t.Fatal("expected error for both user and group")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectGrantDCUsesDefaultPermission(t *testing.T) {
	var permission string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		permission = r.URL.Query().Get("permission")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	f, _, _ := newTestFactory(dcConfig(srv.URL))
	if err := runPermsCmd(t, f, "project", "grant", "--project", "MYPROJ", "--user", "jdoe"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if permission != "PROJECT_READ" {
		t.Fatalf("permission = %q, want PROJECT_READ", permission)
	}
}

func TestRepoGrantDCUsesDefaultPermission(t *testing.T) {
	var permission string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		permission = r.URL.Query().Get("permission")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	f, _, _ := newTestFactory(dcConfig(srv.URL))
	if err := runPermsCmd(t, f, "repo", "grant", "--project", "MYPROJ", "--repo", "service", "--user", "jdoe"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if permission != "REPO_READ" {
		t.Fatalf("permission = %q, want REPO_READ", permission)
	}
}

func TestDataCenterMutationsRequireUserAndRejectGroup(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "project grant missing user", args: []string{"project", "grant", "--project", "MYPROJ"}},
		{name: "project revoke missing user", args: []string{"project", "revoke", "--project", "MYPROJ"}},
		{name: "repo grant missing user", args: []string{"repo", "grant", "--project", "MYPROJ", "--repo", "service"}},
		{name: "repo revoke missing user", args: []string{"repo", "revoke", "--project", "MYPROJ", "--repo", "service"}},
		{name: "project group", args: []string{"project", "grant", "--project", "MYPROJ", "--group", "developers"}},
		{name: "repo group", args: []string{"repo", "revoke", "--project", "MYPROJ", "--repo", "service", "--group", "developers"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _, _ := newTestFactory(dcConfig("http://localhost"))
			err := runPermsCmd(t, f, tt.args...)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), "--user") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCloudPermissionMutationsRejectOAuth(t *testing.T) {
	tests := [][]string{
		{"project", "grant", "--project", "WEB", "--group", "developers"},
		{"project", "revoke", "--project", "WEB", "--group", "developers"},
		{"repo", "grant", "--repo", "service", "--group", "developers"},
		{"repo", "revoke", "--repo", "service", "--group", "developers"},
	}

	for _, args := range tests {
		cfg := cloudConfig("http://localhost")
		cfg.Hosts["mock"].AuthMethod = "oauth"
		f, _, _ := newTestFactory(cfg)
		err := runPermsCmd(t, f, args...)
		if err == nil || !strings.Contains(err.Error(), "do not support OAuth") {
			t.Fatalf("args %v: unexpected error: %v", args, err)
		}
	}
}
