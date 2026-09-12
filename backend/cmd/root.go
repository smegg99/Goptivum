// cmd/root.go

// Package cmd is goptivum-server's command tree.
package cmd

import (
	"errors"
	"io"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/smegg99/s99logger"
	"github.com/spf13/cobra"

	"github.com/smegg99/goptivum/backend/common/config"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/ui"
	"github.com/smegg99/goptivum/backend/version"
)

// errUsage is answered with exit 2, so a script can tell a misuse from a
// failure of the work itself.
var errUsage = errors.New("usage")

// annotationConfig marks a command that needs the configuration, so help and
// completion do not write a config file as a side effect of being asked for
// help.
const annotationConfig = "goptivum:config"

// server is one command's view of the configuration, built once by the root.
type server struct {
	settings config.Config
	path     string
	flags    options

	stdout  io.Writer
	out     *ui.UI
	words   *i18n.Localizer
	logging bool
}

type options struct {
	path, addr, language string
	verbose, noColor     bool
}

// Execute runs the command tree and returns the process exit code.
func Execute() int {
	root, s := newRootCmd()
	err := root.Execute()
	if err != nil {
		if s.logging {
			logger.Log.Error(s99logger.NewEvent(logger.EventRunFailed, s99logger.Err(err)))
		} else {
			root.PrintErrln(messages.CliCommandError(s.say(), messages.CliCommandErrorParams{Detail: err.Error()}))
		}
	}
	if closeErr := logger.Close(); closeErr != nil && err == nil {
		err = closeErr
		root.PrintErrln(messages.CliCommandError(s.say(), messages.CliCommandErrorParams{Detail: err.Error()}))
	}
	if err == nil {
		return 0
	}

	if errors.Is(err, errUsage) {
		return 2
	}
	return 1
}

// newRootCmd builds the tree. The configuration is read before any subcommand
// runs, so no subcommand loads or overrides anything itself.
func newRootCmd() (*cobra.Command, *server) {
	s := &server{words: messages.Localizer(messages.LanguageFromEnv())}

	root := &cobra.Command{
		Use:           "goptivum-server",
		Version:       version.Release,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Replaces Cobra's own unknown-command error so a misuse exits 2.
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return usageError(messages.CliCommandUnknownCommand(s.say(), messages.CliCommandUnknownCommandParams{Command: args[0]}))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			s.stdout = cmd.OutOrStdout()
			if cmd.Annotations[annotationConfig] == "" {
				return nil
			}
			return s.load(cmd)
		},
	}

	// The configuration file owns the settings. These few flags exist because
	// they are what someone changes for one run; a secret is never among them,
	// because a command line is visible in the process list and in a shell's
	// history.
	flags := root.PersistentFlags()
	flags.StringVar(&s.flags.path, "config", config.Path(), "")
	flags.StringVar(&s.flags.addr, "addr", "", "")
	flags.BoolVar(&s.flags.verbose, "verbose", false, "")
	flags.BoolVar(&s.flags.noColor, "no-color", false, "")
	flags.Func("lang", "", func(value string) error {
		if value != "en" && value != "pl" {
			return errors.New(messages.CliCommandInvalidLanguage(s.say()))
		}
		s.flags.language = value
		s.localize(root, value)
		return nil
	})

	// A flag or argument mistake is a usage error, and exits 2.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError(messages.CliCommandInvalidFlag(s.say(), messages.CliCommandInvalidFlagParams{Detail: err.Error()}))
	})

	root.AddCommand(
		newServeCmd(s),
		newStatusCmd(s),
	)
	s.initLocale(root)
	return root, s
}

// load reads the configuration and applies the flags that override it. A flag
// wins only when it was given: a default must not quietly replace a value the
// school wrote down.
func (s *server) load(cmd *cobra.Command) error {
	settings, resolved, err := config.Load(s.flags.path)
	if err != nil {
		return &commandError{messages.CliCommandConfigFailed(s.say(), messages.CliCommandConfigFailedParams{Detail: err.Error()}), err}
	}
	s.settings, s.path = settings, resolved

	if cmd.Flags().Changed("addr") {
		s.settings.Server.Address = s.flags.addr
	}
	if cmd.Flags().Changed("verbose") {
		s.settings.Logging.Verbose = s.flags.verbose
	}
	if cmd.Flags().Changed("no-color") {
		s.settings.Logging.NoColor = s.flags.noColor
	}
	if cmd.Flags().Changed("lang") {
		s.settings.Application.Language = s.flags.language
	}
	s.words = messages.Localizer(s.settings.Application.Language)

	if err := logger.Configure(s.settings.Logging, s.settings.Application.Language); err != nil {
		return &commandError{messages.CliCommandLoggingFailed(s.say(), messages.CliCommandLoggingFailedParams{Detail: err.Error()}), err}
	}
	s.logging = true
	logger.Log.Debug(s99logger.NewEvent(logger.EventConfigLoaded, s99logger.String("path", resolved)))
	return nil
}

// say resolves messages in the language this program renders in.
func (s *server) say() *i18n.Localizer {
	if s.words == nil {
		s.words = messages.Localizer(s.settings.Application.Language)
	}
	return s.words
}

// print builds the writer on first use. serve only logs, and building the
// writer asks the terminal for its background colour, which costs time a
// service start should not spend.
func (s *server) print() *ui.UI {
	if s.out == nil {
		s.out = ui.New(s.stdout)
	}
	return s.out
}

// noArgs is cobra's own check, answered as a usage error so a stray argument
// exits 2 like a wrong flag does.
func (s *server) noArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return usageError(messages.CliCommandNoArgs(s.say(), messages.CliCommandNoArgsParams{Command: cmd.CommandPath()}))
	}
	return nil
}

func usageError(message string) error {
	return &commandError{message: message, cause: errUsage}
}

type commandError struct {
	message string
	cause   error
}

func (e *commandError) Error() string { return e.message }
func (e *commandError) Unwrap() error { return e.cause }
