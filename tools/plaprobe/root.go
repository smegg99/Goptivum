// tools/plaprobe/root.go

package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/spf13/cobra"
	"golang.org/x/text/encoding/charmap"

	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
	"github.com/smegg99/goptivum/backend/ui"
)

// errUsage is answered with exit 2, so a script can tell a misuse from a file
// that could not be read.
var errUsage = errors.New("usage")

// probe is one command's view of a decoded file.
type probe struct {
	out *ui.UI
	say *i18n.Localizer
}

func execute() int {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "plaprobe: %v\n", err)
		if errors.Is(err, errUsage) {
			return 2
		}
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	p := &probe{}

	root := &cobra.Command{
		Use:   "plaprobe",
		Short: "Look inside an Optivum plan file without printing a school",
		Long: "plaprobe decodes a .pla container and reports its shape. census and tree " +
			"disclose element names, attribute names and value types, never values, so a " +
			"real school file can be inspected and pasted into a bug report.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%w: unknown command %q", errUsage, args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			p.out = ui.New(cmd.OutOrStdout())
			// This tool has no configuration file, so the environment names
			// the language it prints in.
			p.say = messages.Localizer(messages.LanguageFromEnv())
			return nil
		},
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %w", errUsage, err)
	})

	root.AddCommand(newCensusCmd(p), newTreeCmd(p), newXMLCmd(p))
	return root
}

// exactArgs is cobra's own check, answered as a usage error so a wrong number
// of arguments exits 2 like a wrong flag does.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(n)(cmd, args); err != nil {
			return fmt.Errorf("%w: %w", errUsage, err)
		}
		return nil
	}
}

// decode reads a container and returns its payload, still iso-8859-2.
func decode(path string) (pla.File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pla.File{}, err
	}
	file, err := pla.Decode(raw)
	if err != nil {
		return pla.File{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return file, nil
}

// header says what the container is. It is not printed by the xml command,
// which writes a document someone redirects into a file.
func (p *probe) header(path string, file pla.File, size int) {
	p.out.Line(p.out.S.Title, "plaprobe")
	p.out.Blank()
	p.out.Fields(
		ui.Field{Label: messages.CliProbeFile(p.say), Value: path},
		ui.Field{Label: messages.CliProbeMagic(p.say), Value: file.Magic, Style: p.out.S.Accent},
		ui.Field{Label: messages.CliProbeContainer(p.say), Value: messages.CliProbeBytes(p.say,
			messages.CliProbeBytesParams{Size: strconv.Itoa(size)})},
		ui.Field{Label: messages.CliProbePayload(p.say), Value: messages.CliProbePayloadEncoding(p.say,
			messages.CliProbePayloadEncodingParams{Size: strconv.Itoa(len(file.XML))})},
		ui.Field{Label: messages.CliProbeCompressed(p.say),
			Value: p.out.State(file.Compressed, p.yesNo(file.Compressed))},
	)
	p.out.Blank()
}

func (p *probe) yesNo(value bool) string {
	if value {
		return messages.CliProbeYes(p.say)
	}
	return messages.CliProbeNo(p.say)
}

// charsetReader resolves the encodings an Optivum plan declares. The prolog
// says iso-8859-2, and parsing pre-transcoded bytes makes encoding/xml refuse
// the document outright.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(label) {
	case "iso-8859-2", "iso8859-2", "iso_8859-2", "latin2", "l2":
		return charmap.ISO8859_2.NewDecoder().Reader(input), nil
	case "utf-8", "utf8", "":
		return input, nil
	default:
		return nil, fmt.Errorf("unsupported charset %q", label)
	}
}

func newDecoder(payload []byte) *xml.Decoder {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	decoder.CharsetReader = charsetReader
	return decoder
}
