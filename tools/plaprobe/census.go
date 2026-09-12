// tools/plaprobe/census.go

package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/ui"
)

func newCensusCmd(p *probe) *cobra.Command {
	return &cobra.Command{
		Use:   "census FILE",
		Short: "Count every element and name its attributes",
		Long:  "census is how the schema notes were produced: element names, counts and attribute names, no values.",
		Args:  exactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return p.census(args[0])
		},
	}
}

func (p *probe) census(path string) error {
	file, err := decode(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	p.header(path, file, int(info.Size()))

	counts := map[string]int{}
	attributes := map[string]map[string]struct{}{}

	decoder := newDecoder(file.XML)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		name := start.Name.Local
		counts[name]++
		if attributes[name] == nil {
			attributes[name] = map[string]struct{}{}
		}
		for _, attribute := range start.Attr {
			attributes[name][attribute.Name.Local] = struct{}{}
		}
	}

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})

	rows := make([][]string, 0, len(names))
	for _, name := range names {
		rows = append(rows, []string{
			name, strconv.Itoa(counts[name]), strings.Join(sorted(attributes[name]), " "),
		})
	}
	p.out.Table([]string{
		messages.CliProbeElement(p.say),
		messages.CliProbeCount(p.say),
		messages.CliProbeAttributes(p.say),
	}, rows)
	p.out.Blank()
	p.out.Fields(ui.Field{Label: messages.CliProbeElements(p.say), Value: strconv.Itoa(len(names))})
	return nil
}

func sorted(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
