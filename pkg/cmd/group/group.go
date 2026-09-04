package group

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/avivsinai/bitbucket-cli/internal/config"
	"github.com/avivsinai/bitbucket-cli/pkg/cmdutil"
)

// NewCommand manages Bitbucket Cloud's legacy workspace groups.
func NewCommand(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Manage Bitbucket Cloud workspace groups",
		Long: `Manage Bitbucket Cloud workspace groups through the legacy 1.0 group APIs.

These commands are Cloud-only and use the active context workspace unless
--workspace is supplied. Review group membership and resource permissions before
deleting a group because deletion can remove access across multiple projects and
repositories.`,
		Example: `  bkt group list --workspace my-team
  bkt group create developers --workspace my-team
  bkt group member add developers --user user@example.com --workspace my-team`,
	}
	cmd.AddCommand(newListCmd(f), newCreateCmd(f), newDeleteCmd(f), newMemberCmd(f))
	return cmd
}

// NewInviteCommand invites an email address into a Bitbucket Cloud group.
func NewInviteCommand(f *cmdutil.Factory) *cobra.Command {
	var workspace, groupSlug string
	cmd := &cobra.Command{
		Use: "invite <email>", Short: "Invite a user to a Bitbucket Cloud group", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, host, err := target(f, cmd, workspace)
			if err != nil {
				return err
			}
			client, err := cmdutil.NewHTTPClient(host)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			req, err := client.NewRequest(ctx, "PUT", legacyURL(host, "/users/"+url.PathEscape(ws)+"/invitations"), map[string]string{"email": args[0], "group_slug": groupSlug})
			if err != nil {
				return err
			}
			var response any
			if err := client.Do(req, &response); err != nil {
				return err
			}
			return write(cmd, f, map[string]string{"workspace": ws, "email": args[0], "group": groupSlug}, fmt.Sprintf("Invited %q to group %q in workspace %q.\n", args[0], groupSlug, ws))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().StringVar(&groupSlug, "group", "", "Group slug (required)")
	_ = cmd.MarkFlagRequired("group")
	return cmd
}

func newListCmd(f *cmdutil.Factory) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{Use: "list", Aliases: []string{"ls"}, Short: "List workspace groups", RunE: func(cmd *cobra.Command, _ []string) error {
		ws, host, err := target(f, cmd, workspace)
		if err != nil {
			return err
		}
		client, err := cmdutil.NewHTTPClient(host)
		if err != nil {
			return err
		}
		req, err := client.NewRequest(cmd.Context(), "GET", legacyURL(host, "/groups/"+url.PathEscape(ws)+"/"), nil)
		if err != nil {
			return err
		}
		var groups any
		if err := client.Do(req, &groups); err != nil {
			return err
		}
		return write(cmd, f, groups, "Listed workspace groups.\n")
	}}
	cmd.Flags().StringVar(&workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	return cmd
}

func newCreateCmd(f *cmdutil.Factory) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{Use: "create <name>", Short: "Create a workspace group", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		ws, host, err := target(f, cmd, workspace)
		if err != nil {
			return err
		}
		client, err := cmdutil.NewHTTPClient(host)
		if err != nil {
			return err
		}
		req, err := client.NewRequest(cmd.Context(), "POST", legacyURL(host, "/groups/"+url.PathEscape(ws)+"/"), map[string]string{"name": args[0]})
		if err != nil {
			return err
		}
		var group any
		if err := client.Do(req, &group); err != nil {
			return err
		}
		return write(cmd, f, group, fmt.Sprintf("Created group %q in workspace %q.\n", args[0], ws))
	}}
	cmd.Flags().StringVar(&workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	return cmd
}

func newDeleteCmd(f *cmdutil.Factory) *cobra.Command {
	var workspace string
	var yes bool
	cmd := &cobra.Command{Use: "delete <group-slug>", Aliases: []string{"rm"}, Short: "Delete a workspace group", Long: `Delete a Bitbucket Cloud workspace group.

The command prompts for confirmation by default because deleting a group can
remove access across multiple projects and repositories. Use --yes to skip the
prompt for non-interactive operation.`, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		ws, host, err := target(f, cmd, workspace)
		if err != nil {
			return err
		}
		if !yes {
			confirmed, err := f.Prompt().Confirm(fmt.Sprintf("Delete group %q from workspace %q?", args[0], ws), false)
			if err != nil {
				return err
			}
			if !confirmed {
				ios, streamErr := f.Streams()
				if streamErr != nil {
					return streamErr
				}
				_, err = fmt.Fprintln(ios.Out, "Aborted.")
				return err
			}
		}
		client, err := cmdutil.NewHTTPClient(host)
		if err != nil {
			return err
		}
		req, err := client.NewRequest(cmd.Context(), "DELETE", legacyURL(host, "/groups/"+url.PathEscape(ws)+"/"+url.PathEscape(args[0])+"/"), nil)
		if err != nil {
			return err
		}
		if err := client.Do(req, nil); err != nil {
			return err
		}
		return write(cmd, f, map[string]string{"workspace": ws, "group": args[0]}, fmt.Sprintf("Deleted group %q from workspace %q.\n", args[0], ws))
	}}
	cmd.Flags().StringVar(&workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

func newMemberCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "member", Short: "Manage workspace group members"}
	cmd.AddCommand(newMemberListCmd(f), newMemberChangeCmd(f, "add"), newMemberChangeCmd(f, "remove"))
	return cmd
}

func newMemberListCmd(f *cmdutil.Factory) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{Use: "list <group-slug>", Aliases: []string{"ls"}, Short: "List group members", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		ws, host, err := target(f, cmd, workspace)
		if err != nil {
			return err
		}
		client, err := cmdutil.NewHTTPClient(host)
		if err != nil {
			return err
		}
		req, err := client.NewRequest(cmd.Context(), "GET", legacyURL(host, memberPath(ws, args[0])), nil)
		if err != nil {
			return err
		}
		var members any
		if err := client.Do(req, &members); err != nil {
			return err
		}
		return write(cmd, f, members, "Listed group members.\n")
	}}
	cmd.Flags().StringVar(&workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	return cmd
}

func newMemberChangeCmd(f *cmdutil.Factory, action string) *cobra.Command {
	var workspace, user string
	method := "PUT"
	past := "Added"
	verb := "Add"
	if action == "remove" {
		method, past, verb = "DELETE", "Removed", "Remove"
	}
	cmd := &cobra.Command{Use: action + " <group-slug>", Short: verb + " a group member", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		ws, host, err := target(f, cmd, workspace)
		if err != nil {
			return err
		}
		member, err := resolveMember(cmd.Context(), host, ws, user)
		if err != nil {
			return err
		}
		client, err := cmdutil.NewHTTPClient(host)
		if err != nil {
			return err
		}
		req, err := client.NewRequest(cmd.Context(), method, legacyURL(host, memberPath(ws, args[0])+"/"+url.PathEscape(member)), map[string]any{})
		if err != nil {
			return err
		}
		var result any
		if err := client.Do(req, &result); err != nil {
			return err
		}
		return write(cmd, f, map[string]string{"workspace": ws, "group": args[0], "user": member}, fmt.Sprintf("%s user %q %s group %q.\n", past, member, map[string]string{"add": "to", "remove": "from"}[action], args[0]))
	}}
	cmd.Flags().StringVar(&workspace, "workspace", "", "Bitbucket workspace override (Cloud)")
	cmd.Flags().StringVar(&user, "user", "", "User UUID, account ID, or email (required)")
	_ = cmd.MarkFlagRequired("user")
	return cmd
}

func target(f *cmdutil.Factory, cmd *cobra.Command, override string) (string, *config.Host, error) {
	_, ctxCfg, host, err := cmdutil.ResolveContext(f, cmd, cmdutil.FlagValue(cmd, "context"))
	if err != nil {
		return "", nil, err
	}
	if host.Kind != "cloud" {
		return "", nil, fmt.Errorf("group commands support Bitbucket Cloud contexts only")
	}
	ws := cmdutil.FirstNonEmpty(override, ctxCfg.Workspace)
	if ws == "" {
		return "", nil, fmt.Errorf("workspace required; set --workspace or configure the context")
	}
	return ws, host, nil
}

func legacyURL(host *config.Host, path string) string {
	u, _ := url.Parse(host.BaseURL)
	u.Path = "/1.0" + path
	u.RawQuery = ""
	return u.String()
}

func memberPath(workspace, group string) string {
	return "/groups/" + url.PathEscape(workspace) + "/" + url.PathEscape(group) + "/members"
}

func resolveMember(ctx context.Context, host *config.Host, workspace, user string) (string, error) {
	client, err := cmdutil.NewCloudClient(host)
	if err != nil {
		return "", err
	}
	resolved, err := client.ResolveWorkspaceUser(ctx, workspace, user)
	if err != nil {
		return "", err
	}
	if resolved.UUID != "" {
		return resolved.UUID, nil
	}
	return resolved.AccountID, nil
}

func write(cmd *cobra.Command, f *cmdutil.Factory, value any, text string) error {
	ios, err := f.Streams()
	if err != nil {
		return err
	}
	return cmdutil.WriteOutput(cmd, ios.Out, value, func() error { _, err := fmt.Fprint(ios.Out, text); return err })
}
