package cleanup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/avivsinai/bitbucket-cli/pkg/bbcloud"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
)

type projectsOptions struct {
	Workspace   string
	InactiveFor string
	Empty       bool
	Limit       int
}

func newProjectsCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &projectsOptions{
		InactiveFor: "180d",
		Limit:       0,
	}

	cmd := &cobra.Command{
		Use:   "projects",
		Short: "List project cleanup candidates",
		Long: `List project cleanup candidates for a Bitbucket Cloud workspace.

At least one heuristic flag is required: --inactive-for or --empty. A project is
a candidate if it matches any of the provided flags (OR logic). Use this to find
projects that are empty or fully inactive and ready for archiving or deletion.

This command is read-only: it never deletes or renames projects.`,
		Example: `  # Find empty projects
  bkt cleanup projects --workspace my-team --empty

  # Find projects where every repository is inactive
  bkt cleanup projects --workspace my-team --inactive-for 365d`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjects(cmd, f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().StringVar(&opts.InactiveFor, "inactive-for", opts.InactiveFor, "Inactivity threshold like 30d, 180d, or 365d")
	cmd.Flags().BoolVar(&opts.Empty, "empty", false, "Match projects with no repositories")
	cmd.Flags().IntVar(&opts.Limit, "limit", opts.Limit, "Maximum projects to evaluate (0 for all)")

	return cmd
}

func runProjects(cmd *cobra.Command, f *cmdutil.Factory, opts *projectsOptions) error {
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
		return fmt.Errorf("cleanup projects supports Bitbucket Cloud contexts only")
	}

	workspace := cmdutil.FirstNonEmpty(opts.Workspace, ctxCfg.Workspace)
	if workspace == "" {
		return fmt.Errorf("workspace required; set with --workspace or configure the context default")
	}

	if !hasAnyProjectHeuristicFlag(cmd) {
		return fmt.Errorf("at least one heuristic flag is required: --inactive-for or --empty")
	}

	var inactiveDays int
	if cmd.Flags().Changed("inactive-for") {
		days, err := parseInactiveFor(opts.InactiveFor)
		if err != nil {
			return err
		}
		inactiveDays = days
	}

	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
	defer cancel()

	projects, err := client.ListProjects(ctx, workspace, opts.Limit)
	if err != nil {
		return err
	}

	candidates := make([]projectCandidate, 0, len(projects))
	for _, project := range projects {
		repos, err := client.ListProjectRepositories(ctx, workspace, project.Key, 0)
		if err != nil {
			return err
		}

		reasons, allInactive, latestUpdated := evaluateProject(repos, inactiveDays)

		deleteCandidate := false
		if opts.Empty && len(repos) == 0 {
			deleteCandidate = true
		}
		if inactiveDays > 0 && allInactive && len(repos) > 0 {
			deleteCandidate = true
		}

		if len(reasons) == 0 {
			continue
		}

		candidates = append(candidates, projectCandidate{
			Workspace:         workspace,
			ProjectKey:        project.Key,
			Name:              project.Name,
			RepoCount:         len(repos),
			Empty:             len(repos) == 0,
			AllReposInactive:  allInactive,
			LatestRepoUpdated: latestUpdated,
			Reason:            reasons,
			ArchiveCandidate:  true,
			DeleteCandidate:   deleteCandidate,
		})
	}

	return cmdutil.WriteOutput(cmd, ios.Out, candidates, func() error {
		if len(candidates) == 0 {
			_, err := fmt.Fprintf(ios.Out, "No project cleanup candidates found in workspace %s.\n", workspace)
			return err
		}

		if _, err := fmt.Fprintf(ios.Out, "Found %d project cleanup candidates in workspace %s:\n", len(candidates), workspace); err != nil {
			return err
		}
		for _, c := range candidates {
			if _, err := fmt.Fprintf(ios.Out, "%s\t%s\trepos: %d\treasons: %s\n", c.ProjectKey, c.Name, c.RepoCount, strings.Join(c.Reason, ", ")); err != nil {
				return err
			}
			if c.DeleteCandidate {
				if _, err := fmt.Fprintf(ios.Out, "    delete candidate: true\n"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func hasAnyProjectHeuristicFlag(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("inactive-for") || cmd.Flags().Changed("empty")
}

func evaluateProject(repos []bbcloud.Repository, inactiveDays int) ([]string, bool, string) {
	var reasons []string

	if len(repos) == 0 {
		reasons = append(reasons, "empty")
		return reasons, true, ""
	}

	allInactive := true
	var latestUpdated time.Time
	var latestUpdatedOn string

	if inactiveDays > 0 {
		threshold := time.Now().UTC().Add(-time.Duration(inactiveDays) * 24 * time.Hour)
		for _, repo := range repos {
			updated, err := time.Parse(time.RFC3339, repo.UpdatedOn)
			if err != nil {
				allInactive = false
				continue
			}
			if updated.After(threshold) {
				allInactive = false
			}
			if updated.After(latestUpdated) {
				latestUpdated = updated
				latestUpdatedOn = repo.UpdatedOn
			}
		}
		if allInactive {
			reasons = append(reasons, fmt.Sprintf("inactive:%dd", inactiveDays))
		}
	}

	return reasons, allInactive, latestUpdatedOn
}

type projectCandidate struct {
	Workspace         string   `json:"workspace"`
	ProjectKey        string   `json:"project_key"`
	Name              string   `json:"name"`
	RepoCount         int      `json:"repo_count"`
	Empty             bool     `json:"empty"`
	AllReposInactive  bool     `json:"all_repos_inactive"`
	LatestRepoUpdated string   `json:"latest_repo_updated_on,omitempty"`
	Reason            []string `json:"reason"`
	ArchiveCandidate  bool     `json:"archive_candidate"`
	DeleteCandidate   bool     `json:"delete_candidate"`
}
