// optivum/charset.go

package optivum

import (
	"bytes"
	"mime"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/charmap"
)

// utf8BOM starts a page some editors saved as UTF-8.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// decodePage turns a page into UTF-8 by the charset it declares. VULCAN has
// written utf-8, iso-8859-2 and windows-1250 exports; a page that declares
// nothing is UTF-8 when it is valid UTF-8, and windows-1250 otherwise, because
// the export comes from Polish Windows software.
func decodePage(data []byte) []byte {
	if bytes.HasPrefix(data, utf8BOM) {
		return data[len(utf8BOM):]
	}
	if label := declaredCharset(data); label != "" {
		if encoding, _ := charset.Lookup(label); encoding != nil {
			if decoded, err := encoding.NewDecoder().Bytes(data); err == nil {
				return decoded
			}
		}
	}
	if utf8.Valid(data) {
		return data
	}
	decoded, err := charmap.Windows1250.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return decoded
}

// declaredCharset reads the charset a page's head declares, in either meta
// form, and stops at the body: a declaration further down is page text.
func declaredCharset(data []byte) string {
	tokens := html.NewTokenizer(bytes.NewReader(data))
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttributes := tokens.TagName()
			switch string(name) {
			case "body", "frameset":
				return ""
			case "meta":
				if !hasAttributes {
					continue
				}
				if label := metaCharset(tokens); label != "" {
					return label
				}
			}
		}
	}
}

// metaCharset is the charset one meta tag names: <meta charset=...> or
// <meta http-equiv="Content-Type" content="...; charset=...">.
func metaCharset(tokens *html.Tokenizer) string {
	var httpEquiv, content string
	for {
		key, value, more := tokens.TagAttr()
		switch strings.ToLower(string(key)) {
		case "charset":
			return strings.TrimSpace(string(value))
		case "http-equiv":
			httpEquiv = string(value)
		case "content":
			content = string(value)
		}
		if !more {
			break
		}
	}
	if !strings.EqualFold(strings.TrimSpace(httpEquiv), "content-type") {
		return ""
	}
	_, params, err := mime.ParseMediaType(content)
	if err != nil {
		return ""
	}
	return params["charset"]
}

// titleSuffix is what VULCAN appends to the school's name in index.html.
const titleSuffix = "Plan lekcji"

// schoolName reads the school's name from the export's index.html, whose
// title is "<school>. Plan lekcji" and ". Plan lekcji" when nobody typed one.
// Goptivum's own export titles it with the name alone.
func schoolName(index []byte) string {
	if index == nil {
		return ""
	}
	root, err := parseHTML(index)
	if err != nil {
		return ""
	}
	titles := findAll(root, func(n *html.Node) bool { return n.Data == "title" })
	if len(titles) == 0 {
		return ""
	}
	title := strings.TrimSpace(text(titles[0]))
	if title == titleSuffix {
		return ""
	}
	if school, ok := strings.CutSuffix(title, ". "+titleSuffix); ok {
		return strings.TrimSpace(school)
	}
	if school, ok := strings.CutSuffix(title, "."+titleSuffix); ok {
		return strings.TrimSpace(school)
	}
	return title
}
