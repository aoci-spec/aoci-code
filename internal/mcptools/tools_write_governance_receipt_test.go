package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecoveryAndGovernanceReceiptsRejectNonCanonicalJSON(t *testing.T) {
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)
	transactionID := strings.Repeat("c", 64)
	recovery := []byte(`{"version":1,"batch_key":"` + transactionID +
		`","pre_index_sha256":"` + hashA + `","post_index_sha256":"` + hashB + `"}`)
	receipt := entriesGovernanceReceipt{
		Version: entriesGovernanceReceiptVersion, Kind: "entries",
		TransactionID: transactionID, PreIndexSHA256: hashA, PostIndexSHA256: hashB,
		Paths: []string{"a.go"}, CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	receipt.ReceiptID = governanceReceiptID(receipt)
	receiptData, err := marshalGovernanceReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		data   []byte
		decode func([]byte) error
	}{
		{
			name: "recovery_duplicate", data: []byte(strings.Replace(
				string(recovery), `"version":1`, `"version":1,"version":1`, 1,
			)),
			decode: func(data []byte) error {
				_, err := decodeAtomicBatchRecovery(data, transactionID)
				return err
			},
		},
		{
			name: "recovery_unknown", data: []byte(strings.Replace(
				string(recovery), `"version":1`, `"version":1,"unknown":true`, 1,
			)),
			decode: func(data []byte) error {
				_, err := decodeAtomicBatchRecovery(data, transactionID)
				return err
			},
		},
		{
			name: "recovery_trailing", data: append(append([]byte{}, recovery...), []byte(` {}`)...),
			decode: func(data []byte) error {
				_, err := decodeAtomicBatchRecovery(data, transactionID)
				return err
			},
		},
		{
			name: "governance_duplicate", data: []byte(strings.Replace(
				string(receiptData), `"version": 1`, `"version": 1,"version": 1`, 1,
			)),
			decode: func(data []byte) error {
				_, err := decodeGovernanceReceipt(data)
				return err
			},
		},
		{
			name: "governance_unknown", data: []byte(strings.Replace(
				string(receiptData), `"version": 1`, `"version": 1,"unknown": true`, 1,
			)),
			decode: func(data []byte) error {
				_, err := decodeGovernanceReceipt(data)
				return err
			},
		},
		{
			name: "governance_trailing", data: append(append([]byte{}, receiptData...), []byte(`{}`)...),
			decode: func(data []byte) error {
				_, err := decodeGovernanceReceipt(data)
				return err
			},
		},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			if err := current.decode(current.data); err == nil {
				t.Fatal("non-canonical proof JSON must fail closed")
			}
		})
	}
}

func TestPendingGovernanceAssetCheckUsesConfiguredIndexPath(t *testing.T) {
	root := t.TempDir()
	indexPath := filepath.Join(root, ".aoci", "custom-index.txt")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath+".aoci-cas.intent", []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rejectOtherPendingGovernanceAssets(
		root, indexPath, strings.Repeat("a", 64),
	); err == nil {
		t.Fatal("a configured-index CAS recovery asset must block supersession")
	}
}
