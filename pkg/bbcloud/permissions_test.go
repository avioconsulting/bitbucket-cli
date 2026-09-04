package bbcloud

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestResolveWorkspaceUserByAccountID(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))

	user, err := client.ResolveWorkspaceUser(context.Background(), "ws", "557058:12345678-1234-1234-1234-123456789abc")
	if err != nil {
		t.Fatalf("ResolveWorkspaceUser: %v", err)
	}
	if user.AccountID != "557058:12345678-1234-1234-1234-123456789abc" {
		t.Fatalf("unexpected account id: %q", user.AccountID)
	}
	if user.UUID != "" {
		t.Fatalf("expected empty uuid for account id input, got %q", user.UUID)
	}
}

func TestResolveWorkspaceUserByUUID(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/members/{12345678-1234-1234-1234-123456789abc}" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "workspace_membership",
			"user": map[string]any{
				"type":         "user",
				"uuid":         "{12345678-1234-1234-1234-123456789abc}",
				"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
				"display_name": "Jane Doe",
			},
		})
	})

	client := newTestClient(t, handler)
	user, err := client.ResolveWorkspaceUser(context.Background(), "ws", "12345678-1234-1234-1234-123456789abc")
	if err != nil {
		t.Fatalf("ResolveWorkspaceUser: %v", err)
	}
	if user.AccountID != "557058:12345678-1234-1234-1234-123456789abc" {
		t.Fatalf("unexpected account id: %q", user.AccountID)
	}
	if user.Display != "Jane Doe" {
		t.Fatalf("unexpected display name: %q", user.Display)
	}
}

func TestResolveWorkspaceUserByEmail(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/members" {
			t.Fatalf("path = %q", got)
		}
		q := r.URL.Query().Get("q")
		if !stringsContains(q, `user.email IN`) {
			t.Fatalf("expected email filter, got %q", q)
		}
		fields := r.URL.Query().Get("fields")
		if !stringsContains(fields, "values.user.email") {
			t.Fatalf("expected fields to contain values.user.email, got %q", fields)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{
					"type": "workspace_membership",
					"user": map[string]any{
						"type":         "user",
						"uuid":         "{12345678-1234-1234-1234-123456789abc}",
						"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
						"display_name": "Jane Doe",
						"email":        "jane@example.com",
					},
				},
			},
		})
	})

	client := newTestClient(t, handler)
	user, err := client.ResolveWorkspaceUser(context.Background(), "ws", "jane@example.com")
	if err != nil {
		t.Fatalf("ResolveWorkspaceUser: %v", err)
	}
	if user.AccountID != "557058:12345678-1234-1234-1234-123456789abc" {
		t.Fatalf("unexpected account id: %q", user.AccountID)
	}
	if user.Email != "jane@example.com" {
		t.Fatalf("unexpected email: %q", user.Email)
	}
}

func stringsContains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestResolveWorkspaceUserByEmailNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{},
		})
	})

	client := newTestClient(t, handler)
	_, err := client.ResolveWorkspaceUser(context.Background(), "ws", "missing@example.com")
	if err == nil {
		t.Fatal("expected error for missing email")
	}
}

func TestResolveWorkspaceUserInvalid(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request")
	}))
	_, err := client.ResolveWorkspaceUser(context.Background(), "ws", "not-an-identifier")
	if err == nil {
		t.Fatal("expected error for invalid identifier")
	}
}

func TestResolveWorkspaceUserMissingWorkspace(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	_, err := client.ResolveWorkspaceUser(context.Background(), "", "557058:12345678-1234-1234-1234-123456789abc")
	if err == nil {
		t.Fatal("expected error for missing workspace")
	}
}

func TestListProjectUserPermissions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/projects/WEB/permissions-config/users" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{
					"type":       "project_user_permission",
					"permission": "admin",
					"user": map[string]any{
						"type":         "user",
						"uuid":         "{12345678-1234-1234-1234-123456789abc}",
						"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
						"display_name": "Jane Doe",
					},
				},
			},
		})
	})

	client := newTestClient(t, handler)
	perms, err := client.ListProjectUserPermissions(context.Background(), "ws", "WEB", 10)
	if err != nil {
		t.Fatalf("ListProjectUserPermissions: %v", err)
	}
	if len(perms) != 1 || perms[0].Permission != "admin" || perms[0].User.Display != "Jane Doe" {
		t.Fatalf("unexpected permissions: %+v", perms)
	}
}

func TestGrantProjectUserPermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/projects/WEB/permissions-config/users/557058:12345678-1234-1234-1234-123456789abc" {
			t.Fatalf("path = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["permission"] != "admin" {
			t.Fatalf("permission = %v", body["permission"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":       "project_user_permission",
			"permission": "admin",
			"user": map[string]any{
				"type":         "user",
				"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
				"display_name": "Jane Doe",
			},
		})
	})

	client := newTestClient(t, handler)
	perm, err := client.GrantProjectUserPermission(context.Background(), "ws", "WEB", "557058:12345678-1234-1234-1234-123456789abc", "admin")
	if err != nil {
		t.Fatalf("GrantProjectUserPermission: %v", err)
	}
	if perm.Permission != "admin" {
		t.Fatalf("unexpected permission: %q", perm.Permission)
	}
}

func TestRevokeProjectUserPermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/projects/WEB/permissions-config/users/557058:12345678-1234-1234-1234-123456789abc" {
			t.Fatalf("path = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	client := newTestClient(t, handler)
	if err := client.RevokeProjectUserPermission(context.Background(), "ws", "WEB", "557058:12345678-1234-1234-1234-123456789abc"); err != nil {
		t.Fatalf("RevokeProjectUserPermission: %v", err)
	}
}

func TestListProjectGroupPermissions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/projects/WEB/permissions-config/groups" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
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
	})

	client := newTestClient(t, handler)
	perms, err := client.ListProjectGroupPermissions(context.Background(), "ws", "WEB", 10)
	if err != nil {
		t.Fatalf("ListProjectGroupPermissions: %v", err)
	}
	if len(perms) != 1 || perms[0].Group.Slug != "developers" {
		t.Fatalf("unexpected permissions: %+v", perms)
	}
}

func TestGrantProjectGroupPermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/projects/WEB/permissions-config/groups/developers" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":       "project_group_permission",
			"permission": "write",
			"group": map[string]any{
				"type": "group",
				"name": "Developers",
				"slug": "developers",
			},
		})
	})

	client := newTestClient(t, handler)
	perm, err := client.GrantProjectGroupPermission(context.Background(), "ws", "WEB", "developers", "write")
	if err != nil {
		t.Fatalf("GrantProjectGroupPermission: %v", err)
	}
	if perm.Group.Slug != "developers" {
		t.Fatalf("unexpected group slug: %q", perm.Group.Slug)
	}
}

func TestRevokeProjectGroupPermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/projects/WEB/permissions-config/groups/developers" {
			t.Fatalf("path = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	client := newTestClient(t, handler)
	if err := client.RevokeProjectGroupPermission(context.Background(), "ws", "WEB", "developers"); err != nil {
		t.Fatalf("RevokeProjectGroupPermission: %v", err)
	}
}

func TestListRepoUserPermissions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/repositories/ws/my-service/permissions-config/users" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{
					"type":       "repository_user_permission",
					"permission": "write",
					"user": map[string]any{
						"type":         "user",
						"account_id":   "557058:12345678-1234-1234-1234-123456789abc",
						"display_name": "Jane Doe",
					},
				},
			},
		})
	})

	client := newTestClient(t, handler)
	perms, err := client.ListRepoUserPermissions(context.Background(), "ws", "my-service", 10)
	if err != nil {
		t.Fatalf("ListRepoUserPermissions: %v", err)
	}
	if len(perms) != 1 || perms[0].Permission != "write" {
		t.Fatalf("unexpected permissions: %+v", perms)
	}
}

func TestGrantRepoUserPermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/repositories/ws/my-service/permissions-config/users/557058:12345678-1234-1234-1234-123456789abc" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":       "repository_user_permission",
			"permission": "admin",
		})
	})

	client := newTestClient(t, handler)
	perm, err := client.GrantRepoUserPermission(context.Background(), "ws", "my-service", "557058:12345678-1234-1234-1234-123456789abc", "admin")
	if err != nil {
		t.Fatalf("GrantRepoUserPermission: %v", err)
	}
	if perm.Permission != "admin" {
		t.Fatalf("unexpected permission: %q", perm.Permission)
	}
}

func TestRevokeRepoUserPermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/repositories/ws/my-service/permissions-config/users/557058:12345678-1234-1234-1234-123456789abc" {
			t.Fatalf("path = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	client := newTestClient(t, handler)
	if err := client.RevokeRepoUserPermission(context.Background(), "ws", "my-service", "557058:12345678-1234-1234-1234-123456789abc"); err != nil {
		t.Fatalf("RevokeRepoUserPermission: %v", err)
	}
}

func TestListRepoGroupPermissions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/repositories/ws/my-service/permissions-config/groups" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
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
	})

	client := newTestClient(t, handler)
	perms, err := client.ListRepoGroupPermissions(context.Background(), "ws", "my-service", 10)
	if err != nil {
		t.Fatalf("ListRepoGroupPermissions: %v", err)
	}
	if len(perms) != 1 || perms[0].Group.Slug != "developers" {
		t.Fatalf("unexpected permissions: %+v", perms)
	}
}

func TestGrantRepoGroupPermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/repositories/ws/my-service/permissions-config/groups/developers" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":       "repository_group_permission",
			"permission": "read",
		})
	})

	client := newTestClient(t, handler)
	perm, err := client.GrantRepoGroupPermission(context.Background(), "ws", "my-service", "developers", "read")
	if err != nil {
		t.Fatalf("GrantRepoGroupPermission: %v", err)
	}
	if perm.Permission != "read" {
		t.Fatalf("unexpected permission: %q", perm.Permission)
	}
}

func TestRevokeRepoGroupPermission(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/repositories/ws/my-service/permissions-config/groups/developers" {
			t.Fatalf("path = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	client := newTestClient(t, handler)
	if err := client.RevokeRepoGroupPermission(context.Background(), "ws", "my-service", "developers"); err != nil {
		t.Fatalf("RevokeRepoGroupPermission: %v", err)
	}
}

func TestPermissionValidation(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	if _, err := client.ListProjectUserPermissions(context.Background(), "", "WEB", 10); err == nil {
		t.Fatal("expected error for empty workspace")
	}
	if _, err := client.ListProjectGroupPermissions(context.Background(), "ws", "", 10); err == nil {
		t.Fatal("expected error for empty project key")
	}
	if _, err := client.ListRepoUserPermissions(context.Background(), "ws", "", 10); err == nil {
		t.Fatal("expected error for empty repo slug")
	}
	if _, err := client.GrantRepoGroupPermission(context.Background(), "ws", "my-service", "", "read"); err == nil {
		t.Fatal("expected error for empty group slug")
	}
	if err := client.RevokeProjectUserPermission(context.Background(), "ws", "WEB", ""); err == nil {
		t.Fatal("expected error for empty account id")
	}
}
