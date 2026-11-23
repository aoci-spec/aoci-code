package databasebootstrap

import "testing"

// A Root whose last line has no line break still rolls back: the descriptor
// becomes its new last line and rootPreimage finds it there (audit G1-04).
func TestRootPreimageRestoresARootWithoutFinalLineBreak(t *testing.T) {
	for _, root := range []string{
		"#AOCI-ROOT-MANIFEST: 1\n#Volume: id=code kind=code path=aoci.code.txt\n",
		"#AOCI-ROOT-MANIFEST: 1\n#Volume: id=code kind=code path=aoci.code.txt",
		"#AOCI-ROOT-MANIFEST: 1\r\n#Volume: id=code kind=code path=aoci.code.txt",
	} {
		post, err := addDatabaseDescriptor([]byte(root))
		if err != nil {
			t.Fatal(err)
		}
		got := rootPreimage(&Preview{RootPostimage: string(post)})
		if string(got) != root {
			t.Fatalf("rootPreimage(%q) = %q, want %q", post, got, root)
		}
	}
}
