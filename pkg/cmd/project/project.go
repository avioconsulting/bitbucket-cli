package project

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/avivsinai/bitbucket-cli/internal/config"
	"github.com/avivsinai/bitbucket-cli/pkg/bbcloud"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
	"github.com/avivsinai/bitbucket-cli/pkg/iostreams"
)

// NewCmdProject wires project-focused subcommands.
func NewCmdProject(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Work with Bitbucket projects",
		Long: `List and inspect Bitbucket projects. Projects are top-level containers that
group related repositories on Bitbucket Data Center and Cloud.`,
		Example: `  # List all visible projects
  bkt project list

  # List projects in a Cloud workspace
  bkt project list --workspace my-team

  # View a Cloud project
  bkt project view WEB --workspace my-team`,
	}

	cmd.AddCommand(newListCmd(f))
	cmd.AddCommand(newCreateCmd(f))
	cmd.AddCommand(newViewCmd(f))
	cmd.AddCommand(newReposCmd(f))
	cmd.AddCommand(newRenameCmd(f))
	cmd.AddCommand(newArchiveCmd(f))
	cmd.AddCommand(newDeleteCmd(f))

	return cmd
}

type listOptions struct {
	Host      string
	Workspace string
	Limit     int
}

type viewOptions struct {
	Workspace string
}

type reposOptions struct {
	Workspace string
	Limit     int
}

type createOptions struct {
	Workspace   string
	Name        string
	Description string
	Private     bool
}

type renameOptions struct {
	Workspace   string
	Name        string
	Description string
}

type archiveOptions struct {
	Workspace   string
	Prefix      string
	Description string
}

type deleteOptions struct {
	Workspace string
	Cascade   bool
	Yes       bool
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &listOptions{
		Limit: 30,
	}
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List Bitbucket projects",
		Long: `List all projects visible to the authenticated user on a Bitbucket Data
Center instance or in a Bitbucket Cloud workspace. Each project is displayed
with its key, name, description, web URL, and visibility status. Use --limit to
control the number of results returned.`,
		Example: `  # List projects (default limit of 30)
  bkt project list

  # List all projects without a limit
  bkt project ls --limit 0

  # List projects on a specific Data Center host
  bkt project list --host my-dc-server

  # List projects in a Cloud workspace
  bkt project list --workspace my-team

  # List projects in JSON format
  bkt project list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, f, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Host, "host", "", "Host key or base URL override")
	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().IntVar(&opts.Limit, "limit", opts.Limit, "Maximum projects to display (0 for all)")

	return cmd
}

func runList(cmd *cobra.Command, f *cmdutil.Factory, opts *listOptions) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	contextOverride := cmdutil.FlagValue(cmd, "context")
	var hostKey string
	var workspace string
	var hostCfg *config.Host
	if strings.TrimSpace(opts.Host) != "" {
		resolvedHostKey, resolvedHost, err := cmdutil.ResolveHost(f, contextOverride, opts.Host)
		if err != nil {
			return err
		}
		hostKey = resolvedHostKey
		hostCfg = resolvedHost
		workspace = strings.TrimSpace(opts.Workspace)
	} else {
		resolvedHostKey, ctxCfg, resolvedHost, err := cmdutil.ResolveContext(f, cmd, contextOverride)
		if err != nil {
			return err
		}
		hostKey = resolvedHostKey
		hostCfg = resolvedHost
		workspace = cmdutil.FirstNonEmpty(opts.Workspace, ctxCfg.Workspace)
	}

	switch hostCfg.Kind {
	case "dc":
		return runListDC(cmd, f, ios.Out, hostKey, hostCfg, opts.Limit)
	case "cloud":
		return runListCloud(cmd, f, ios.Out, hostCfg, workspace, opts.Limit)
	default:
		return fmt.Errorf("unsupported host kind %q", hostCfg.Kind)
	}
}

func runListDC(cmd *cobra.Command, f *cmdutil.Factory, iosOut io.Writer, hostKey string, hostCfg *config.Host, limit int) error {
	client, err := cmdutil.NewDCClient(hostCfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	projects, err := client.ListProjects(ctx, limit)
	if err != nil {
		return err
	}

	type projectSummary struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		ID          int    `json:"id"`
		Type        string `json:"type"`
		Public      bool   `json:"public"`
		Description string `json:"description,omitempty"`
		WebURL      string `json:"web_url"`
	}

	baseURL := strings.TrimRight(hostCfg.BaseURL, "/")
	var summaries []projectSummary
	for _, p := range projects {
		key := strings.ToUpper(strings.TrimSpace(p.Key))
		webURL := fmt.Sprintf("%s/projects/%s", baseURL, url.PathEscape(key))

		summaries = append(summaries, projectSummary{
			Key:         key,
			Name:        p.Name,
			ID:          p.ID,
			Type:        p.Type,
			Public:      p.Public,
			Description: strings.TrimSpace(p.Description),
			WebURL:      webURL,
		})
	}

	payload := struct {
		HostKey  string           `json:"host_key"`
		BaseURL  string           `json:"base_url"`
		Projects []projectSummary `json:"projects"`
	}{
		HostKey:  hostKey,
		BaseURL:  baseURL,
		Projects: summaries,
	}

	return cmdutil.WriteOutput(cmd, iosOut, payload, func() error {
		if len(summaries) == 0 {
			_, err := fmt.Fprintf(iosOut, "No projects visible on host %s.\n", baseURL)
			return err
		}

		if _, err := fmt.Fprintf(iosOut, "Projects on %s:\n", baseURL); err != nil {
			return err
		}
		for _, p := range summaries {
			if _, err := fmt.Fprintf(iosOut, "%s\t%s\n", p.Key, p.Name); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(iosOut, "    link: %s\n", p.WebURL); err != nil {
				return err
			}
			if p.Description != "" {
				if _, err := fmt.Fprintf(iosOut, "    desc: %s\n", p.Description); err != nil {
					return err
				}
			}
			if p.Public {
				if _, err := fmt.Fprintln(iosOut, "    visibility: public"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func runListCloud(cmd *cobra.Command, f *cmdutil.Factory, iosOut io.Writer, hostCfg *config.Host, workspace string, limit int) error {
	if workspace == "" {
		return fmt.Errorf("workspace required; set with --workspace or configure the context default")
	}

	client, err := cmdutil.NewCloudClient(hostCfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	projects, err := client.ListProjects(ctx, workspace, limit)
	if err != nil {
		return err
	}

	projectSummaries := summarizeCloudProjects(projects)
	payload := struct {
		Workspace string                `json:"workspace"`
		Projects  []cloudProjectSummary `json:"projects"`
	}{
		Workspace: workspace,
		Projects:  projectSummaries,
	}

	return cmdutil.WriteOutput(cmd, iosOut, payload, func() error {
		if len(projectSummaries) == 0 {
			_, err := fmt.Fprintf(iosOut, "No projects found in workspace %s.\n", workspace)
			return err
		}

		if _, err := fmt.Fprintf(iosOut, "Projects in workspace %s:\n", workspace); err != nil {
			return err
		}
		for _, p := range projectSummaries {
			if _, err := fmt.Fprintf(iosOut, "%s\t%s\n", p.Key, p.Name); err != nil {
				return err
			}
			if p.WebURL != "" {
				if _, err := fmt.Fprintf(iosOut, "    link: %s\n", p.WebURL); err != nil {
					return err
				}
			}
			if p.Description != "" {
				if _, err := fmt.Fprintf(iosOut, "    desc: %s\n", p.Description); err != nil {
					return err
				}
			}
			if !p.Private {
				if _, err := fmt.Fprintln(iosOut, "    visibility: public"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

type cloudProjectSummary struct {
	Key                     string `json:"key"`
	Name                    string `json:"name"`
	UUID                    string `json:"uuid,omitempty"`
	Private                 bool   `json:"private"`
	Description             string `json:"description,omitempty"`
	CreatedOn               string `json:"created_on,omitempty"`
	UpdatedOn               string `json:"updated_on,omitempty"`
	HasPubliclyVisibleRepos bool   `json:"has_publicly_visible_repos"`
	WebURL                  string `json:"web_url,omitempty"`
}

func summarizeCloudProjects(projects []bbcloud.Project) []cloudProjectSummary {
	summaries := make([]cloudProjectSummary, 0, len(projects))
	for _, p := range projects {
		summaries = append(summaries, summarizeCloudProject(p))
	}
	return summaries
}

func summarizeCloudProject(p bbcloud.Project) cloudProjectSummary {
	return cloudProjectSummary{
		Key:                     strings.ToUpper(strings.TrimSpace(p.Key)),
		Name:                    p.Name,
		UUID:                    strings.Trim(p.UUID, "{}"),
		Private:                 p.IsPrivate,
		Description:             strings.TrimSpace(p.Description),
		CreatedOn:               p.CreatedOn,
		UpdatedOn:               p.UpdatedOn,
		HasPubliclyVisibleRepos: p.HasPubliclyVisibleRepos,
		WebURL:                  p.Links.HTML.Href,
	}
}

func newViewCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &viewOptions{}
	cmd := &cobra.Command{
		Use:   "view <project-key>",
		Short: "Display details for a Bitbucket Cloud project",
		Long: `Display details for a Bitbucket Cloud project, including its key, name,
description, privacy, timestamps, and web URL.

This command is only available for Bitbucket Cloud contexts.`,
		Example: `  # View a Cloud project in the active workspace
  bkt project view WEB

  # View a Cloud project in a specific workspace
  bkt project view WEB --workspace my-team`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runView(cmd, f, opts, args[0])
		},
	}
	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	return cmd
}

func runView(cmd *cobra.Command, f *cmdutil.Factory, opts *viewOptions, projectKey string) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	workspace, host, err := resolveCloudProjectTarget(f, cmd, opts.Workspace)
	if err != nil {
		return err
	}

	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	project, err := client.GetProject(ctx, workspace, normalizeProjectKey(projectKey))
	if err != nil {
		return err
	}
	summary := summarizeCloudProject(*project)

	return cmdutil.WriteOutput(cmd, ios.Out, summary, func() error {
		if _, err := fmt.Fprintf(ios.Out, "%s\t%s\n", summary.Key, summary.Name); err != nil {
			return err
		}
		if summary.WebURL != "" {
			if _, err := fmt.Fprintf(ios.Out, "    link: %s\n", summary.WebURL); err != nil {
				return err
			}
		}
		if summary.Description != "" {
			if _, err := fmt.Fprintf(ios.Out, "    desc: %s\n", summary.Description); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(ios.Out, "    visibility: %s\n", cloudProjectVisibility(summary.Private)); err != nil {
			return err
		}
		if summary.CreatedOn != "" {
			if _, err := fmt.Fprintf(ios.Out, "    created: %s\n", summary.CreatedOn); err != nil {
				return err
			}
		}
		if summary.UpdatedOn != "" {
			if _, err := fmt.Fprintf(ios.Out, "    updated: %s\n", summary.UpdatedOn); err != nil {
				return err
			}
		}
		return nil
	})
}

func newCreateCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &createOptions{Private: true}
	cmd := &cobra.Command{
		Use:   "create <project-key>",
		Short: "Create a Bitbucket Cloud project",
		Long: `Create a new Bitbucket Cloud project in the active or specified workspace.

This command is only available for Bitbucket Cloud contexts.`,
		Example: `  # Create a private project with a default name
  bkt project create WEB --workspace my-team

  # Create a project with a friendly name and description
  bkt project create WEB --workspace my-team --name "Web Platform" --description "Frontend services"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd, f, opts, args[0])
		},
	}

	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().StringVar(&opts.Name, "name", "", "Project display name (defaults to project key)")
	cmd.Flags().StringVar(&opts.Description, "description", "", "Project description")
	cmd.Flags().BoolVar(&opts.Private, "private", true, "Create project as private")

	return cmd
}

func runCreate(cmd *cobra.Command, f *cmdutil.Factory, opts *createOptions, projectKey string) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	workspace, host, err := resolveCloudProjectTarget(f, cmd, opts.Workspace)
	if err != nil {
		return err
	}

	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	key := normalizeProjectKey(projectKey)
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = key
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	project, err := client.CreateProject(ctx, workspace, bbcloud.CreateProjectInput{
		Key:         key,
		Name:        name,
		Description: strings.TrimSpace(opts.Description),
		IsPrivate:   opts.Private,
	})
	if err != nil {
		return err
	}

	summary := summarizeCloudProject(*project)
	return cmdutil.WriteOutput(cmd, ios.Out, summary, func() error {
		_, err := fmt.Fprintf(ios.Out, "Created project %s/%s (%s).\n", workspace, summary.Key, summary.Name)
		return err
	})
}

func newReposCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &reposOptions{Limit: 30}
	cmd := &cobra.Command{
		Use:   "repos <project-key>",
		Short: "List repositories in a Bitbucket Cloud project",
		Long: `List repositories assigned to a Bitbucket Cloud project.

This command is only available for Bitbucket Cloud contexts.`,
		Example: `  # List repositories in a Cloud project
  bkt project repos WEB

  # List all repositories in a Cloud project
  bkt project repos WEB --limit 0`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepos(cmd, f, opts, args[0])
		},
	}
	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().IntVar(&opts.Limit, "limit", opts.Limit, "Maximum repositories to display (0 for all)")
	return cmd
}

func runRepos(cmd *cobra.Command, f *cmdutil.Factory, opts *reposOptions, projectKey string) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	workspace, host, err := resolveCloudProjectTarget(f, cmd, opts.Workspace)
	if err != nil {
		return err
	}

	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	repos, err := listCloudProjectRepos(ctx, client, workspace, projectKey, opts.Limit)
	if err != nil {
		return err
	}
	summaries := summarizeCloudRepositories(repos)
	key := normalizeProjectKey(projectKey)
	payload := struct {
		Workspace    string                 `json:"workspace"`
		Project      string                 `json:"project"`
		Repositories []cloudRepositoryEntry `json:"repositories"`
	}{Workspace: workspace, Project: key, Repositories: summaries}

	return cmdutil.WriteOutput(cmd, ios.Out, payload, func() error {
		if len(summaries) == 0 {
			_, err := fmt.Fprintf(ios.Out, "No repositories found in project %s.\n", key)
			return err
		}
		for _, r := range summaries {
			if _, err := fmt.Fprintf(ios.Out, "%s/%s\t%s\n", workspace, r.Slug, r.Name); err != nil {
				return err
			}
			if r.WebURL != "" {
				if _, err := fmt.Fprintf(ios.Out, "    web:   %s\n", r.WebURL); err != nil {
					return err
				}
			}
			if len(r.Clone) > 0 {
				if _, err := fmt.Fprintf(ios.Out, "    clone: %s\n", strings.Join(r.Clone, ", ")); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func newRenameCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &renameOptions{}
	cmd := &cobra.Command{
		Use:   "rename <project-key>",
		Short: "Rename a Bitbucket Cloud project",
		Long: `Rename a Bitbucket Cloud project. The project key is not changed;
only the display name (and optionally the description) are updated.

This is useful for archiving by renaming projects to sort them to the end of
project lists, for example "ZZ - Archived - <name>".

This command is only available for Bitbucket Cloud contexts.`,
		Example: `  # Rename a project
  bkt project rename DFW --name "DFW Airport (Archived)"

  # Rename for archive-style sorting
  bkt project rename DFW --name "ZZ - Archived - DFW Airport"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRename(cmd, f, opts, args[0])
		},
	}
	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().StringVar(&opts.Name, "name", "", "New project name (required)")
	cmd.Flags().StringVar(&opts.Description, "description", "", "New project description")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func runRename(cmd *cobra.Command, f *cmdutil.Factory, opts *renameOptions, projectKey string) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	workspace, host, err := resolveCloudProjectTarget(f, cmd, opts.Workspace)
	if err != nil {
		return err
	}

	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	key := normalizeProjectKey(projectKey)
	oldProject, err := client.GetProject(ctx, workspace, key)
	if err != nil {
		return err
	}

	updated, err := client.UpdateProject(ctx, workspace, key, bbcloud.UpdateProjectInput{
		Name:        strings.TrimSpace(opts.Name),
		Description: strings.TrimSpace(opts.Description),
	})
	if err != nil {
		return err
	}

	result := struct {
		Workspace string `json:"workspace"`
		Project   string `json:"project_key"`
		OldName   string `json:"old_name"`
		NewName   string `json:"new_name"`
	}{
		Workspace: workspace,
		Project:   key,
		OldName:   oldProject.Name,
		NewName:   updated.Name,
	}

	return cmdutil.WriteOutput(cmd, ios.Out, result, func() error {
		_, err := fmt.Fprintf(ios.Out, "Renamed project %q from %q to %q.\n", key, result.OldName, result.NewName)
		return err
	})
}

func newArchiveCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &archiveOptions{
		Prefix: "ZZ - Archived - ",
	}
	cmd := &cobra.Command{
		Use:   "archive <project-key>",
		Short: "Archive a Bitbucket Cloud project and downgrade group write access",
		Long: `Archive a Bitbucket Cloud project by renaming it with a prefix.

The current project name is looked up automatically, and the configured prefix is
prepended. The default prefix is "ZZ - Archived - ", which sorts the project to
the end of most project lists.

The project key is not changed, so repository URLs remain valid.

Explicit project group permissions with write access are downgraded to read.
Group permissions with admin access are retained.

Downgrading permissions requires an API token or app password because Bitbucket
does not support OAuth for permission mutations.

This command is only available for Bitbucket Cloud contexts.`,
		Example: `  # Archive a project using the default prefix
  bkt project archive DFW

  # Archive with a custom prefix
  bkt project archive DFW --prefix "Legacy - "`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArchive(cmd, f, opts, args[0])
		},
	}
	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().StringVar(&opts.Prefix, "prefix", opts.Prefix, "Prefix to prepend to the current project name")
	cmd.Flags().StringVar(&opts.Description, "description", "", "New project description")
	return cmd
}

func runArchive(cmd *cobra.Command, f *cmdutil.Factory, opts *archiveOptions, projectKey string) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	workspace, host, err := resolveCloudProjectTarget(f, cmd, opts.Workspace)
	if err != nil {
		return err
	}

	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	key := normalizeProjectKey(projectKey)
	oldProject, err := client.GetProject(ctx, workspace, key)
	if err != nil {
		return err
	}
	groupPerms, err := client.ListProjectGroupPermissions(ctx, workspace, key, 0)
	if err != nil {
		return err
	}

	prefix := opts.Prefix
	if prefix == "" {
		return fmt.Errorf("archive prefix cannot be empty")
	}

	oldName := strings.TrimSpace(oldProject.Name)
	newName := prefix + oldName
	if oldName == "" {
		newName = prefix + key
	}

	downgradedGroups := make([]string, 0)
	for _, perm := range groupPerms {
		if perm.Permission == "write" {
			if err := validatePermissionMutationAuth(host); err != nil {
				return err
			}
			break
		}
	}
	for _, perm := range groupPerms {
		if perm.Permission != "write" {
			continue
		}
		if _, err := client.GrantProjectGroupPermission(ctx, workspace, key, perm.Group.Slug, "read"); err != nil {
			return fmt.Errorf("downgrade group %q permission to read: %w", perm.Group.Slug, err)
		}
		downgradedGroups = append(downgradedGroups, perm.Group.Slug)
	}
	sort.Strings(downgradedGroups)

	updated, err := client.UpdateProject(ctx, workspace, key, bbcloud.UpdateProjectInput{
		Name:        newName,
		Description: strings.TrimSpace(opts.Description),
	})
	if err != nil {
		return err
	}

	result := struct {
		Workspace        string   `json:"workspace"`
		Project          string   `json:"project_key"`
		OldName          string   `json:"old_name"`
		NewName          string   `json:"new_name"`
		Prefix           string   `json:"prefix"`
		DowngradedGroups []string `json:"downgraded_groups"`
	}{
		Workspace:        workspace,
		Project:          key,
		OldName:          oldName,
		NewName:          updated.Name,
		Prefix:           prefix,
		DowngradedGroups: downgradedGroups,
	}

	return cmdutil.WriteOutput(cmd, ios.Out, result, func() error {
		_, err := fmt.Fprintf(ios.Out, "Archived project %q by renaming it from %q to %q. Downgraded %d group write permission(s) to read.\n", key, result.OldName, result.NewName, len(result.DowngradedGroups))
		return err
	})
}

func validatePermissionMutationAuth(host *config.Host) error {
	if host != nil && host.AuthMethod == "oauth" {
		return fmt.Errorf("archiving a project with group write permissions requires a scoped API token because Bitbucket does not support OAuth for permission changes; run `bkt auth login https://bitbucket.org --kind cloud --web-token`")
	}
	return nil
}

func newDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	opts := &deleteOptions{}
	cmd := &cobra.Command{
		Use:     "delete <project-key>",
		Aliases: []string{"rm"},
		Short:   "Delete an empty Bitbucket Cloud project",
		Long: `Permanently delete an empty Bitbucket Cloud project.

This command refuses to delete projects that still contain repositories unless
--cascade is passed, in which case it will delete all repositories first. Use
--yes to skip the confirmation prompt in scripts or CI.

This command is only available for Bitbucket Cloud contexts.`,
		Example: `  # Delete an empty Cloud project (will prompt for confirmation)
  bkt project delete WEB

  # Delete a project and all of its repositories
  bkt project delete WEB --workspace my-team --cascade

  # Delete without confirmation
  bkt project delete WEB --workspace my-team --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd, f, opts, args[0])
		},
	}
	cmd.Flags().StringVar(&opts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().BoolVar(&opts.Cascade, "cascade", false, "Delete repositories before deleting the project")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(cmd *cobra.Command, f *cmdutil.Factory, opts *deleteOptions, projectKey string) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	workspace, host, err := resolveCloudProjectTarget(f, cmd, opts.Workspace)
	if err != nil {
		return err
	}

	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	key := normalizeProjectKey(projectKey)
	project, err := client.GetProject(ctx, workspace, key)
	if err != nil {
		return err
	}

	repos, err := listCloudProjectRepos(ctx, client, workspace, key, 0)
	if err != nil {
		return err
	}
	if len(repos) > 0 && !opts.Cascade {
		return fmt.Errorf("project %s still contains %d repositories; delete them first or use --cascade", key, len(repos))
	}

	if !opts.Yes {
		confirmed, err := confirmProjectDelete(ios, f, project.Name, workspace, repos, opts.Cascade)
		if err != nil {
			return err
		}
		if !confirmed {
			_, _ = fmt.Fprintln(ios.Out, "Aborted.")
			return nil
		}
	}

	deletedRepoCount := 0
	if opts.Cascade && len(repos) > 0 {
		deletedRepoCount, err = cascadeDeleteRepositories(ctx, ios, client, workspace, repos)
		if err != nil {
			return err
		}
	}

	if err := client.DeleteProject(ctx, workspace, key); err != nil {
		return err
	}

	payload := struct {
		Workspace    string `json:"workspace"`
		Project      string `json:"project"`
		Name         string `json:"name"`
		Deleted      bool   `json:"deleted"`
		DeletedRepos int    `json:"deleted_repositories"`
		Cascaded     bool   `json:"cascaded"`
	}{Workspace: workspace, Project: key, Name: project.Name, Deleted: true, DeletedRepos: deletedRepoCount, Cascaded: opts.Cascade}

	return cmdutil.WriteOutput(cmd, ios.Out, payload, func() error {
		if opts.Cascade {
			_, err := fmt.Fprintf(ios.Out, "Deleted project %q and %d repositories from workspace %q.\n", key, deletedRepoCount, workspace)
			return err
		}
		_, err := fmt.Fprintf(ios.Out, "Deleted project %q from workspace %q.\n", key, workspace)
		return err
	})
}

func confirmProjectDelete(ios *iostreams.IOStreams, f *cmdutil.Factory, projectName, workspace string, repos []bbcloud.Repository, cascade bool) (bool, error) {
	var prompt string
	if cascade && len(repos) > 0 {
		if _, err := fmt.Fprintf(ios.Out, "Delete project %q from workspace %q?\n", projectName, workspace); err != nil {
			return false, err
		}
		if _, err := fmt.Fprintf(ios.Out, "\nThis will also delete %d repositories:\n", len(repos)); err != nil {
			return false, err
		}
		for _, repo := range repos {
			if _, err := fmt.Fprintf(ios.Out, "  - %s\n", repo.Slug); err != nil {
				return false, err
			}
		}
		if _, err := fmt.Fprintln(ios.Out); err != nil {
			return false, err
		}
		prompt = "Proceed?"
	} else {
		prompt = fmt.Sprintf("Delete project %q from workspace %q?", projectName, workspace)
	}
	return f.Prompt().Confirm(prompt, false)
}

func cascadeDeleteRepositories(ctx context.Context, ios *iostreams.IOStreams, client *bbcloud.Client, workspace string, repos []bbcloud.Repository) (int, error) {
	deletedCount := 0
	for _, repo := range repos {
		if err := client.DeleteRepository(ctx, workspace, repo.Slug); err != nil {
			return deletedCount, fmt.Errorf("failed to delete repository %q: %w", repo.Slug, err)
		}
		deletedCount++
		if _, err := fmt.Fprintf(ios.Out, "  deleted repository %q\n", repo.Slug); err != nil {
			return deletedCount, err
		}
	}
	return deletedCount, nil
}

func resolveCloudProjectTarget(f *cmdutil.Factory, cmd *cobra.Command, workspaceOverride string) (string, *config.Host, error) {
	_, ctxCfg, host, err := cmdutil.ResolveContext(f, cmd, cmdutil.FlagValue(cmd, "context"))
	if err != nil {
		return "", nil, err
	}
	if host.Kind != "cloud" {
		return "", nil, fmt.Errorf("project command supports Bitbucket Cloud contexts only")
	}
	workspace := cmdutil.FirstNonEmpty(workspaceOverride, ctxCfg.Workspace)
	if workspace == "" {
		return "", nil, fmt.Errorf("workspace required; set with --workspace or configure the context default")
	}
	return workspace, host, nil
}

func normalizeProjectKey(projectKey string) string {
	return strings.ToUpper(strings.TrimSpace(projectKey))
}

func cloudProjectVisibility(private bool) string {
	if private {
		return "private"
	}
	return "public"
}

type cloudRepositoryEntry struct {
	Slug   string   `json:"slug"`
	Name   string   `json:"name"`
	UUID   string   `json:"uuid,omitempty"`
	WebURL string   `json:"web_url,omitempty"`
	Clone  []string `json:"clone_urls,omitempty"`
}

func listCloudProjectRepos(ctx context.Context, client *bbcloud.Client, workspace, projectKey string, limit int) ([]bbcloud.Repository, error) {
	key := normalizeProjectKey(projectKey)
	return client.ListProjectRepositories(ctx, workspace, key, limit)
}

func summarizeCloudRepositories(repos []bbcloud.Repository) []cloudRepositoryEntry {
	summaries := make([]cloudRepositoryEntry, 0, len(repos))
	for _, repo := range repos {
		summaries = append(summaries, cloudRepositoryEntry{
			Slug:   repo.Slug,
			Name:   repo.Name,
			UUID:   strings.Trim(repo.UUID, "{}"),
			WebURL: cloudRepoWebURL(repo),
			Clone:  cloudRepoCloneLinks(repo),
		})
	}
	return summaries
}

func cloudRepoWebURL(repo bbcloud.Repository) string {
	if repo.Links.HTML.Href != "" {
		return repo.Links.HTML.Href
	}
	for _, clone := range repo.Links.Clone {
		if strings.EqualFold(clone.Name, "https") {
			return clone.Href
		}
	}
	return ""
}

func cloudRepoCloneLinks(repo bbcloud.Repository) []string {
	var urls []string
	for _, link := range repo.Links.Clone {
		if strings.TrimSpace(link.Href) == "" {
			continue
		}
		urls = append(urls, fmt.Sprintf("%s (%s)", link.Href, link.Name))
	}
	return urls
}
