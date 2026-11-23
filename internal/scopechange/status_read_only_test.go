package scopechange

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Inspect, the scope status of a transaction, writes nothing even beside a
// cut CAS exchange; Resume settles it before it reads the images (rv34, audit
// G1-02).
func TestScopeStatusReadsACutExchangeAndResumeSettlesIt(t *testing.T) {
	root, candidates := buildChangeFixture(t)
	preview, approval := buildApprovedPreview(t, root, candidates)
	indexPath := filepath.Join(root, "aoci.txt")
	pre, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	original := transactionFault
	t.Cleanup(func() { transactionFault = original })
	transactionFault = func(point string) error {
		if point == "after_publish_aoci_txt" {
			return errors.New("injected")
		}
		return nil
	}
	if _, err := Apply(root, preview, approval); err == nil {
		t.Fatal("fault did not interrupt Apply")
	}
	transactionFault = func(string) error { return nil }
	post, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
	intent, _ := json.Marshal(map[string]string{"expected_sha256": digest(pre), "new_sha256": digest(post)})
	if err := os.WriteFile(indexPath+".aoci-cas.swap", pre, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath+".aoci-cas.intent", intent, 0o644); err != nil {
		t.Fatal(err)
	}

	transactionID := preview.EnvelopeDigest[:24]
	status, err := Inspect(root, transactionID)
	if err != nil || status.ThirdPartyConflict {
		t.Fatalf("status of a cut exchange: %+v err=%v", status, err)
	}
	for path, want := range map[string][]byte{indexPath: post, indexPath + ".aoci-cas.swap": pre, indexPath + ".aoci-cas.intent": intent} {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("status changed %s: %v", filepath.Base(path), err)
		}
	}
	if result, err := Resume(root, transactionID); err != nil || result.Status != "applied" {
		t.Fatalf("resume: %+v err=%v", result, err)
	}
	for _, suffix := range []string{".aoci-cas.swap", ".aoci-cas.intent"} {
		if _, err := os.Lstat(indexPath + suffix); !os.IsNotExist(err) {
			t.Fatalf("resume left %s: %v", suffix, err)
		}
	}
}
