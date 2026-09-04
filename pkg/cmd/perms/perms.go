package perms

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/avivsinai/bitbucket-cli/internal/config"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
	"github.com/avivsinai/bitbucket-cli/pkg/iostreams"
)

// NewCommand manages repository and project permissions.
func NewCommand(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "perms",
		Short: "Manage Bitbucket permissions",
		Long: `Manage user and group permissions at the project and repository level on Bitbucket.

Grant, revoke, and list permissions for individual users and groups. Project-level
permissions apply to all repositories within that project, while repository-level
permissions override the project defaults for a specific repository.

Data Center contexts use usernames and PROJECT_* / REPO_* permission levels.
Cloud contexts use Atlassian account IDs, UUIDs, or email addresses for users,
group slugs for groups, and lower-case permission levels such as read, write,
and admin. Cloud permission grant and revoke operations require an API token or
app password; Bitbucket does not support OAuth for those mutations.`,
		Example: `  # List who has access to a project (Data Center)
  bkt perms project list --project MYPROJ

  # Grant a user write access to a specific repository (Data Center)
  bkt perms repo grant --project MYPROJ --repo my-service --user jdoe --perm REPO_WRITE

  # Revoke a user's project-level permission (Data Center)
  bkt perms project revoke --project MYPROJ --user jdoe

  # List Cloud project permissions
  bkt perms project list --workspace my-team --project WEB

  # Grant a Cloud user admin access to a repository
  bkt perms repo grant --workspace my-team --repo my-service --user jdoe@example.com --perm admin

  # Grant a Cloud group write access to a project
  bkt perms project grant --workspace my-team --project WEB --group developers --perm write`,
	}

	cmd.AddCommand(newProjectCmd(f))
	cmd.AddCommand(newRepoCmd(f))

	return cmd
}

type projectListOptions struct {
	Project   string
	Workspace string
	Limit     int
}

type projectGrantOptions struct {
	Project    string
	Workspace  string
	Username   string
	Group      string
	Permission string
}

type projectRevokeOptions struct {
	Project   string
	Workspace string
	Username  string
	Group     string
}

func newProjectCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage project-level permissions",
		Long: `Manage project-level permissions on Bitbucket.

Project permissions control default access for all repositories within a project.
You can list current permission entries, grant a permission level to a user or group,
or revoke a permission entirely.

Data Center: valid permission levels are PROJECT_READ, PROJECT_WRITE, and PROJECT_ADMIN.
Cloud: valid permission levels are read, write, create-repo, and admin.`,
		Example: `  # List all users with permissions on a project (Data Center)
  bkt perms project list --project MYPROJ

  # Grant admin access to a user (Data Center)
  bkt perms project grant --project MYPROJ --user jdoe --perm PROJECT_ADMIN

  # Revoke a user's project permission (Data Center)
  bkt perms project revoke --project MYPROJ --user jdoe

  # List Cloud project permissions
  bkt perms project list --workspace my-team --project WEB

  # Grant a Cloud group write access to a project
  bkt perms project grant --workspace my-team --project WEB --group developers --perm write

  # Revoke a Cloud user permission
  bkt perms project revoke --workspace my-team --project WEB --user jdoe@example.com`,
	}

	listOpts := &projectListOptions{Limit: 100}
	list := &cobra.Command{
		Use:   "list",
		Short: "List project permissions",
		Long: `List the permission entries for a Bitbucket project.

Displays each user and group who has been granted explicit access to the project
along with their permission level. Use --limit to control how many entries are
returned; set it to 0 to fetch all.`,
		Example: `  # List permissions for a project (Data Center)
  bkt perms project list --project MYPROJ

  # List Cloud project permissions
  bkt perms project list --workspace my-team --project WEB

  # Output as JSON
  bkt perms project list --project MYPROJ --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectList(cmd, f, listOpts)
		},
	}
	list.Flags().StringVar(&listOpts.Project, "project", "", "Bitbucket project key (required)")
	list.Flags().StringVar(&listOpts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	list.Flags().IntVar(&listOpts.Limit, "limit", listOpts.Limit, "Maximum entries to display (0 for all)")
	_ = list.MarkFlagRequired("project")

	grantOpts := &projectGrantOptions{Permission: ""}
	grant := &cobra.Command{
		Use:   "grant",
		Short: "Grant project permissions",
		Long: `Grant a permission level to a user or group on a Bitbucket project.

The recipient receives the specified permission for the project and inherits it
across all repositories within that project unless overridden at the repository
level.

Data Center: valid values for --perm are PROJECT_READ, PROJECT_WRITE, and PROJECT_ADMIN.
Cloud: valid values for --perm are read, write, create-repo, and admin.
If --perm is omitted it defaults to read.`,
		Example: `  # Grant read access (default) (Data Center)
  bkt perms project grant --project MYPROJ --user jdoe

  # Grant write access (Data Center)
  bkt perms project grant --project MYPROJ --user jdoe --perm PROJECT_WRITE

  # Grant admin access (Cloud)
  bkt perms project grant --workspace my-team --project WEB --user jdoe@example.com --perm admin

  # Grant group access (Cloud)
  bkt perms project grant --workspace my-team --project WEB --group developers --perm write`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectGrant(cmd, f, grantOpts)
		},
	}
	grant.Flags().StringVar(&grantOpts.Project, "project", "", "Bitbucket project key (required)")
	grant.Flags().StringVar(&grantOpts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	grant.Flags().StringVar(&grantOpts.Username, "user", "", "User identifier (account_id, uuid, or email)")
	grant.Flags().StringVar(&grantOpts.Group, "group", "", "Group slug")
	grant.Flags().StringVar(&grantOpts.Permission, "perm", "", "Permission level (defaults to read for Cloud or PROJECT_READ for Data Center)")
	_ = grant.MarkFlagRequired("project")

	revokeOpts := &projectRevokeOptions{}
	revoke := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke project permissions",
		Long: `Revoke a user or group permission on a Bitbucket project.

Removes the explicit project-level permission entry for the specified user or group.
After revocation the recipient loses access granted at the project level, though
individuals may still have access through repository-level or global permissions.`,
		Example: `  # Revoke a user's project permission (Data Center)
  bkt perms project revoke --project MYPROJ --user jdoe

  # Revoke a Cloud user permission
  bkt perms project revoke --workspace my-team --project WEB --user jdoe@example.com

  # Revoke a Cloud group permission
  bkt perms project revoke --workspace my-team --project WEB --group developers`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectRevoke(cmd, f, revokeOpts)
		},
	}
	revoke.Flags().StringVar(&revokeOpts.Project, "project", "", "Bitbucket project key (required)")
	revoke.Flags().StringVar(&revokeOpts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	revoke.Flags().StringVar(&revokeOpts.Username, "user", "", "User identifier (account_id, uuid, or email)")
	revoke.Flags().StringVar(&revokeOpts.Group, "group", "", "Group slug")
	_ = revoke.MarkFlagRequired("project")

	cmd.AddCommand(list, grant, revoke)
	return cmd
}

type repoListOptions struct {
	Project   string
	Workspace string
	Repo      string
	Limit     int
}

type repoGrantOptions struct {
	Project    string
	Workspace  string
	Repo       string
	Username   string
	Group      string
	Permission string
}

type repoRevokeOptions struct {
	Project   string
	Workspace string
	Repo      string
	Username  string
	Group     string
}

func newRepoCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage repository-level permissions",
		Long: `Manage repository-level permissions on Bitbucket.

Repository permissions override the project defaults for a specific repository.
You can list current permission entries, grant a permission level to a user or group,
or revoke a permission entirely.

Data Center: valid permission levels are REPO_READ, REPO_WRITE, and REPO_ADMIN.
Cloud: valid permission levels are read, write, and admin.`,
		Example: `  # List permissions on a repository (Data Center)
  bkt perms repo list --project MYPROJ --repo my-service

  # Grant write access to a user (Data Center)
  bkt perms repo grant --project MYPROJ --repo my-service --user jdoe --perm REPO_WRITE

  # Revoke a user's repository permission (Data Center)
  bkt perms repo revoke --project MYPROJ --repo my-service --user jdoe

  # List Cloud repository permissions
  bkt perms repo list --workspace my-team --repo my-service

  # Grant a Cloud user admin access
  bkt perms repo grant --workspace my-team --repo my-service --user jdoe@example.com --perm admin

  # Grant a Cloud group read access
  bkt perms repo grant --workspace my-team --repo my-service --group developers --perm read`,
	}

	listOpts := &repoListOptions{Limit: 100}
	list := &cobra.Command{
		Use:   "list",
		Short: "List repository permissions",
		Long: `List the permission entries for a Bitbucket repository.

Displays each user and group who has been granted explicit access to the repository
along with their permission level. Use --limit to control how many entries are
returned; set it to 0 to fetch all.`,
		Example: `  # List permissions for a repository (Data Center)
  bkt perms repo list --project MYPROJ --repo my-service

  # List Cloud repository permissions
  bkt perms repo list --workspace my-team --repo my-service

  # Fetch all permission entries
  bkt perms repo list --project MYPROJ --repo my-service --limit 0

  # Output as JSON
  bkt perms repo list --project MYPROJ --repo my-service --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoList(cmd, f, listOpts)
		},
	}
	list.Flags().StringVar(&listOpts.Project, "project", "", "Bitbucket project key (Data Center)")
	list.Flags().StringVar(&listOpts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	list.Flags().StringVar(&listOpts.Repo, "repo", "", "Repository slug (required)")
	list.Flags().IntVar(&listOpts.Limit, "limit", listOpts.Limit, "Maximum entries to display (0 for all)")
	_ = list.MarkFlagRequired("repo")

	grantOpts := &repoGrantOptions{Permission: ""}
	grant := &cobra.Command{
		Use:   "grant",
		Short: "Grant repository permissions",
		Long: `Grant a permission level to a user or group on a Bitbucket repository.

The recipient receives the specified permission for the repository, overriding any
project-level permission they may already have.

Data Center: valid values for --perm are REPO_READ, REPO_WRITE, and REPO_ADMIN.
Cloud: valid values for --perm are read, write, and admin.
If --perm is omitted it defaults to read.`,
		Example: `  # Grant read access (default) (Data Center)
  bkt perms repo grant --project MYPROJ --repo my-service --user jdoe

  # Grant write access (Data Center)
  bkt perms repo grant --project MYPROJ --repo my-service --user jdoe --perm REPO_WRITE

  # Grant admin access (Cloud)
  bkt perms repo grant --workspace my-team --repo my-service --user jdoe@example.com --perm admin

  # Grant group access (Cloud)
  bkt perms repo grant --workspace my-team --repo my-service --group developers --perm read`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoGrant(cmd, f, grantOpts)
		},
	}
	grant.Flags().StringVar(&grantOpts.Project, "project", "", "Bitbucket project key (Data Center)")
	grant.Flags().StringVar(&grantOpts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	grant.Flags().StringVar(&grantOpts.Repo, "repo", "", "Repository slug (required)")
	grant.Flags().StringVar(&grantOpts.Username, "user", "", "User identifier (account_id, uuid, or email)")
	grant.Flags().StringVar(&grantOpts.Group, "group", "", "Group slug")
	grant.Flags().StringVar(&grantOpts.Permission, "perm", "", "Permission level (defaults to read for Cloud or REPO_READ for Data Center)")
	_ = grant.MarkFlagRequired("repo")

	revokeOpts := &repoRevokeOptions{}
	revoke := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke repository permissions",
		Long: `Revoke a user or group permission on a Bitbucket repository.

Removes the explicit repository-level permission entry for the specified user or group.
After revocation the recipient may still have access through project-level or global
permissions.`,
		Example: `  # Revoke a user's repository permission (Data Center)
  bkt perms repo revoke --project MYPROJ --repo my-service --user jdoe

  # Revoke a Cloud user permission
  bkt perms repo revoke --workspace my-team --repo my-service --user jdoe@example.com

  # Revoke a Cloud group permission
  bkt perms repo revoke --workspace my-team --repo my-service --group developers`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepoRevoke(cmd, f, revokeOpts)
		},
	}
	revoke.Flags().StringVar(&revokeOpts.Project, "project", "", "Bitbucket project key (Data Center)")
	revoke.Flags().StringVar(&revokeOpts.Workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	revoke.Flags().StringVar(&revokeOpts.Repo, "repo", "", "Repository slug (required)")
	revoke.Flags().StringVar(&revokeOpts.Username, "user", "", "User identifier (account_id, uuid, or email)")
	revoke.Flags().StringVar(&revokeOpts.Group, "group", "", "Group slug")
	_ = revoke.MarkFlagRequired("repo")

	cmd.AddCommand(list, grant, revoke)
	return cmd
}

func runProjectList(cmd *cobra.Command, f *cmdutil.Factory, opts *projectListOptions) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	override := cmdutil.FlagValue(cmd, "context")
	_, _, host, err := cmdutil.ResolveContext(f, cmd, override)
	if err != nil {
		return err
	}

	switch host.Kind {
	case "dc":
		return runProjectListDC(cmd, f, ios, host, opts)
	case "cloud":
		return runProjectListCloud(cmd, f, ios, host, opts)
	default:
		return fmt.Errorf("unsupported host kind %q", host.Kind)
	}
}

func runProjectListDC(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, opts *projectListOptions) error {
	client, err := cmdutil.NewDCClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	perms, err := client.ListProjectPermissions(ctx, opts.Project, opts.Limit)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"project":     opts.Project,
		"permissions": perms,
	}

	return cmdutil.WriteOutput(cmd, ios.Out, payload, func() error {
		for _, p := range perms {
			if _, err := fmt.Fprintf(ios.Out, "%s\t%s\n", cmdutil.FirstNonEmpty(p.User.FullName, p.User.Name), p.Permission); err != nil {
				return err
			}
		}
		if len(perms) == 0 {
			if _, err := fmt.Fprintln(ios.Out, "No permissions found."); err != nil {
				return err
			}
		}
		return nil
	})
}

func runProjectListCloud(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, opts *projectListOptions) error {
	workspace, err := resolveCloudWorkspace(cmd, f, opts.Workspace)
	if err != nil {
		return err
	}

	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	userPerms, err := client.ListProjectUserPermissions(ctx, workspace, opts.Project, opts.Limit)
	if err != nil {
		return err
	}
	groupPerms, err := client.ListProjectGroupPermissions(ctx, workspace, opts.Project, opts.Limit)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"workspace": workspace,
		"project":   opts.Project,
		"users":     userPerms,
		"groups":    groupPerms,
	}

	return cmdutil.WriteOutput(cmd, ios.Out, payload, func() error {
		if len(userPerms) == 0 && len(groupPerms) == 0 {
			_, err := fmt.Fprintln(ios.Out, "No permissions found.")
			return err
		}
		for _, p := range userPerms {
			if _, err := fmt.Fprintf(ios.Out, "USER\t%s\t%s\t%s\n", p.User.Display, p.User.AccountID, p.Permission); err != nil {
				return err
			}
		}
		for _, p := range groupPerms {
			if _, err := fmt.Fprintf(ios.Out, "GROUP\t%s\t%s\t%s\n", p.Group.Name, p.Group.Slug, p.Permission); err != nil {
				return err
			}
		}
		return nil
	})
}

func runProjectGrant(cmd *cobra.Command, f *cmdutil.Factory, opts *projectGrantOptions) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	override := cmdutil.FlagValue(cmd, "context")
	_, _, host, err := cmdutil.ResolveContext(f, cmd, override)
	if err != nil {
		return err
	}

	switch host.Kind {
	case "dc":
		if err := validateDCUser(opts.Username, opts.Group); err != nil {
			return err
		}
		permission := opts.Permission
		if permission == "" {
			permission = "PROJECT_READ"
		}
		return runProjectGrantDC(cmd, f, ios, host, opts.Project, opts.Username, permission)
	case "cloud":
		if err := validateCloudPermissionMutationAuth(host); err != nil {
			return err
		}
		permission := opts.Permission
		if permission == "" {
			permission = "read"
		}
		workspace, err := resolveCloudWorkspace(cmd, f, opts.Workspace)
		if err != nil {
			return err
		}
		if err := validateUserOrGroup(opts.Username, opts.Group); err != nil {
			return err
		}
		return runProjectGrantCloud(cmd, f, ios, host, workspace, opts.Project, opts.Username, opts.Group, permission)
	default:
		return fmt.Errorf("unsupported host kind %q", host.Kind)
	}
}

func runProjectGrantDC(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, project, username, permission string) error {
	client, err := cmdutil.NewDCClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	if err := client.GrantProjectPermission(ctx, project, username, permission); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(ios.Out, "Granted %s on project %s to %s\n", strings.ToUpper(permission), project, username); err != nil {
		return err
	}
	return nil
}

func runProjectGrantCloud(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, workspace, project, username, groupSlug, permission string) error {
	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	if groupSlug != "" {
		perm, err := client.GrantProjectGroupPermission(ctx, workspace, project, groupSlug, permission)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(ios.Out, "Granted %s on project %s to group %s\n", perm.Permission, project, groupSlug); err != nil {
			return err
		}
		return nil
	}

	user, err := client.ResolveWorkspaceUser(ctx, workspace, username)
	if err != nil {
		return err
	}
	perm, err := client.GrantProjectUserPermission(ctx, workspace, project, user.AccountID, permission)
	if err != nil {
		return err
	}
	identifier := username
	if user.Email != "" {
		identifier = user.Email
	}
	if _, err := fmt.Fprintf(ios.Out, "Granted %s on project %s to user %s\n", perm.Permission, project, identifier); err != nil {
		return err
	}
	return nil
}

func runProjectRevoke(cmd *cobra.Command, f *cmdutil.Factory, opts *projectRevokeOptions) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	override := cmdutil.FlagValue(cmd, "context")
	_, _, host, err := cmdutil.ResolveContext(f, cmd, override)
	if err != nil {
		return err
	}

	switch host.Kind {
	case "dc":
		if err := validateDCUser(opts.Username, opts.Group); err != nil {
			return err
		}
		return runProjectRevokeDC(cmd, f, ios, host, opts.Project, opts.Username)
	case "cloud":
		if err := validateCloudPermissionMutationAuth(host); err != nil {
			return err
		}
		workspace, err := resolveCloudWorkspace(cmd, f, opts.Workspace)
		if err != nil {
			return err
		}
		if err := validateUserOrGroup(opts.Username, opts.Group); err != nil {
			return err
		}
		return runProjectRevokeCloud(cmd, f, ios, host, workspace, opts.Project, opts.Username, opts.Group)
	default:
		return fmt.Errorf("unsupported host kind %q", host.Kind)
	}
}

func runProjectRevokeDC(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, project, username string) error {
	client, err := cmdutil.NewDCClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	if err := client.RevokeProjectPermission(ctx, project, username); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(ios.Out, "Revoked project permission for %s on %s\n", username, project); err != nil {
		return err
	}
	return nil
}

func runProjectRevokeCloud(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, workspace, project, username, groupSlug string) error {
	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	if groupSlug != "" {
		if err := client.RevokeProjectGroupPermission(ctx, workspace, project, groupSlug); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(ios.Out, "Revoked group permission for %s on project %s\n", groupSlug, project); err != nil {
			return err
		}
		return nil
	}

	user, err := client.ResolveWorkspaceUser(ctx, workspace, username)
	if err != nil {
		return err
	}
	if err := client.RevokeProjectUserPermission(ctx, workspace, project, user.AccountID); err != nil {
		return err
	}
	identifier := username
	if user.Email != "" {
		identifier = user.Email
	}
	if _, err := fmt.Fprintf(ios.Out, "Revoked user permission for %s on project %s\n", identifier, project); err != nil {
		return err
	}
	return nil
}

func runRepoList(cmd *cobra.Command, f *cmdutil.Factory, opts *repoListOptions) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	override := cmdutil.FlagValue(cmd, "context")
	_, _, host, err := cmdutil.ResolveContext(f, cmd, override)
	if err != nil {
		return err
	}

	switch host.Kind {
	case "dc":
		if opts.Project == "" {
			return fmt.Errorf("--project is required for Data Center")
		}
		return runRepoListDC(cmd, f, ios, host, opts)
	case "cloud":
		workspace, err := resolveCloudWorkspace(cmd, f, opts.Workspace)
		if err != nil {
			return err
		}
		return runRepoListCloud(cmd, f, ios, host, workspace, opts)
	default:
		return fmt.Errorf("unsupported host kind %q", host.Kind)
	}
}

func runRepoListDC(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, opts *repoListOptions) error {
	client, err := cmdutil.NewDCClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	perms, err := client.ListRepoPermissions(ctx, opts.Project, opts.Repo, opts.Limit)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"project":     opts.Project,
		"repo":        opts.Repo,
		"permissions": perms,
	}

	return cmdutil.WriteOutput(cmd, ios.Out, payload, func() error {
		for _, p := range perms {
			if _, err := fmt.Fprintf(ios.Out, "%s\t%s\n", cmdutil.FirstNonEmpty(p.User.FullName, p.User.Name), p.Permission); err != nil {
				return err
			}
		}
		if len(perms) == 0 {
			if _, err := fmt.Fprintln(ios.Out, "No permissions found."); err != nil {
				return err
			}
		}
		return nil
	})
}

func runRepoListCloud(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, workspace string, opts *repoListOptions) error {
	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	userPerms, err := client.ListRepoUserPermissions(ctx, workspace, opts.Repo, opts.Limit)
	if err != nil {
		return err
	}
	groupPerms, err := client.ListRepoGroupPermissions(ctx, workspace, opts.Repo, opts.Limit)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"workspace": workspace,
		"repo":      opts.Repo,
		"users":     userPerms,
		"groups":    groupPerms,
	}

	return cmdutil.WriteOutput(cmd, ios.Out, payload, func() error {
		if len(userPerms) == 0 && len(groupPerms) == 0 {
			_, err := fmt.Fprintln(ios.Out, "No permissions found.")
			return err
		}
		for _, p := range userPerms {
			if _, err := fmt.Fprintf(ios.Out, "USER\t%s\t%s\t%s\n", p.User.Display, p.User.AccountID, p.Permission); err != nil {
				return err
			}
		}
		for _, p := range groupPerms {
			if _, err := fmt.Fprintf(ios.Out, "GROUP\t%s\t%s\t%s\n", p.Group.Name, p.Group.Slug, p.Permission); err != nil {
				return err
			}
		}
		return nil
	})
}

func runRepoGrant(cmd *cobra.Command, f *cmdutil.Factory, opts *repoGrantOptions) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	override := cmdutil.FlagValue(cmd, "context")
	_, _, host, err := cmdutil.ResolveContext(f, cmd, override)
	if err != nil {
		return err
	}

	switch host.Kind {
	case "dc":
		if opts.Project == "" {
			return fmt.Errorf("--project is required for Data Center")
		}
		if err := validateDCUser(opts.Username, opts.Group); err != nil {
			return err
		}
		permission := opts.Permission
		if permission == "" {
			permission = "REPO_READ"
		}
		return runRepoGrantDC(cmd, f, ios, host, opts.Project, opts.Repo, opts.Username, permission)
	case "cloud":
		if err := validateCloudPermissionMutationAuth(host); err != nil {
			return err
		}
		permission := opts.Permission
		if permission == "" {
			permission = "read"
		}
		workspace, err := resolveCloudWorkspace(cmd, f, opts.Workspace)
		if err != nil {
			return err
		}
		if err := validateUserOrGroup(opts.Username, opts.Group); err != nil {
			return err
		}
		return runRepoGrantCloud(cmd, f, ios, host, workspace, opts.Repo, opts.Username, opts.Group, permission)
	default:
		return fmt.Errorf("unsupported host kind %q", host.Kind)
	}
}

func runRepoGrantDC(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, project, repo, username, permission string) error {
	client, err := cmdutil.NewDCClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	if err := client.GrantRepoPermission(ctx, project, repo, username, permission); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(ios.Out, "Granted %s on %s/%s to %s\n", strings.ToUpper(permission), project, repo, username); err != nil {
		return err
	}
	return nil
}

func runRepoGrantCloud(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, workspace, repo, username, groupSlug, permission string) error {
	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	if groupSlug != "" {
		perm, err := client.GrantRepoGroupPermission(ctx, workspace, repo, groupSlug, permission)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(ios.Out, "Granted %s on repository %s to group %s\n", perm.Permission, repo, groupSlug); err != nil {
			return err
		}
		return nil
	}

	user, err := client.ResolveWorkspaceUser(ctx, workspace, username)
	if err != nil {
		return err
	}
	perm, err := client.GrantRepoUserPermission(ctx, workspace, repo, user.AccountID, permission)
	if err != nil {
		return err
	}
	identifier := username
	if user.Email != "" {
		identifier = user.Email
	}
	if _, err := fmt.Fprintf(ios.Out, "Granted %s on repository %s to user %s\n", perm.Permission, repo, identifier); err != nil {
		return err
	}
	return nil
}

func runRepoRevoke(cmd *cobra.Command, f *cmdutil.Factory, opts *repoRevokeOptions) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}

	override := cmdutil.FlagValue(cmd, "context")
	_, _, host, err := cmdutil.ResolveContext(f, cmd, override)
	if err != nil {
		return err
	}

	switch host.Kind {
	case "dc":
		if opts.Project == "" {
			return fmt.Errorf("--project is required for Data Center")
		}
		if err := validateDCUser(opts.Username, opts.Group); err != nil {
			return err
		}
		return runRepoRevokeDC(cmd, f, ios, host, opts.Project, opts.Repo, opts.Username)
	case "cloud":
		if err := validateCloudPermissionMutationAuth(host); err != nil {
			return err
		}
		workspace, err := resolveCloudWorkspace(cmd, f, opts.Workspace)
		if err != nil {
			return err
		}
		if err := validateUserOrGroup(opts.Username, opts.Group); err != nil {
			return err
		}
		return runRepoRevokeCloud(cmd, f, ios, host, workspace, opts.Repo, opts.Username, opts.Group)
	default:
		return fmt.Errorf("unsupported host kind %q", host.Kind)
	}
}

func runRepoRevokeDC(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, project, repo, username string) error {
	client, err := cmdutil.NewDCClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()

	if err := client.RevokeRepoPermission(ctx, project, repo, username); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(ios.Out, "Revoked repository permission for %s on %s/%s\n", username, project, repo); err != nil {
		return err
	}
	return nil
}

func runRepoRevokeCloud(cmd *cobra.Command, f *cmdutil.Factory, ios *iostreams.IOStreams, host *config.Host, workspace, repo, username, groupSlug string) error {
	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
	defer cancel()

	if groupSlug != "" {
		if err := client.RevokeRepoGroupPermission(ctx, workspace, repo, groupSlug); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(ios.Out, "Revoked group permission for %s on repository %s\n", groupSlug, repo); err != nil {
			return err
		}
		return nil
	}

	user, err := client.ResolveWorkspaceUser(ctx, workspace, username)
	if err != nil {
		return err
	}
	if err := client.RevokeRepoUserPermission(ctx, workspace, repo, user.AccountID); err != nil {
		return err
	}
	identifier := username
	if user.Email != "" {
		identifier = user.Email
	}
	if _, err := fmt.Fprintf(ios.Out, "Revoked user permission for %s on repository %s\n", identifier, repo); err != nil {
		return err
	}
	return nil
}

func resolveCloudWorkspace(cmd *cobra.Command, f *cmdutil.Factory, workspaceOverride string) (string, error) {
	_, ctxCfg, host, err := cmdutil.ResolveContext(f, cmd, cmdutil.FlagValue(cmd, "context"))
	if err != nil {
		return "", err
	}
	if host.Kind != "cloud" {
		return "", fmt.Errorf("command supports Bitbucket Cloud contexts only")
	}
	workspace := cmdutil.FirstNonEmpty(workspaceOverride, ctxCfg.Workspace)
	if workspace == "" {
		return "", fmt.Errorf("workspace required; set with --workspace or configure the context default")
	}
	return workspace, nil
}

func validateUserOrGroup(username, group string) error {
	hasUser := strings.TrimSpace(username) != ""
	hasGroup := strings.TrimSpace(group) != ""
	if hasUser && hasGroup {
		return fmt.Errorf("specify either --user or --group, not both")
	}
	if !hasUser && !hasGroup {
		return fmt.Errorf("specify either --user or --group")
	}
	return nil
}

func validateDCUser(username, group string) error {
	if strings.TrimSpace(group) != "" {
		return fmt.Errorf("--group is only supported for Bitbucket Cloud; use --user for Data Center")
	}
	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("--user is required for Data Center")
	}
	return nil
}

func validateCloudPermissionMutationAuth(host *config.Host) error {
	if host != nil && host.AuthMethod == "oauth" {
		return fmt.Errorf("permission changes on Bitbucket Cloud do not support OAuth; authenticate with a scoped API token using `bkt auth login https://bitbucket.org --kind cloud --web-token`")
	}
	return nil
}
