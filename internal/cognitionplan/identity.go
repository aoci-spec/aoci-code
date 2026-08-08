package cognitionplan

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash"
)

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type identityEncoder struct{ hash hash.Hash }

func newIdentity(kind string) *identityEncoder {
	encoder := &identityEncoder{hash: sha256.New()}
	encoder.field("identity_protocol", "aoci-length-framed-v1")
	encoder.field("identity_kind", kind)
	return encoder
}

func (encoder *identityEncoder) field(kind, value string) {
	writeFramed(encoder.hash, []byte(kind))
	writeFramed(encoder.hash, []byte(value))
}

func (encoder *identityEncoder) sum() string { return hex.EncodeToString(encoder.hash.Sum(nil)) }

func writeFramed(target hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = target.Write(size[:])
	_, _ = target.Write(value)
}

func canonicalJSON(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
