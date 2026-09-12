// tools/plaprobe/xml.go

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/text/encoding/charmap"
)

func newXMLCmd(p *probe) *cobra.Command {
	return &cobra.Command{
		Use:   "xml FILE",
		Short: "Write the decoded plan XML as utf-8",
		Long: "xml is the one command that prints what is inside the file, so it writes the " +
			"document and nothing else and a redirect produces a usable file. A real plan " +
			"carries teacher names: do not paste the result anywhere.",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file, err := decode(args[0])
			if err != nil {
				return err
			}
			// The payload is iso-8859-2; a mis-decoded Polish name is a silent
			// bug, so the transcode is explicit and its failure is reported.
			utf8XML, err := charmap.ISO8859_2.NewDecoder().Bytes(file.XML)
			if err != nil {
				return fmt.Errorf("transcode %s from iso-8859-2: %w", args[0], err)
			}
			_, err = os.Stdout.Write(utf8XML)
			return err
		},
	}
}
