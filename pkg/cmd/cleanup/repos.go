package cleanup

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/avivsinai/bitbucket-cli/pkg/bbcloud"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
)

type reposOptions struct {
	Workspace      string
	Project        string
	InactiveFor    string
	Empty          bool
	Public         bool
	Private        bool
	ExcludePattern string
	Limit          int
}

func newReposCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &reposOptions{
		InactiveFor: "180d",
		Limit:       0,
	}

	cmd := &cobra.Command{
		Use:   "repos",
		Short: "List repository cleanup candidates",
		Long: `List repository cleanup candidates for a Bitbucket Cloud workspace.

At least one heuristic flag is required: --inactive-for, --empty, --public,
or --private. A repository is a candidate if it matches any of the provided
flags (OR logic). Use --exclude-pattern to skip repositories by slug.

This command is read-only: it never deletes repositories.`,
		Example: `  # Find repositories inactive for 180 days
  bkt cleanup repos --workspace my-team --inactive-for 180d

  # Find empty repositories in a project
  bkt cleanup repos --workspace my-team --project WEB --empty

  # Find public repositories and exclude legacy suffixes
  bkt cleanup repos --workspace my-team --public --exclude-pattern '*-legacy'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepos(cmd, f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().StringVar(&opts.Project, "project", "", "Bitbucket project key scope")
	cmd.Flags().StringVar(&opts.InactiveFor, "inactive-for", opts.InactiveFor, "Inactivity threshold like 30d, 180d, or 365d")
	cmd.Flags().BoolVar(&opts.Empty, "empty", false, "Match repositories with no commits")
	cmd.Flags().BoolVar(&opts.Public, "public", false, "Match public repositories")
	cmd.Flags().BoolVar(&opts.Private, "private", false, "Match private repositories")
	cmd.Flags().StringVar(&opts.ExcludePattern, "exclude-pattern", "", "Case-insensitive glob pattern for repository slugs to skip")
	cmd.Flags().IntVar(&opts.Limit, "limit", opts.Limit, "Maximum repositories to evaluate (0 for all)")

	return cmd
}

func runRepos(cmd *cobra.Command, f *cmdutil.Factory, opts *reposOptions) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	override := cmdutil.FlagValue(cmd, "context")
	_, ctxCfg, host, err := cmdutil.ResolveContext(f, cmd, override)
	if err != nil {
		return err
	}

	if host.Kind != "cloud" {
		return fmt.Errorf("cleanup repos supports Bitbucket Cloud contexts only")
	}

	workspace := cmdutil.FirstNonEmpty(opts.Workspace, ctxCfg.Workspace)
	if workspace == "" {
		return fmt.Errorf("workspace required; set with --workspace or configure the context default")
	}

	if !hasAnyHeuristicFlag(opts, cmd) {
		return fmt.Errorf("at least one heuristic flag is required: --inactive-for, --empty, --public, or --private")
	}

	var inactiveDays int
	if cmd.Flags().Changed("inactive-for") {
		days, err := parseInactiveFor(opts.InactiveFor)
		if err != nil {
			return err
		}
		inactiveDays = days
	}

	var excludeGlob string
	if opts.ExcludePattern != "" {
		excludeGlob = opts.ExcludePattern
	}

	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	var repos []bbcloud.Repository
	if strings.TrimSpace(opts.Project) != "" {
		repos, err = client.ListProjectRepositories(ctx, workspace, opts.Project, opts.Limit)
	} else {
		repos, err = client.ListRepositories(ctx, workspace, opts.Limit)
	}
	if err != nil {
		return err
	}

	candidates := evaluateRepositories(repos, repoFilter{
		InactiveDays:   inactiveDays,
		Empty:          opts.Empty,
		Public:         opts.Public,
		Private:        opts.Private,
		ExcludePattern: excludeGlob,
	})

	return cmdutil.WriteOutput(cmd, ios.Out, candidates, func() error {
		if len(candidates) == 0 {
			_, err := fmt.Fprintf(ios.Out, "No repository cleanup candidates found in workspace %s.\n", workspace)
			return err
		}

		if _, err := fmt.Fprintf(ios.Out, "Found %d repository cleanup candidates in workspace %s:\n", len(candidates), workspace); err != nil {
			return err
		}
		for _, c := range candidates {
			if _, err := fmt.Fprintf(ios.Out, "%s/%s\tvisibility: %s\treasons: %s\n", c.Workspace, c.Repo, c.Visibility, strings.Join(c.Reason, ", ")); err != nil {
				return err
			}
			if c.DefaultBranch != "" {
				if _, err := fmt.Fprintf(ios.Out, "    default branch: %s\n", c.DefaultBranch); err != nil {
					return err
				}
			}
			if c.UpdatedOn != "" {
				if _, err := fmt.Fprintf(ios.Out, "    updated: %s\n", c.UpdatedOn); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func hasAnyHeuristicFlag(opts *reposOptions, cmd *cobra.Command) bool {
	return cmd.Flags().Changed("inactive-for") ||
		cmd.Flags().Changed("empty") ||
		cmd.Flags().Changed("public") ||
		cmd.Flags().Changed("private")
}

func parseInactiveFor(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("--inactive-for value cannot be empty")
	}
	if !strings.HasSuffix(s, "d") {
		return 0, fmt.Errorf("--inactive-for must be a number of days like 30d, 180d, or 365d")
	}
	days, err := parseDurationDays(s)
	if err != nil {
		return 0, err
	}
	if days < 0 {
		return 0, fmt.Errorf("--inactive-for must be a non-negative number of days")
	}
	return days, nil
}

func parseDurationDays(s string) (int, error) {
	value := strings.TrimSuffix(strings.TrimSpace(s), "d")
	value = strings.TrimSpace(value)
	var days int
	if _, err := fmt.Sscanf(value, "%d", &days); err != nil {
		return 0, fmt.Errorf("invalid --inactive-for value %q; expected format like 30d", s)
	}
	return days, nil
}

type repoFilter struct {
	InactiveDays   int
	Empty          bool
	Public         bool
	Private        bool
	ExcludePattern string
}

type repoCandidate struct {
	Workspace       string   `json:"workspace"`
	ProjectKey      string   `json:"project_key"`
	Repo            string   `json:"repo"`
	Visibility      string   `json:"visibility"`
	CreatedOn       string   `json:"created_on,omitempty"`
	UpdatedOn       string   `json:"updated_on,omitempty"`
	DefaultBranch   string   `json:"default_branch,omitempty"`
	Reason          []string `json:"reason"`
	DeleteCandidate bool     `json:"delete_candidate"`
}

func evaluateRepositories(repos []bbcloud.Repository, filter repoFilter) []repoCandidate {
	var candidates []repoCandidate

	var threshold time.Time
	if filter.InactiveDays > 0 {
		threshold = time.Now().UTC().Add(-time.Duration(filter.InactiveDays) * 24 * time.Hour)
	}

	for _, repo := range repos {
		if isExcluded(repo.Slug, filter.ExcludePattern) {
			continue
		}

		reasons := matchRepo(repo, filter, threshold)
		if len(reasons) == 0 {
			continue
		}

		projectKey := repo.Project.Key
		if projectKey == "" {
			projectKey = ""
		}

		visibility := "public"
		if repo.IsPrivate {
			visibility = "private"
		}

		candidates = append(candidates, repoCandidate{
			Workspace:       repo.Workspace.Slug,
			ProjectKey:      projectKey,
			Repo:            repo.Slug,
			Visibility:      visibility,
			CreatedOn:       repo.CreatedOn,
			UpdatedOn:       repo.UpdatedOn,
			DefaultBranch:   repo.MainBranch.Name,
			Reason:          reasons,
			DeleteCandidate: true,
		})
	}

	return candidates
}

func matchRepo(repo bbcloud.Repository, filter repoFilter, threshold time.Time) []string {
	var reasons []string

	if filter.InactiveDays > 0 {
		updated, err := time.Parse(time.RFC3339, repo.UpdatedOn)
		if err == nil && updated.Before(threshold) {
			reasons = append(reasons, fmt.Sprintf("inactive:%dd", filter.InactiveDays))
		}
	}

	if filter.Empty && repo.MainBranch.Name == "" {
		reasons = append(reasons, "empty")
	}

	if filter.Public && !repo.IsPrivate {
		reasons = append(reasons, "public")
	}

	if filter.Private && repo.IsPrivate {
		reasons = append(reasons, "private")
	}

	return reasons
}

func isExcluded(slug, pattern string) bool {
	if pattern == "" {
		return false
	}
	matched, err := path.Match(strings.ToLower(pattern), strings.ToLower(slug))
	if err != nil {
		return false
	}
	return matched
}
