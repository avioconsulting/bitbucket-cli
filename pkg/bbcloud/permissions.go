package bbcloud

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// ResolvedUser holds the canonical identifiers for a workspace user.
type ResolvedUser struct {
	UUID      string
	AccountID string
	Display   string
	Email     string
}

// workspaceMembershipPage is the paginated response for workspace members.
type workspaceMembershipPage struct {
	Values []workspaceMembership `json:"values"`
	Next   string                `json:"next"`
}

type workspaceMembership struct {
	Type      string             `json:"type"`
	User      resolvedUserRecord `json:"user"`
	Workspace struct {
		Slug string `json:"slug"`
	} `json:"workspace"`
}

type resolvedUserRecord struct {
	Type      string `json:"type"`
	UUID      string `json:"uuid"`
	AccountID string `json:"account_id"`
	Display   string `json:"display_name"`
	Email     string `json:"email,omitempty"`
}

// ResolveWorkspaceUser resolves a user identifier to an account ID.
// Accepts Atlassian account IDs (557058:...), UUIDs ({...}), or email
// addresses. Emails are resolved by filtering workspace members; UUIDs are
// resolved by fetching the membership record.
func (c *Client) ResolveWorkspaceUser(ctx context.Context, workspace, identifier string) (*ResolvedUser, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("user identifier is required")
	}
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	if LooksLikeAccountID(identifier) {
		return &ResolvedUser{AccountID: identifier}, nil
	}

	if LooksLikeUUID(identifier) {
		uuid := NormalizeUUID(identifier)
		return c.resolveUserByUUID(ctx, workspace, uuid)
	}

	if strings.Contains(identifier, "@") {
		return c.resolveUserByEmail(ctx, workspace, identifier)
	}

	return nil, fmt.Errorf("user identifier %q is not a recognized account_id, uuid, or email address", identifier)
}

func (c *Client) resolveUserByUUID(ctx context.Context, workspace, uuid string) (*ResolvedUser, error) {
	path := fmt.Sprintf("/workspaces/%s/members/%s",
		url.PathEscape(workspace),
		url.PathEscape(uuid),
	)
	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var membership workspaceMembership
	if err := c.http.Do(req, &membership); err != nil {
		return nil, err
	}

	return &ResolvedUser{
		UUID:      membership.User.UUID,
		AccountID: membership.User.AccountID,
		Display:   membership.User.Display,
	}, nil
}

func (c *Client) resolveUserByEmail(ctx context.Context, workspace, email string) (*ResolvedUser, error) {
	query := fmt.Sprintf(`user.email IN ("%s")`, email)
	params := url.Values{}
	params.Set("q", query)
	params.Set("fields", "+values.user.email")
	path := fmt.Sprintf("/workspaces/%s/members?%s",
		url.PathEscape(workspace),
		params.Encode(),
	)

	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var page workspaceMembershipPage
	if err := c.http.Do(req, &page); err != nil {
		return nil, err
	}

	lowerEmail := strings.ToLower(email)
	for _, m := range page.Values {
		if strings.EqualFold(m.User.Email, email) || strings.ToLower(m.User.Email) == lowerEmail {
			return &ResolvedUser{
				UUID:      m.User.UUID,
				AccountID: m.User.AccountID,
				Display:   m.User.Display,
				Email:     m.User.Email,
			}, nil
		}
	}

	return nil, fmt.Errorf("no workspace member found with email %q", email)
}

// ProjectUserPermission is an explicit user permission on a project.
type ProjectUserPermission struct {
	Type       string     `json:"type"`
	Permission string     `json:"permission"`
	User       UserRecord `json:"user"`
	Links      struct {
		Self struct {
			Href string `json:"href"`
		} `json:"self"`
	} `json:"links"`
}

// ProjectGroupPermission is an explicit group permission on a project.
type ProjectGroupPermission struct {
	Type       string      `json:"type"`
	Permission string      `json:"permission"`
	Group      GroupRecord `json:"group"`
	Links      struct {
		Self struct {
			Href string `json:"href"`
		} `json:"self"`
	} `json:"links"`
}

// RepoUserPermission is an explicit user permission on a repository.
type RepoUserPermission struct {
	Type       string     `json:"type"`
	Permission string     `json:"permission"`
	User       UserRecord `json:"user"`
	Links      struct {
		Self struct {
			Href string `json:"href"`
		} `json:"self"`
	} `json:"links"`
}

// RepoGroupPermission is an explicit group permission on a repository.
type RepoGroupPermission struct {
	Type       string      `json:"type"`
	Permission string      `json:"permission"`
	Group      GroupRecord `json:"group"`
	Links      struct {
		Self struct {
			Href string `json:"href"`
		} `json:"self"`
	} `json:"links"`
}

// UserRecord is a minimal user reference returned by permission endpoints.
type UserRecord struct {
	Type      string `json:"type"`
	UUID      string `json:"uuid"`
	AccountID string `json:"account_id"`
	Display   string `json:"display_name"`
}

// GroupRecord is a minimal group reference returned by permission endpoints.
type GroupRecord struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// PermissionUpdateRequest is the body for grant/update permission calls.
type PermissionUpdateRequest struct {
	Permission string `json:"permission"`
}

type projectUserPermissionPage struct {
	Values []ProjectUserPermission `json:"values"`
	Next   string                  `json:"next"`
}

type projectGroupPermissionPage struct {
	Values []ProjectGroupPermission `json:"values"`
	Next   string                   `json:"next"`
}

type repoUserPermissionPage struct {
	Values []RepoUserPermission `json:"values"`
	Next   string               `json:"next"`
}

type repoGroupPermissionPage struct {
	Values []RepoGroupPermission `json:"values"`
	Next   string                `json:"next"`
}

// ListProjectUserPermissions lists explicit user permissions on a project.
func (c *Client) ListProjectUserPermissions(ctx context.Context, workspace, projectKey string, limit int) ([]ProjectUserPermission, error) {
	if workspace == "" || projectKey == "" {
		return nil, fmt.Errorf("workspace and project key are required")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s/permissions-config/users",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
	)
	return c.listProjectUserPermissions(ctx, path, limit)
}

func (c *Client) listProjectUserPermissions(ctx context.Context, path string, limit int) ([]ProjectUserPermission, error) {
	pageLen := limit
	if pageLen <= 0 || pageLen > 100 {
		pageLen = 20
	}
	if !strings.Contains(path, "?") {
		path += "?"
	} else {
		path += "&"
	}
	path += fmt.Sprintf("pagelen=%d", pageLen)

	var perms []ProjectUserPermission
	for path != "" {
		req, err := c.http.NewRequest(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}

		var page projectUserPermissionPage
		if err := c.http.Do(req, &page); err != nil {
			return nil, err
		}

		perms = append(perms, page.Values...)
		if limit > 0 && len(perms) >= limit {
			perms = perms[:limit]
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
	return perms, nil
}

// GetProjectUserPermission returns an explicit user permission on a project.
func (c *Client) GetProjectUserPermission(ctx context.Context, workspace, projectKey, accountID string) (*ProjectUserPermission, error) {
	if workspace == "" || projectKey == "" || accountID == "" {
		return nil, fmt.Errorf("workspace, project key, and account id are required")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s/permissions-config/users/%s",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
		url.PathEscape(accountID),
	)
	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var perm ProjectUserPermission
	if err := c.http.Do(req, &perm); err != nil {
		return nil, err
	}
	return &perm, nil
}

// GrantProjectUserPermission grants or updates a user permission on a project.
func (c *Client) GrantProjectUserPermission(ctx context.Context, workspace, projectKey, accountID, permission string) (*ProjectUserPermission, error) {
	if workspace == "" || projectKey == "" || accountID == "" {
		return nil, fmt.Errorf("workspace, project key, and account id are required")
	}
	if permission == "" {
		return nil, fmt.Errorf("permission is required")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s/permissions-config/users/%s",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
		url.PathEscape(accountID),
	)
	req, err := c.http.NewRequest(ctx, "PUT", path, PermissionUpdateRequest{Permission: permission})
	if err != nil {
		return nil, err
	}
	var perm ProjectUserPermission
	if err := c.http.Do(req, &perm); err != nil {
		return nil, err
	}
	return &perm, nil
}

// RevokeProjectUserPermission removes an explicit user permission from a project.
func (c *Client) RevokeProjectUserPermission(ctx context.Context, workspace, projectKey, accountID string) error {
	if workspace == "" || projectKey == "" || accountID == "" {
		return fmt.Errorf("workspace, project key, and account id are required")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s/permissions-config/users/%s",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
		url.PathEscape(accountID),
	)
	req, err := c.http.NewRequest(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	return c.http.Do(req, nil)
}

// ListProjectGroupPermissions lists explicit group permissions on a project.
func (c *Client) ListProjectGroupPermissions(ctx context.Context, workspace, projectKey string, limit int) ([]ProjectGroupPermission, error) {
	if workspace == "" || projectKey == "" {
		return nil, fmt.Errorf("workspace and project key are required")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s/permissions-config/groups",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
	)
	return c.listProjectGroupPermissions(ctx, path, limit)
}

func (c *Client) listProjectGroupPermissions(ctx context.Context, path string, limit int) ([]ProjectGroupPermission, error) {
	pageLen := limit
	if pageLen <= 0 || pageLen > 100 {
		pageLen = 20
	}
	if !strings.Contains(path, "?") {
		path += "?"
	} else {
		path += "&"
	}
	path += fmt.Sprintf("pagelen=%d", pageLen)

	var perms []ProjectGroupPermission
	for path != "" {
		req, err := c.http.NewRequest(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}

		var page projectGroupPermissionPage
		if err := c.http.Do(req, &page); err != nil {
			return nil, err
		}

		perms = append(perms, page.Values...)
		if limit > 0 && len(perms) >= limit {
			perms = perms[:limit]
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
	return perms, nil
}

// GetProjectGroupPermission returns an explicit group permission on a project.
func (c *Client) GetProjectGroupPermission(ctx context.Context, workspace, projectKey, groupSlug string) (*ProjectGroupPermission, error) {
	if workspace == "" || projectKey == "" || groupSlug == "" {
		return nil, fmt.Errorf("workspace, project key, and group slug are required")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s/permissions-config/groups/%s",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
		url.PathEscape(groupSlug),
	)
	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var perm ProjectGroupPermission
	if err := c.http.Do(req, &perm); err != nil {
		return nil, err
	}
	return &perm, nil
}

// GrantProjectGroupPermission grants or updates a group permission on a project.
func (c *Client) GrantProjectGroupPermission(ctx context.Context, workspace, projectKey, groupSlug, permission string) (*ProjectGroupPermission, error) {
	if workspace == "" || projectKey == "" || groupSlug == "" {
		return nil, fmt.Errorf("workspace, project key, and group slug are required")
	}
	if permission == "" {
		return nil, fmt.Errorf("permission is required")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s/permissions-config/groups/%s",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
		url.PathEscape(groupSlug),
	)
	req, err := c.http.NewRequest(ctx, "PUT", path, PermissionUpdateRequest{Permission: permission})
	if err != nil {
		return nil, err
	}
	var perm ProjectGroupPermission
	if err := c.http.Do(req, &perm); err != nil {
		return nil, err
	}
	return &perm, nil
}

// RevokeProjectGroupPermission removes an explicit group permission from a project.
func (c *Client) RevokeProjectGroupPermission(ctx context.Context, workspace, projectKey, groupSlug string) error {
	if workspace == "" || projectKey == "" || groupSlug == "" {
		return fmt.Errorf("workspace, project key, and group slug are required")
	}

	path := fmt.Sprintf("/workspaces/%s/projects/%s/permissions-config/groups/%s",
		url.PathEscape(workspace),
		url.PathEscape(projectKey),
		url.PathEscape(groupSlug),
	)
	req, err := c.http.NewRequest(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	return c.http.Do(req, nil)
}

// ListRepoUserPermissions lists explicit user permissions on a repository.
func (c *Client) ListRepoUserPermissions(ctx context.Context, workspace, repoSlug string, limit int) ([]RepoUserPermission, error) {
	if workspace == "" || repoSlug == "" {
		return nil, fmt.Errorf("workspace and repository slug are required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/permissions-config/users",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
	)
	return c.listRepoUserPermissions(ctx, path, limit)
}

func (c *Client) listRepoUserPermissions(ctx context.Context, path string, limit int) ([]RepoUserPermission, error) {
	pageLen := limit
	if pageLen <= 0 || pageLen > 100 {
		pageLen = 20
	}
	if !strings.Contains(path, "?") {
		path += "?"
	} else {
		path += "&"
	}
	path += fmt.Sprintf("pagelen=%d", pageLen)

	var perms []RepoUserPermission
	for path != "" {
		req, err := c.http.NewRequest(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}

		var page repoUserPermissionPage
		if err := c.http.Do(req, &page); err != nil {
			return nil, err
		}

		perms = append(perms, page.Values...)
		if limit > 0 && len(perms) >= limit {
			perms = perms[:limit]
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
	return perms, nil
}

// GetRepoUserPermission returns an explicit user permission on a repository.
func (c *Client) GetRepoUserPermission(ctx context.Context, workspace, repoSlug, accountID string) (*RepoUserPermission, error) {
	if workspace == "" || repoSlug == "" || accountID == "" {
		return nil, fmt.Errorf("workspace, repository slug, and account id are required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/permissions-config/users/%s",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
		url.PathEscape(accountID),
	)
	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var perm RepoUserPermission
	if err := c.http.Do(req, &perm); err != nil {
		return nil, err
	}
	return &perm, nil
}

// GrantRepoUserPermission grants or updates a user permission on a repository.
func (c *Client) GrantRepoUserPermission(ctx context.Context, workspace, repoSlug, accountID, permission string) (*RepoUserPermission, error) {
	if workspace == "" || repoSlug == "" || accountID == "" {
		return nil, fmt.Errorf("workspace, repository slug, and account id are required")
	}
	if permission == "" {
		return nil, fmt.Errorf("permission is required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/permissions-config/users/%s",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
		url.PathEscape(accountID),
	)
	req, err := c.http.NewRequest(ctx, "PUT", path, PermissionUpdateRequest{Permission: permission})
	if err != nil {
		return nil, err
	}
	var perm RepoUserPermission
	if err := c.http.Do(req, &perm); err != nil {
		return nil, err
	}
	return &perm, nil
}

// RevokeRepoUserPermission removes an explicit user permission from a repository.
func (c *Client) RevokeRepoUserPermission(ctx context.Context, workspace, repoSlug, accountID string) error {
	if workspace == "" || repoSlug == "" || accountID == "" {
		return fmt.Errorf("workspace, repository slug, and account id are required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/permissions-config/users/%s",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
		url.PathEscape(accountID),
	)
	req, err := c.http.NewRequest(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	return c.http.Do(req, nil)
}

// ListRepoGroupPermissions lists explicit group permissions on a repository.
func (c *Client) ListRepoGroupPermissions(ctx context.Context, workspace, repoSlug string, limit int) ([]RepoGroupPermission, error) {
	if workspace == "" || repoSlug == "" {
		return nil, fmt.Errorf("workspace and repository slug are required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/permissions-config/groups",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
	)
	return c.listRepoGroupPermissions(ctx, path, limit)
}

func (c *Client) listRepoGroupPermissions(ctx context.Context, path string, limit int) ([]RepoGroupPermission, error) {
	pageLen := limit
	if pageLen <= 0 || pageLen > 100 {
		pageLen = 20
	}
	if !strings.Contains(path, "?") {
		path += "?"
	} else {
		path += "&"
	}
	path += fmt.Sprintf("pagelen=%d", pageLen)

	var perms []RepoGroupPermission
	for path != "" {
		req, err := c.http.NewRequest(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}

		var page repoGroupPermissionPage
		if err := c.http.Do(req, &page); err != nil {
			return nil, err
		}

		perms = append(perms, page.Values...)
		if limit > 0 && len(perms) >= limit {
			perms = perms[:limit]
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
	return perms, nil
}

// GetRepoGroupPermission returns an explicit group permission on a repository.
func (c *Client) GetRepoGroupPermission(ctx context.Context, workspace, repoSlug, groupSlug string) (*RepoGroupPermission, error) {
	if workspace == "" || repoSlug == "" || groupSlug == "" {
		return nil, fmt.Errorf("workspace, repository slug, and group slug are required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/permissions-config/groups/%s",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
		url.PathEscape(groupSlug),
	)
	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var perm RepoGroupPermission
	if err := c.http.Do(req, &perm); err != nil {
		return nil, err
	}
	return &perm, nil
}

// GrantRepoGroupPermission grants or updates a group permission on a repository.
func (c *Client) GrantRepoGroupPermission(ctx context.Context, workspace, repoSlug, groupSlug, permission string) (*RepoGroupPermission, error) {
	if workspace == "" || repoSlug == "" || groupSlug == "" {
		return nil, fmt.Errorf("workspace, repository slug, and group slug are required")
	}
	if permission == "" {
		return nil, fmt.Errorf("permission is required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/permissions-config/groups/%s",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
		url.PathEscape(groupSlug),
	)
	req, err := c.http.NewRequest(ctx, "PUT", path, PermissionUpdateRequest{Permission: permission})
	if err != nil {
		return nil, err
	}
	var perm RepoGroupPermission
	if err := c.http.Do(req, &perm); err != nil {
		return nil, err
	}
	return &perm, nil
}

// RevokeRepoGroupPermission removes an explicit group permission from a repository.
func (c *Client) RevokeRepoGroupPermission(ctx context.Context, workspace, repoSlug, groupSlug string) error {
	if workspace == "" || repoSlug == "" || groupSlug == "" {
		return fmt.Errorf("workspace, repository slug, and group slug are required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/permissions-config/groups/%s",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
		url.PathEscape(groupSlug),
	)
	req, err := c.http.NewRequest(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	return c.http.Do(req, nil)
}
