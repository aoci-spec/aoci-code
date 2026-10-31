package index

import "testing"

func TestReplaceLocaleMarkerChangesOnlyTheMarkerLine(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"lf": {
			"#AOCI-ROOT-MANIFEST: 1\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: p\n",
			"#AOCI-ROOT-MANIFEST: 1\n#Format-Version: cognition-volumes/v1\n#Locale: zh-CN\n#Project: p\n",
		},
		"crlf": {
			"#AOCI-ROOT-MANIFEST: 1\r\n#Locale: en-US\r\n#Project: p\r\n",
			"#AOCI-ROOT-MANIFEST: 1\r\n#Locale: zh-CN\r\n#Project: p\r\n",
		},
		"bom and padding": {
			"\ufeff#AOCI-ROOT-MANIFEST: 1\n  #Locale:   en-US  \n#Project: p",
			"\ufeff#AOCI-ROOT-MANIFEST: 1\n#Locale: zh-CN\n#Project: p",
		},
		"marker first line": {
			"#Locale: en-US\n#Header\n",
			"#Locale: zh-CN\n#Header\n",
		},
		"no marker gains one first": {
			"#AOCI-LOCALE-FREE\nf.go[XC5T]: F:x | R:- | A:- | S:-\n",
			"#Locale: zh-CN\n#AOCI-LOCALE-FREE\nf.go[XC5T]: F:x | R:- | A:- | S:-\n",
		},
		"no marker keeps crlf": {
			"#Header\r\nline\r\n",
			"#Locale: zh-CN\r\n#Header\r\nline\r\n",
		},
	}
	for name, tc := range cases {
		got, err := ReplaceLocaleMarker([]byte(tc.in), "zh-CN")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != tc.want {
			t.Fatalf("%s:\n got %q\nwant %q", name, got, tc.want)
		}
		locale, explicit, err := DetectLocale(string(got))
		if err != nil || !explicit || locale != "zh-CN" {
			t.Fatalf("%s: replaced document does not detect as zh-CN: %q %v %v", name, locale, explicit, err)
		}
	}
	if _, err := ReplaceLocaleMarker([]byte("#Locale: en-US\n"), "fr-FR"); err == nil {
		t.Fatal("an unsupported target Locale must be refused")
	}
	if _, err := ReplaceLocaleMarker([]byte("#Locale: en-US\n#Locale: zh-CN\n"), "zh-CN"); err == nil {
		t.Fatal("duplicate markers must be refused rather than half-replaced")
	}
}
