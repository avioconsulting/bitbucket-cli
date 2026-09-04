package group

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/avivsinai/bitbucket-cli/internal/config"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
	"github.com/avivsinai/bitbucket-cli/pkg/iostreams"
	"github.com/avivsinai/bitbucket-cli/pkg/prompter"
)

type testPrompter struct {
	confirmed bool
	called    bool
}

func (p *testPrompter) Input(string, string) (string, error) { return "", nil }
func (p *testPrompter) Password(string) (string, error)      { return "", nil }
func (p *testPrompter) Confirm(string, bool) (bool, error) {
	p.called = true
	return p.confirmed, nil
}

var _ prompter.Interface = (*testPrompter)(nil)

func groupTestFactory(baseURL string, prompt prompter.Interface) (*cmdutil.Factory, *bytes.Buffer) {
	out := &bytes.Buffer{}
	f := &cmdutil.Factory{
		AppVersion:     "test",
		ExecutableName: "bkt",
		IOStreams: &iostreams.IOStreams{
			In:     io.NopCloser(bytes.NewReader(nil)),
			Out:    out,
			ErrOut: &bytes.Buffer{},
		},
		Config: func() (*config.Config, error) {
			return &config.Config{
				ActiveContext: "test",
				Contexts:      map[string]*config.Context{"test": {Host: "mock", Workspace: "ws"}},
				Hosts:         map[string]*config.Host{"mock": {Kind: "cloud", BaseURL: baseURL, Username: "admin", Token: "token"}},
			}, nil
		},
		Prompter: prompt,
	}
	return f, out
}

func executeGroupCommand(t *testing.T, cmd *cobra.Command, f *cmdutil.Factory, args ...string) error {
	t.Helper()
	cmd.PersistentFlags().String("context", "", "Named context")
	cmd.PersistentFlags().Bool("json", false, "JSON output")
	cmd.PersistentFlags().Bool("yaml", false, "YAML output")
	cmd.PersistentFlags().String("format", "", "Output format")
	cmd.PersistentFlags().String("jq", "", "jq expression")
	cmd.PersistentFlags().String("template", "", "Template")
	cmd.SetArgs(args)
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	return cmd.ExecuteContext(context.Background())
}

func TestGroupLegacyRoutes(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "list", args: []string{"list"}, want: "GET /1.0/groups/ws/"},
		{name: "create", args: []string{"create", "Developers"}, want: "POST /1.0/groups/ws/"},
		{name: "members", args: []string{"member", "list", "developers"}, want: "GET /1.0/groups/ws/developers/members"},
		{name: "add", args: []string{"member", "add", "developers", "--user", "557058:12345678-1234-1234-1234-123456789abc"}, want: "PUT /1.0/groups/ws/developers/members/557058:12345678-1234-1234-1234-123456789abc"},
		{name: "remove", args: []string{"member", "remove", "developers", "--user", "557058:12345678-1234-1234-1234-123456789abc"}, want: "DELETE /1.0/groups/ws/developers/members/557058:12345678-1234-1234-1234-123456789abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests = nil
			f, _ := groupTestFactory(srv.URL, nil)
			if err := executeGroupCommand(t, NewCommand(f), f, tt.args...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(requests) != 1 || requests[0] != tt.want {
				t.Fatalf("requests = %v, want %q", requests, tt.want)
			}
		})
	}
}

func TestInviteLegacyRoute(t *testing.T) {
	var body map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/1.0/users/ws/invitations" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	t.Cleanup(srv.Close)

	f, _ := groupTestFactory(srv.URL, nil)
	if err := executeGroupCommand(t, NewInviteCommand(f), f, "person@example.com", "--group", "developers"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["email"] != "person@example.com" || body["group_slug"] != "developers" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestGroupDeleteConfirmsAndSupportsYes(t *testing.T) {
	var deletes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/1.0/groups/ws/developers/" {
			deletes++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	prompt := &testPrompter{confirmed: false}
	f, out := groupTestFactory(srv.URL, prompt)
	if err := executeGroupCommand(t, NewCommand(f), f, "delete", "developers"); err != nil {
		t.Fatalf("unexpected decline error: %v", err)
	}
	if !prompt.called || deletes != 0 || !strings.Contains(out.String(), "Aborted.") {
		t.Fatalf("prompt=%v deletes=%d output=%q", prompt.called, deletes, out.String())
	}

	f, _ = groupTestFactory(srv.URL, &testPrompter{})
	if err := executeGroupCommand(t, NewCommand(f), f, "delete", "developers", "--yes"); err != nil {
		t.Fatalf("unexpected --yes error: %v", err)
	}
	if deletes != 1 {
		t.Fatalf("deletes = %d, want 1", deletes)
	}
}

func TestGroupRejectsDataCenter(t *testing.T) {
	f, _ := groupTestFactory("http://localhost", nil)
	cfg, _ := f.Config()
	cfg.Hosts["mock"].Kind = "dc"
	f.Config = func() (*config.Config, error) { return cfg, nil }
	err := executeGroupCommand(t, NewCommand(f), f, "list")
	if err == nil || !strings.Contains(err.Error(), "Cloud") {
		t.Fatalf("unexpected error: %v", err)
	}
}
