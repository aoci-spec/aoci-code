package cognitiontxn

import (
	"os"
	"path/filepath"
	"testing"
)

// Every receipt kind AOCI writes directly under .aoci/transactions is pending,
// and so is a file nobody recognises: the Overview guard, Verify, Guide,
// Maintain, the CLI gate, and every transaction start read this one list, so
// no consumer can call the repository aligned while another refuses it (#74).
func TestPendingRecognisesEveryReceiptKindAndForeignFiles(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, ".aoci", "transactions")
	for _, sub := range []string{"history", "scope-5"} {
		if err := os.MkdirAll(filepath.Join(directory, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"database-bootstrap-1.json", "bootstrap-2.json", "migration-3.json", "reversal-4.json",
		"scope-5.json", "remove-6.json", "entries-7.json", "header-8.json", "note.json", "history/entries-9.json", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(rel)), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := Pending(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, receipt := range pending {
		got[receipt.Filename] = receipt.Operation + "/" + receipt.ID
	}
	want := map[string]string{
		"database-bootstrap-1.json": "database-bootstrap/1", "bootstrap-2.json": "bootstrap/2",
		"migration-3.json": "migration/3", "reversal-4.json": "reversal/4", "scope-5.json": "scope/5",
		"remove-6.json": "remove/6", "entries-7.json": "entries/7", "header-8.json": "header/8",
		"note.json": OperationUnknown + "/note",
	}
	if len(got) != len(want) {
		t.Fatalf("pending=%v want=%v", got, want)
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Fatalf("%s read as %q, want %q", name, got[name], kind)
		}
	}
	for i := 1; i < len(pending); i++ {
		if pending[i-1].Filename > pending[i].Filename {
			t.Fatalf("pending receipts must be sorted: %v", pending)
		}
	}
	if _, ok := ParseReceiptName("remove-6.json.bak"); ok {
		t.Fatal("a non-json name is not a receipt")
	}
}
