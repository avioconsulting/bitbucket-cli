package bbcloud

import (
	"context"
	"fmt"
	"io"
	"net/url"
)

// Commit represents a Bitbucket Cloud commit summary.
type Commit struct {
	Hash    string `json:"hash"`
	Date    string `json:"date"`
	Message string `json:"message"`
}

// CommitListOptions configures commit listings.
type CommitListOptions struct {
	Include string
	Limit   int
}

type commitListPage struct {
	Values []Commit `json:"values"`
	Next   string   `json:"next"`
}

// CommitDiff streams the raw unified diff between two refs into w.
// spec must be "ref1..ref2" where refs can be commit SHAs, branch names, or tags.
func (c *Client) CommitDiff(ctx context.Context, workspace, repoSlug, spec string, w io.Writer) error {
	if workspace == "" {
		return fmt.Errorf("workspace is required")
	}
	if repoSlug == "" {
		return fmt.Errorf("repository slug is required")
	}
	if spec == "" {
		return fmt.Errorf("spec is required")
	}
	if w == nil {
		return fmt.Errorf("writer is required")
	}

	path := fmt.Sprintf("/repositories/%s/%s/diff/%s",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
		url.PathEscape(spec),
	)
	req, err := c.http.NewRequest(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/plain")

	return c.http.Do(req, w)
}

// ListCommits lists repository commits in reverse chronological order.
func (c *Client) ListCommits(ctx context.Context, workspace, repoSlug string, opts CommitListOptions) ([]Commit, error) {
	if workspace == "" || repoSlug == "" {
		return nil, fmt.Errorf("workspace and repository slug are required")
	}

	pageLen := opts.Limit
	if pageLen <= 0 || pageLen > 100 {
		pageLen = 20
	}

	params := url.Values{}
	params.Set("pagelen", fmt.Sprintf("%d", pageLen))
	if opts.Include != "" {
		params.Set("include", opts.Include)
	}

	path := fmt.Sprintf("/repositories/%s/%s/commits?%s",
		url.PathEscape(workspace),
		url.PathEscape(repoSlug),
		params.Encode(),
	)

	var commits []Commit
	for path != "" {
		req, err := c.http.NewRequest(ctx, "GET", path, nil)
		if err != nil {
			return nil, err
		}

		var page commitListPage
		if err := c.http.Do(req, &page); err != nil {
			return nil, err
		}

		commits = append(commits, page.Values...)

		if opts.Limit > 0 && len(commits) >= opts.Limit {
			commits = commits[:opts.Limit]
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

	return commits, nil
}
