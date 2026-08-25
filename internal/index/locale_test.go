package index

import (
	"bytes"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

func TestReplaceLocaleMarkerPreservesEveryOtherByte(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{
			name: "explicit_crlf",
			in:   []byte("#AOCI-ROOT-MANIFEST v1\r\n#Locale: en-US\r\n# custom 中文\r\n"),
			want: []byte("#AOCI-ROOT-MANIFEST v1\r\n#Locale: zh-CN\r\n# custom 中文\r\n"),
		},
		{
			name: "legacy_implicit_with_bom",
			in:   append([]byte{0xef, 0xbb, 0xbf}, []byte("custom header\n===code /repo/===\n")...),
			want: append([]byte{0xef, 0xbb, 0xbf}, []byte("#Locale: zh-CN\ncustom header\n===code /repo/===\n")...),
		},
		{
			name: "explicit_with_bom",
			in:   append([]byte{0xef, 0xbb, 0xbf}, []byte("#Locale: en-US\ncustom header\n")...),
			want: append([]byte{0xef, 0xbb, 0xbf}, []byte("#Locale: zh-CN\ncustom header\n")...),
		},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			got, err := ReplaceLocaleMarker(current.in, textassets.LegacyLocale)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, current.want) {
				t.Fatalf("marker rewrite changed unrelated bytes:\n got %q\nwant %q", got, current.want)
			}
		})
	}
}

func TestReplaceLocaleMarkerRejectsDuplicateMarker(t *testing.T) {
	if _, err := ReplaceLocaleMarker([]byte("#Locale: en-US\n#Locale: en-US\n"), textassets.LegacyLocale); err == nil {
		t.Fatal("duplicate Locale markers were accepted")
	}
}
