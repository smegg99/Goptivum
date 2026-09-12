// cmd/locale.go

package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/version"
)

func (s *server) initLocale(root *cobra.Command) {
	root.InitDefaultCompletionCmd()
	root.InitDefaultHelpCmd()
	for _, cmd := range root.Commands() {
		if cmd.Name() != "help" {
			continue
		}
		cmd.Run = nil
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			target, remaining, err := root.Find(args)
			if err != nil || len(remaining) != 0 {
				return usageError(messages.CliCommandUnknownCommand(s.say(), messages.CliCommandUnknownCommandParams{Command: strings.Join(args, " ")}))
			}
			return target.Help()
		}
	}
	s.localize(root, messages.LanguageFromEnv())
}

// localize runs at construction and when pflag parses --lang, before help or version.
func (s *server) localize(root *cobra.Command, language string) {
	s.words = messages.Localizer(language)
	words := s.say()
	root.Short = messages.CliCommandRootShort(words)
	root.Long = messages.CliCommandRootLong(words)
	root.SetVersionTemplate(messages.CliCommandVersion(words, messages.CliCommandVersionParams{
		Release: version.Release, Api: version.API, Client: version.MinClient,
	}) + "\n")

	flags := root.PersistentFlags()
	flags.Lookup("config").Usage = messages.CliCommandConfigFlag(words)
	flags.Lookup("addr").Usage = messages.CliCommandAddrFlag(words)
	flags.Lookup("verbose").Usage = messages.CliCommandVerboseFlag(words)
	flags.Lookup("no-color").Usage = messages.CliCommandNoColorFlag(words)
	flags.Lookup("lang").Usage = messages.CliCommandLangFlag(words)

	// Keep Cobra's command layout and flag rendering; translate its template labels.
	root.SetUsageTemplate("")
	root.SetUsageTemplate(strings.NewReplacer(
		"Usage:", messages.CliCommandUsage(words),
		"Available Commands:", messages.CliCommandCommands(words),
		"Global Flags:", messages.CliCommandGlobalFlags(words),
		"Flags:", messages.CliCommandFlags(words),
		`Use "{{.CommandPath}} [command] --help" for more information about a command.`,
		messages.CliCommandHelpHint(words, messages.CliCommandHelpHintParams{Command: "{{.CommandPath}} [command] --help"}),
	).Replace(root.UsageTemplate()))

	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		switch cmd.Name() {
		case "serve":
			cmd.Short = messages.CliCommandServeShort(words)
		case "status":
			cmd.Short = messages.CliCommandStatusShort(words)
			cmd.Long = messages.CliCommandStatusLong(words)
		case "help":
			cmd.Short = messages.CliCommandHelpShort(words)
			cmd.Long = cmd.Short
		case "completion":
			cmd.Short = messages.CliCommandCompletionShort(words)
			cmd.Long = cmd.Short
		case "bash", "zsh", "fish", "powershell":
			cmd.Short = messages.CliCommandShellShort(words, messages.CliCommandShellShortParams{Shell: cmd.Name()})
			cmd.Long = cmd.Short
		}
		cmd.InitDefaultHelpFlag()
		cmd.InitDefaultVersionFlag()
		cmd.Flags().Lookup("help").Usage = messages.CliCommandHelpFlag(words)
		if flag := cmd.Flags().Lookup("version"); flag != nil {
			flag.Usage = messages.CliCommandVersionFlag(words)
		}
		if flag := cmd.Flags().Lookup("no-descriptions"); flag != nil {
			flag.Usage = messages.CliCommandNoDescriptionsFlag(words)
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}
