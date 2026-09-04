package bbcloud

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestListProjects(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/projects" {
			t.Fatalf("path = %q", got)
		}
		if got := r.URL.Query().Get("pagelen"); got != "2" {
			t.Fatalf("pagelen = %q, want 2", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"key": "WEB", "name": "Web"},
				{"key": "DATA", "name": "Data"},
			},
		})
	})

	client := newTestClient(t, handler)
	projects, err := client.ListProjects(context.Background(), "ws", 2)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 2 || projects[0].Key != "WEB" || projects[1].Key != "DATA" {
		t.Fatalf("unexpected projects: %+v", projects)
	}
}

func TestListProjectsValidation(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	_, err := client.ListProjects(context.Background(), "", 10)
	if err == nil {
		t.Fatal("expected error for empty workspace")
	}
}

func TestGetProject(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/projects/WEB" {
			t.Fatalf("path = %q", got)
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
			"links": map[string]any{
				"html": map[string]any{"href": "https://bitbucket.org/ws/workspace/projects/WEB"},
			},
		})
	})

	client := newTestClient(t, handler)
	project, err := client.GetProject(context.Background(), "ws", "WEB")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if project.Key != "WEB" || project.Name != "Web Platform" || !project.IsPrivate {
		t.Fatalf("unexpected project: %+v", project)
	}
	if project.Links.HTML.Href == "" {
		t.Fatal("expected HTML link")
	}
}

func TestGetProjectValidation(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	_, err := client.GetProject(context.Background(), "", "WEB")
	if err == nil {
		t.Fatal("expected error for empty workspace")
	}
	_, err = client.GetProject(context.Background(), "ws", "")
	if err == nil {
		t.Fatal("expected error for empty project key")
	}
}

func TestCreateProject(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/projects" {
			t.Fatalf("path = %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["key"] != "WEB" || body["name"] != "Web Platform" {
			t.Fatalf("unexpected body: %v", body)
		}
		if body["is_private"] != true {
			t.Fatalf("expected is_private=true, got %v", body["is_private"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "WEB", "name": "Web Platform"})
	})

	client := newTestClient(t, handler)
	project, err := client.CreateProject(context.Background(), "ws", CreateProjectInput{
		Key:         "WEB",
		Name:        "Web Platform",
		Description: "Frontend",
		IsPrivate:   true,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if project.Key != "WEB" {
		t.Fatalf("unexpected key: %q", project.Key)
	}
}

func TestCreateProjectValidation(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	_, err := client.CreateProject(context.Background(), "", CreateProjectInput{Key: "WEB", Name: "Web"})
	if err == nil {
		t.Fatal("expected error for empty workspace")
	}
	_, err = client.CreateProject(context.Background(), "ws", CreateProjectInput{Key: "", Name: "Web"})
	if err == nil {
		t.Fatal("expected error for empty project key")
	}
	_, err = client.CreateProject(context.Background(), "ws", CreateProjectInput{Key: "WEB", Name: ""})
	if err == nil {
		t.Fatal("expected error for empty project name")
	}
}

func TestDeleteProject(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if got := r.URL.Path; got != "/workspaces/ws/projects/WEB" {
			t.Fatalf("path = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	client := newTestClient(t, handler)
	if err := client.DeleteProject(context.Background(), "ws", "WEB"); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
}

func TestDeleteProjectValidation(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	err := client.DeleteProject(context.Background(), "", "WEB")
	if err == nil {
		t.Fatal("expected error for empty workspace")
	}
	err = client.DeleteProject(context.Background(), "ws", "")
	if err == nil {
		t.Fatal("expected error for empty project key")
	}
}
