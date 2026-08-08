package cognition

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
)

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

// identityEncoder uses typed, length-delimited fields. This prevents the
// ambiguity of raw concatenation ("ab"+"c" versus "a"+"bc").
type identityEncoder struct{ hash hash.Hash }

func newIdentityEncoder(kind string) *identityEncoder {
	e := &identityEncoder{hash: sha256.New()}
	e.field("identity_protocol", "aoci-length-framed-v1")
	e.field("identity_kind", kind)
	return e
}

func (e *identityEncoder) field(fieldType, value string) {
	writeFramed(e.hash, []byte(fieldType))
	writeFramed(e.hash, []byte(value))
}

func (e *identityEncoder) sum() string { return hex.EncodeToString(e.hash.Sum(nil)) }

func writeFramed(target hash.Hash, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = target.Write(size[:])
	_, _ = target.Write(value)
}

func (set *Set) computeIdentities() {
	composite := newIdentityEncoder("composite")
	composite.field("layout_mode", set.LayoutMode)
	composite.field("layout_version", set.LayoutVersion)
	encodeAssetIdentity(composite, "root", &set.Root)
	if set.LayoutMode == LayoutVolumesV1 {
		encodeAssetIdentity(composite, "meta", &set.Meta)
		for _, id := range []string{"code", "database"} {
			encodeAssetIdentity(composite, id, set.assetOrAbsent(id))
		}
	}
	set.CompositeIdentity = composite.sum()
}

func encodeAssetIdentity(e *identityEncoder, slot string, asset *Asset) {
	e.field("slot", slot)
	e.field("volume_id", asset.Descriptor.ID)
	e.field("volume_kind", asset.Descriptor.Kind)
	e.field("volume_path", asset.Descriptor.Path)
	e.field("format_version", asset.Descriptor.FormatVersion)
	e.field("asset_state", asset.State)
	e.field("content_sha256", asset.SHA256)
}

func (set *Set) scopeIdentity(scope string, assets []*Asset) string {
	e := newIdentityEncoder("scope")
	e.field("layout_mode", set.LayoutMode)
	e.field("layout_version", set.LayoutVersion)
	e.field("requested_scope", scope)
	for _, asset := range assets {
		encodeAssetIdentity(e, asset.Descriptor.ID, asset)
	}
	if set.LayoutMode == LayoutVolumesV1 && (scope == ScopeCode || scope == ScopeDatabase) && set.Volumes[scope] == nil {
		encodeAssetIdentity(e, scope, set.assetOrAbsent(scope))
	}
	return e.sum()
}

func (set *Set) assetOrAbsent(id string) *Asset {
	if asset := set.Volumes[id]; asset != nil {
		return asset
	}
	kind := id
	path := "aoci." + id + ".txt"
	format := "object-fras-v2"
	if id == "database" {
		format = "table-fras-v2"
	}
	return &Asset{Descriptor: Descriptor{ID: id, Kind: kind, Path: path, FormatVersion: format}, State: AssetAbsent}
}
