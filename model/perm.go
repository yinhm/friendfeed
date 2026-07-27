package model

import (
	"errors"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
)

// feedFromProfile builds the canonical actor reference for a comment or
// like: the stable UUID as identity plus a display snapshot (id, name,
// picture, type) taken from the profile at write time. All write paths
// must use this instead of hand-rolling pb.Feed literals, so new records
// always carry From.Uuid.
//
// The profile must carry a valid identity: nil profile, empty or
// malformed Uuid, and the zero UUID are rejected — persisting those
// would create records nobody can ever be authorized on. Display fields
// (id, name, picture, type) may be empty.
func feedFromProfile(profile *pb.Profile) (*pb.Feed, error) {
	if profile == nil {
		return nil, errors.New("nil profile")
	}
	if profile.Uuid == "" {
		return nil, errors.New("profile has no uuid")
	}
	profileUUID, err := uuid.FromString(profile.Uuid)
	if err != nil {
		return nil, err
	}
	if profileUUID == uuid.Nil {
		return nil, errors.New("profile uuid is the zero uuid")
	}
	return &pb.Feed{
		Uuid:    profile.Uuid,
		Id:      profile.Id,
		Name:    profile.Name,
		Picture: profile.Picture,
		Type:    profile.Type,
	}, nil
}

// permOwnedBy reports whether ref belongs to profile, for authorization
// and ownership dedupe. UUID is the only identity:
//
//   - nil ref or nil profile: false;
//   - ref.Uuid empty (legacy record): false — never claimed via the id;
//   - ref.Uuid malformed: false — no fallback to the id, a recycled id
//     must not grant the current registrant rights over older records;
//   - profile.Uuid empty or malformed: false (fail safe, no panic);
//   - either side the zero UUID: false — it parses but is no identity;
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
	// The zero UUID parses successfully but is not a valid identity;
	// two zero UUIDs must never compare as the same user.
	if refUUID == uuid.Nil || profileUUID == uuid.Nil {
		return false
	}
	return refUUID == profileUUID
}
