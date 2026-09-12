// formats/optivum/xml/census.go

package xml

import (
	"bytes"
	"encoding/xml"
	"io"
	"sort"
)

// census counts every element in the document by its full path. The reader
// diffs it against what document.go consumes, so an element VULCAN adds in a
// later version surfaces as a Finding instead of disappearing.
type census struct {
	counts map[string]int
	filled map[string]bool // path has at least one non-empty attribute value
	order  []string        // first-seen order, so findings are deterministic
}

// consumed is every element path the reader reads. A path absent from this
// list and present in the document becomes an unmapped_element finding.
//
// The three meanings of <p> are dispatched here, by parent and never by tag
// name: przydzialy/p is a lesson block, info/p is a marked slot, and
// dyzuryplan/p is a duty roster this reader does not model.
var consumed = map[string]bool{
	"plan":                                      true,
	"plan/placowka":                             true,
	"plan/dni":                                  true,
	"plan/dni/dzien":                            true,
	"plan/lekcje":                               true,
	"plan/lekcje/lekcja":                        true,
	"plan/nauczyciele":                          true,
	"plan/nauczyciele/nauczyciel":               true,
	"plan/nauczyciele/nauczyciel/info":          true,
	"plan/nauczyciele/nauczyciel/info/p":        true,
	"plan/przedmioty":                           true,
	"plan/przedmioty/przedmiot":                 true,
	"plan/budynki":                              true,
	"plan/budynki/budynek":                      true,
	"plan/sale":                                 true,
	"plan/sale/sala":                            true,
	"plan/sale/sala/info":                       true,
	"plan/sale/sala/info/p":                     true,
	"plan/oddzialy":                             true,
	"plan/oddzialy/oddzial":                     true,
	"plan/oddzialy/oddzial/info":                true,
	"plan/oddzialy/oddzial/info/p":              true,
	"plan/oddzialy/oddzial/grupy":               true,
	"plan/oddzialy/oddzial/grupy/podzial":       true,
	"plan/oddzialy/oddzial/grupy/podzial/grupa": true,
	"plan/zbiory":                               true,
	"plan/zbiory/zbior":                         true,
	"plan/przydzialy":                           true,
	"plan/przydzialy/p":                         true,
}

// elementCensus walks the raw payload once. It is tolerant on purpose: the
// structural parse reports malformed XML, this pass only describes shape.
func elementCensus(raw []byte) (*census, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.CharsetReader = charsetReader

	c := &census{counts: map[string]int{}, filled: map[string]bool{}}
	var stack []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			path := element.Name.Local
			if len(stack) > 0 {
				path = stack[len(stack)-1] + "/" + path
			}
			stack = append(stack, path)
			if c.counts[path] == 0 {
				c.order = append(c.order, path)
			}
			c.counts[path]++
			for _, attr := range element.Attr {
				if attr.Value != "" {
					c.filled[path] = true
					break
				}
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return c, nil
}

// unmapped returns the element paths carrying data the reader does not read.
// An empty element loses nothing, so only paths with a non-empty attribute
// are reported: a bare container is shape, not content.
func (c *census) unmapped() []string {
	var out []string
	for _, path := range c.order {
		if consumed[path] || !c.filled[path] {
			continue
		}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
