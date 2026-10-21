package plist

import (
	"strings"
	"testing"
)

func TestMarshal(t *testing.T) {
	tests := []struct {
		value any
		name  string
		want  string
	}{
		{name: "string is escaped", value: `a & <b> "c"`, want: "<string>a &amp; &lt;b&gt; \"c\"</string>\n"},
		{name: "true", value: true, want: "<true/>\n"},
		{name: "false", value: false, want: "<false/>\n"},
		{name: "integer", value: 1, want: "<integer>1</integer>\n"},
		{name: "empty array", value: []any{}, want: "<array/>\n"},
		{name: "empty dict", value: map[string]any{}, want: "<dict/>\n"},
		{name: "string slice", value: []string{"x", "y"}, want: "<array>\n\t<string>x</string>\n\t<string>y</string>\n</array>\n"},
		{
			name:  "dict keys are sorted and nested values indented",
			value: map[string]any{"b": map[string]string{"k": "v"}, "a": []any{true}},
			want: "<dict>\n\t<key>a</key>\n\t<array>\n\t\t<true/>\n\t</array>\n" +
				"\t<key>b</key>\n\t<dict>\n\t\t<key>k</key>\n\t\t<string>v</string>\n\t</dict>\n</dict>\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}

			want := header + tt.want + "</plist>\n"
			if string(got) != want {
				t.Errorf("got\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestMarshalRejectsControlCharacters(t *testing.T) {
	_, err := Marshal("a\x00b")
	if err == nil || !strings.Contains(err.Error(), "control character U+0000") {
		t.Fatalf("got %v, want a control character error", err)
	}
}

func TestMarshalUnsupportedType(t *testing.T) {
	_, err := Marshal(map[string]any{"PayloadContent": []any{1.5}})
	if err == nil || !strings.Contains(err.Error(), "PayloadContent: plist: unsupported value type float64") {
		t.Fatalf("got %v, want an unsupported type error naming the key", err)
	}
}
