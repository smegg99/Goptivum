// cmd/arrango.go

package cmd

import (
	"context"
	"fmt"

	"github.com/smegg99/s99ab/data/db"
	"github.com/spf13/cobra"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/backend/ui"
	"github.com/smegg99/goptivum/backend/version"
	"github.com/smegg99/goptivum/solveclient"
)

// newArrangoCmd is the operator's surface on the solving nodes. Pairing over
// HTTP is admin-only and comes with the contract; this is how the box itself
// does it.
func newArrangoCmd(s *server) *cobra.Command {
	arrangoCmd := &cobra.Command{
		Use:         "arrango",
		Annotations: map[string]string{annotationConfig: "yes"},
		Args:        s.noArgs,
	}
	arrangoCmd.AddCommand(
		newArrangoPairCmd(s),
		newArrangoNodesCmd(s),
		newArrangoRevokeCmd(s),
	)
	return arrangoCmd
}

func newArrangoPairCmd(s *server) *cobra.Command {
	var label string
	pair := &cobra.Command{
		Use:         "pair",
		Annotations: map[string]string{annotationConfig: "yes"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.arrangoPair(cmd.Context(), args[0], label)
		},
	}
	pair.Flags().StringVar(&label, "label", "goptivum", "how this node is named locally")
	return pair
}

func newArrangoNodesCmd(s *server) *cobra.Command {
	return &cobra.Command{
		Use:         "nodes",
		Annotations: map[string]string{annotationConfig: "yes"},
		Args:        s.noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return s.arrangoNodes(cmd.Context())
		},
	}
}

func newArrangoRevokeCmd(s *server) *cobra.Command {
	return &cobra.Command{
		Use:         "revoke",
		Annotations: map[string]string{annotationConfig: "yes"},
		Args:        cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.arrangoRevoke(cmd.Context(), args[0])
		},
	}
}

// arrangoRegistry opens the index. The caller closes the connection.
func (s *server) arrangoRegistry() (*arrango.Registry, func() error, error) {
	database := s.settings.Storage.Database
	conn, err := db.Open(db.Config{Driver: database.Driver, DSN: s.settings.DatabaseDSN()})
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", database.Driver, err)
	}
	if err := db.Migrate(conn, migrations.FS, database.Driver); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("migrate: %w", err)
	}
	return arrango.NewRegistry(conn, database.Driver), conn.Close, nil
}

func (s *server) arrangoPair(ctx context.Context, uri, label string) error {
	if uri == "" {
		return fmt.Errorf("%s", messages.CliArrangoNeedURI(s.say()))
	}
	self, err := solveclient.LoadIdentity(s.settings.Arrango.IdentityDir)
	if err != nil {
		return err
	}
	dialer, err := solveclient.NewDialer(self, version.Client())
	if err != nil {
		return err
	}

	// The token travels only after the presented certificate matched the pin.
	paired, err := dialer.Pair(ctx, uri)
	if err != nil {
		return err
	}

	registry, closeDB, err := s.arrangoRegistry()
	if err != nil {
		return err
	}
	defer closeDB()

	node, err := registry.Record(ctx, label, paired)
	if err != nil {
		return err
	}

	out, say := s.print(), s.say()
	out.Line(out.S.Title, "%s", messages.CliArrangoPaired(say))
	out.Blank()
	out.Fields(
		ui.Field{Label: messages.CliArrangoLabel(say), Value: node.Label},
		ui.Field{Label: messages.CliArrangoAddress(say), Value: node.Address},
		ui.Field{Label: messages.CliArrangoFingerprint(say), Value: node.Fingerprint},
		ui.Field{Label: messages.CliArrangoVersions(say),
			Value: fmt.Sprintf("api %s, solver %s, clients >= %s",
				paired.NodeAPI, paired.Solver, paired.MinClient)},
		// The node pinned this, so replacing the identity directory means
		// every node must pair again.
		ui.Field{Label: messages.CliArrangoOurFingerprint(say), Value: self.Fingerprint().String()},
	)
	out.Blank()
	return nil
}

func (s *server) arrangoNodes(ctx context.Context) error {
	registry, closeDB, err := s.arrangoRegistry()
	if err != nil {
		return err
	}
	defer closeDB()

	nodes, err := registry.List(ctx)
	if err != nil {
		return err
	}

	out, say := s.print(), s.say()
	out.Line(out.S.Title, "%s", messages.CliArrangoNodes(say))
	out.Blank()
	if len(nodes) == 0 {
		out.Line(out.S.Muted, "%s", messages.CliArrangoNone(say))
		return nil
	}

	rows := [][]string{}
	for _, node := range nodes {
		state := out.State(true, messages.CliArrangoActive(say))
		if node.Revoked() {
			state = out.Warning(messages.CliArrangoRevoked(say))
		}
		rows = append(rows, []string{node.Label, node.Address, node.Fingerprint, state})
	}
	out.Table([]string{
		messages.CliArrangoLabel(say), messages.CliArrangoAddress(say),
		messages.CliArrangoFingerprint(say), messages.CliArrangoState(say),
	}, rows)
	return nil
}

func (s *server) arrangoRevoke(ctx context.Context, label string) error {
	registry, closeDB, err := s.arrangoRegistry()
	if err != nil {
		return err
	}
	defer closeDB()

	nodes, err := registry.List(ctx)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if node.Label != label && node.Fingerprint != label {
			continue
		}
		if err := registry.Revoke(ctx, node.ID); err != nil {
			return err
		}
		out, say := s.print(), s.say()
		out.Line(out.S.Title, "%s", messages.CliArrangoRevokedNode(say))
		out.Fields(ui.Field{Label: messages.CliArrangoLabel(say), Value: node.Label})
		return nil
	}
	return arrango.ErrNoNode
}
