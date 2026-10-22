// Package plist encodes the subset of XML property lists that macOS
// configuration profiles need.
package plist

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

const header = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
`

// textEscaper escapes only what XML element text requires, so JSON-valued
// strings stay readable in the generated profiles.
var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// Marshal returns v as an XML property list. Supported value types are
// string, bool, int, []string, []any, map[string]string and map[string]any.
// Dictionary keys are sorted, so the same input always produces the same bytes.
func Marshal(v any) ([]byte, error) {
	var b bytes.Buffer

	b.WriteString(header)

	if err := encode(&b, v, 0); err != nil {
		return nil, err
	}

	b.WriteString("</plist>\n")

	return b.Bytes(), nil
}

func encode(b *bytes.Buffer, v any, depth int) error {
	indent := strings.Repeat("\t", depth)

	switch v := v.(type) {
	case string:
		return element(b, indent, "string", v)
	case bool:
		b.WriteString(indent)

		if v {
			b.WriteString("<true/>\n")
		} else {
			b.WriteString("<false/>\n")
		}
	case int:
		fmt.Fprintf(b, "%s<integer>%d</integer>\n", indent, v)
	case []string:
		items := make([]any, len(v))
		for i, s := range v {
			items[i] = s
		}

		return encode(b, items, depth)
	case []any:
		return encodeArray(b, v, depth)
	case map[string]string:
		dict := make(map[string]any, len(v))
		for k, s := range v {
			dict[k] = s
		}

		return encode(b, dict, depth)
	case map[string]any:
		return encodeDict(b, v, depth)
	default:
		return fmt.Errorf("plist: unsupported value type %T", v)
	}

	return nil
}

func encodeArray(b *bytes.Buffer, items []any, depth int) error {
	indent := strings.Repeat("\t", depth)
	if len(items) == 0 {
		b.WriteString(indent + "<array/>\n")

		return nil
	}

	b.WriteString(indent + "<array>\n")

	for _, item := range items {
		if err := encode(b, item, depth+1); err != nil {
			return err
		}
	}

	b.WriteString(indent + "</array>\n")

	return nil
}

func encodeDict(b *bytes.Buffer, dict map[string]any, depth int) error {
	indent := strings.Repeat("\t", depth)
	if len(dict) == 0 {
		b.WriteString(indent + "<dict/>\n")

		return nil
	}

	keys := make([]string, 0, len(dict))
	for k := range dict {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	b.WriteString(indent + "<dict>\n")

	for _, k := range keys {
		if err := element(b, indent+"\t", "key", k); err != nil {
			return err
		}

		if err := encode(b, dict[k], depth+1); err != nil {
			return fmt.Errorf("%s: %s", k, err.Error())
		}
	}

	b.WriteString(indent + "</dict>\n")

	return nil
}

func element(b *bytes.Buffer, indent, name, text string) error {
	for _, r := range text {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return fmt.Errorf("plist: %s %q contains control character %U, which XML cannot represent", name, text, r)
		}
	}

	b.WriteString(indent + "<" + name + ">" + textEscaper.Replace(text) + "</" + name + ">\n")

	return nil
}
