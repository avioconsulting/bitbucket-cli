package bbcloud

import (
	"context"
	"fmt"
	"net/url"
)

// Project identifies a Bitbucket Cloud project within a workspace.
type Project struct {
	UUID                    string `json:"uuid"`
	Key                     string `json:"key"`
	Name                    string `json:"name"`
	Description             string `json:"description"`
	IsPrivate               bool   `json:"is_private"`
	CreatedOn               string `json:"created_on"`
	UpdatedOn               string `json:"updated_on"`
	HasPubliclyVisibleRepos bool   `json:"has_publicly_visible_repos"`
	Links                   struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

type projectListPage struct {
	Values []Project `json:"values"`
	Next   string    `json:"next"`
}

// ListProjects lists projects in a Bitbucket Cloud workspace.
func (c *Client) ListProjects(ctx context.Context, workspace string, limit int) ([]Project, error) {
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	pageLen := limit
	if pageLen <= 0 || pageLen > 100 {
		pageLen = 20
	}

	path := fmt.Sprintf("/workspaces/%s/projects?pagelen=%d",
		url.PathEscape(workspace),
		pageLen,
	)

	var projects []Project
	for path != "" {
		req, err := c.http.NewRequest(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}

		var page projectListPage
		if err := c.http.Do(req, &page); err != nil {
			return nil, err
		}

		projects = append(projects, page.Values...)

		if limit > 0 && len(projects) >= limit {
			projects = projects[:limit]
			break
		}

		if page.Next == "" {
			break
		}

		nextURL, err := url.Parse(page.Next)
		if err != nil {
			return nil, err
		}
		path = nextURL.RequestURI()
	}

	return projects, nil
}

// GetProject retrieves a project in a Bitbucket Cloud workspace.
func (c *Client) GetProject(ctx context.Context, workspace, projectKey string) (*Project, error) {
	if workspace == "" || projectKey == "" {
		return nil, fmt.Errorf("workspace and project key are required")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
	)
	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var project Project
	if err := c.http.Do(req, &project); err != nil {
		return nil, err
	}
	return &project, nil
}

// CreateProjectInput describes project creation parameters.
type CreateProjectInput struct {
	Key         string
	Name        string
	Description string
	IsPrivate   bool
}

// CreateProject creates a project within the workspace.
func (c *Client) CreateProject(ctx context.Context, workspace string, input CreateProjectInput) (*Project, error) {
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	if input.Key == "" {
		return nil, fmt.Errorf("project key is required")
	}
	if input.Name == "" {
		return nil, fmt.Errorf("project name is required")
	}

	body := map[string]any{
		"key":  input.Key,
		"name": input.Name,
	}
	if input.Description != "" {
		body["description"] = input.Description
	}
	body["is_private"] = input.IsPrivate

	path := fmt.Sprintf("/workspaces/%s/projects", url.PathEscape(workspace))
	req, err := c.http.NewRequest(ctx, "POST", path, body)
	if err != nil {
		return nil, err
	}

	var project Project
	if err := c.http.Do(req, &project); err != nil {
		return nil, err
	}
	return &project, nil
}

// UpdateProjectInput describes project update parameters.
type UpdateProjectInput struct {
	Name        string
	Description string
	IsPrivate   *bool
}

// UpdateProject updates a project within the workspace.
// The project key cannot be changed by this helper.
func (c *Client) UpdateProject(ctx context.Context, workspace, projectKey string, input UpdateProjectInput) (*Project, error) {
	if workspace == "" || projectKey == "" {
		return nil, fmt.Errorf("workspace and project key are required")
	}
	if input.Name == "" {
		return nil, fmt.Errorf("project name is required")
	}

	body := map[string]any{
		"name": input.Name,
	}
	if input.Description != "" {
		body["description"] = input.Description
	}
	if input.IsPrivate != nil {
		body["is_private"] = *input.IsPrivate
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
	)
	req, err := c.http.NewRequest(ctx, "PUT", path, body)
	if err != nil {
		return nil, err
	}

	var project Project
	if err := c.http.Do(req, &project); err != nil {
		return nil, err
	}
	return &project, nil
}

// DeleteProject permanently removes an empty Bitbucket Cloud project.
func (c *Client) DeleteProject(ctx context.Context, workspace, projectKey string) error {
	if workspace == "" || projectKey == "" {
		return fmt.Errorf("workspace and project key are required")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
	)
	req, err := c.http.NewRequest(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}

	return c.http.Do(req, nil)
}
