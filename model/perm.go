package model

import (
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
)

// feedFromProfile builds the canonical actor reference for a comment or
// like: the stable UUID as identity plus a display snapshot (id, name,
// picture, type) taken from the profile at write time. All write paths
// must use this instead of hand-rolling pb.Feed literals, so new records
// always carry From.Uuid. Returns nil for a nil profile.
func feedFromProfile(profile *pb.Profile) *pb.Feed {
	if profile == nil {
		return nil
	}
	return &pb.Feed{
		Uuid:    profile.Uuid,
		Id:      profile.Id,
		Name:    profile.Name,
		Picture: profile.Picture,
		Type:    profile.Type,
	}
}

// permOwnedBy reports whether ref belongs to profile, for authorization
// and ownership dedupe. UUID is the only identity:
//
//   - nil ref or nil profile: false;
//   - ref.Uuid empty (legacy record): false — never claimed via the id;
//   - ref.Uuid malformed: false — no fallback to the id, a recycled id
//     must not grant the current registrant rights over older records;
//   - profile.Uuid empty or malformed: false (fail safe, no panic);
//   - both parse: compare by UUID value, not text form.
func permOwnedBy(ref *pb.Feed, profile *pb.Profile) bool {
	if ref == nil || profile == nil {
		return false
	}
	if ref.Uuid == "" || profile.Uuid == "" {
		return false
	}
	refUUID, err := uuid.FromString(ref.Uuid)
	if err != nil {
		return false
	}
	profileUUID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return false
	}
	return refUUID == profileUUID
}
