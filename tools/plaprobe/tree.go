// tools/plaprobe/tree.go

package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/smegg99/goptivum/backend/common/messages"
)

func newTreeCmd(p *probe) *cobra.Command {
	return &cobra.Command{
		Use:   "tree FILE",
		Short: "Show where each element sits and what type its attributes hold",
		Long:  "tree prints the element tree with the value shape of every attribute, never a value.",
		Args:  exactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return p.tree(args[0])
		},
	}
}

type element struct {
	parents map[string]struct{}
	attrs   map[string]map[string]struct{}
}

func (p *probe) tree(path string) error {
	file, err := decode(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	p.header(path, file, int(info.Size()))

	elements := map[string]*element{}
	var stack []string

	decoder := newDecoder(file.XML)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		switch node := token.(type) {
		case xml.StartElement:
			name := node.Name.Local
			if elements[name] == nil {
				elements[name] = &element{
					parents: map[string]struct{}{},
					attrs:   map[string]map[string]struct{}{},
				}
			}
			parent := "(root)"
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			elements[name].parents[parent] = struct{}{}
			for _, attribute := range node.Attr {
				key := attribute.Name.Local
				if elements[name].attrs[key] == nil {
					elements[name].attrs[key] = map[string]struct{}{}
				}
				elements[name].attrs[key][shapeOf(attribute.Value)] = struct{}{}
			}
			stack = append(stack, name)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	names := make([]string, 0, len(elements))
	for name := range elements {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		p.out.Line(p.out.S.Accent, "%s", name)
		p.out.Line(p.out.S.Muted, "  %s", messages.CliProbeInside(p.say,
			messages.CliProbeInsideParams{Parents: strings.Join(sorted(elements[name].parents), ", ")}))
		rows := make([][]string, 0, len(elements[name].attrs))
		for _, key := range sorted(keysOf(elements[name].attrs)) {
			rows = append(rows, []string{"@" + key, strings.Join(sorted(elements[name].attrs[key]), " ")})
		}
		if len(rows) > 0 {
			p.out.Table([]string{
				messages.CliProbeAttribute(p.say),
				messages.CliProbeValueShape(p.say),
			}, rows)
		}
		p.out.Blank()
	}
	return nil
}

func keysOf(attrs map[string]map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(attrs))
	for key := range attrs {
		out[key] = struct{}{}
	}
	return out
}

// Shapes a value can have. They describe a value without disclosing it.
var (
	integer = regexp.MustCompile(`^-?\d+$`)
	clock   = regexp.MustCompile(`^\s*\d{1,2}:\d{2}$`)
	colour  = regexp.MustCompile(`^#?[0-9A-Fa-f]{6}$`)
	code    = regexp.MustCompile(`^[A-Za-z]{1,4}\d*$`)
	numbers = regexp.MustCompile(`^[\d,; ]+$`)
)

// shapeOf classifies a value without disclosing it.
func shapeOf(value string) string {
	switch {
	case value == "":
		return "empty"
	case integer.MatchString(value):
		return "int"
	case clock.MatchString(value):
		return "time"
	case colour.MatchString(value):
		return "colour"
	case code.MatchString(value):
		return "code"
	case numbers.MatchString(value):
		return "int-list"
	default:
		return fmt.Sprintf("text(len<=%d)", roundUp(len(value)))
	}
}

func roundUp(n int) int { return ((n / 8) + 1) * 8 }
