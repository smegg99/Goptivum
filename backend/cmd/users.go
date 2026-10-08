// cmd/users.go

package cmd

import (
	"context"
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/identity"
	"github.com/smegg99/goptivum/backend/ui"
)

// newUsersCmd is the operator's way to the school's accounts from the server
// host. It talks to Kratos's admin API directly, so it works while the server
// runs and without an administrator signed in.
func newUsersCmd(s *server) *cobra.Command {
	users := &cobra.Command{
		Use:         "users",
		Annotations: map[string]string{annotationConfig: "yes"},
		Args:        s.noArgs,
	}
	users.AddCommand(newUsersSetRoleCmd(s))
	return users
}

func newUsersSetRoleCmd(s *server) *cobra.Command {
	return &cobra.Command{
		Use:         "set-role IDENTIFIER admin|planner",
		Annotations: map[string]string{annotationConfig: "yes"},
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 || strings.TrimSpace(args[0]) == "" {
				return usageError(messages.KeyCliUsersSetRoleArgs, nil)
			}
			if args[1] != identity.RoleAdmin && args[1] != identity.RolePlanner {
				return usageError(messages.KeyCliUsersUnknownRole, map[string]any{"Role": args[1]})
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.usersSetRole(cmd.Context(), args[0], args[1])
		},
	}
}

// usersSetRole sets an account's role through the same last-admin guard the
// Users page uses. The role is read from Kratos on every request, so it
// applies at the account's next request with no restart.
func (s *server) usersSetRole(ctx context.Context, identifier, role string) error {
	settings := s.settings.Identity
	if settings.AdminURL == "" {
		return messages.NewFault(messages.KeyCliUsersNoIdentity, nil)
	}
	accounts, err := identity.NewAdmin(settings.AdminURL, seconds(settings.TimeoutSeconds))
	if err != nil {
		return err
	}
	found, err := accounts.FindByIdentifier(ctx, identifier)
	if errors.Is(err, identity.ErrNotFound) {
		return messages.NewFault(messages.KeyCliUsersNoAccount, map[string]any{"Identifier": strings.TrimSpace(identifier)})
	}
	if err != nil {
		return err
	}
	account, err := accounts.Update(ctx, found.ID, identity.Change{Role: &role})
	if errors.Is(err, identity.ErrLastAdmin) {
		return messages.WrapFault(err, messages.KeyUsersLastAdmin, nil)
	}
	if err != nil {
		return err
	}

	out, say := s.print(), s.say()
	state := out.State(true, messages.CliUsersEnabled(say))
	if !account.Active {
		state = out.Warning(messages.CliUsersDisabled(say))
	}
	out.Line(out.S.Title, "%s", messages.CliUsersRoleSet(say))
	out.Blank()
	out.Fields(
		ui.Field{Label: messages.CliUsersUsername(say), Value: account.Username},
		ui.Field{Label: messages.CliUsersEmail(say), Value: account.Email},
		ui.Field{Label: messages.CliUsersRole(say), Value: account.Role},
		ui.Field{Label: messages.CliUsersState(say), Value: state},
	)
	out.Blank()
	return nil
}
