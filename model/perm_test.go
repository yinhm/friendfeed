package model

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
)

func TestFeedFromProfile(t *testing.T) {
	profileUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{
		Uuid:    profileUUID.String(),
		Id:      "yinhm",
		Name:    "Heming",
		Picture: "http://example.com/p.jpg",
		Type:    "user",
	}

	ref := feedFromProfile(profile)
	if ref == nil {
		t.Fatal("ref is nil")
	}
	if ref.Uuid != profile.Uuid {
		t.Errorf("Uuid = %q; want %q", ref.Uuid, profile.Uuid)
	}
	if ref.Id != "yinhm" || ref.Name != "Heming" ||
		ref.Picture != "http://example.com/p.jpg" || ref.Type != "user" {
		t.Errorf("snapshot = <%q, %q, %q, %q>; want copied from profile",
			ref.Id, ref.Name, ref.Picture, ref.Type)
	}
}

func TestFeedFromProfileNil(t *testing.T) {
	if ref := feedFromProfile(nil); ref != nil {
		t.Errorf("feedFromProfile(nil) = %v; want nil", ref)
	}
}

func TestFeedFromProfileSparse(t *testing.T) {
	profileUUID := uuid.Must(uuid.NewV4())
	ref := feedFromProfile(&pb.Profile{Uuid: profileUUID.String(), Id: "yinhm"})
	if ref.Uuid != profileUUID.String() || ref.Id != "yinhm" {
		t.Errorf("ref = <%q, %q>; want uuid and id set", ref.Uuid, ref.Id)
	}
	if ref.Name != "" || ref.Picture != "" || ref.Type != "" {
		t.Errorf("empty fields = <%q, %q, %q>; want empty strings", ref.Name, ref.Picture, ref.Type)
	}
}

func TestPermOwnedBy(t *testing.T) {
	ownerUUID := uuid.Must(uuid.NewV4())
	otherUUID := uuid.Must(uuid.NewV4())
	owner := &pb.Profile{Uuid: ownerUUID.String(), Id: "owner"}

	cases := []struct {
		name    string
		ref     *pb.Feed
		profile *pb.Profile
		want    bool
	}{
		{
			name:    "uuid match",
			ref:     &pb.Feed{Uuid: ownerUUID.String(), Id: "owner"},
			profile: owner,
			want:    true,
		},
		{
			name:    "uuid match across text forms",
			ref:     &pb.Feed{Uuid: ownerUUID.String()},
			profile: &pb.Profile{Uuid: "{" + ownerUUID.String() + "}", Id: "owner"},
			want:    true,
		},
		{
			name:    "different uuid with same id",
			ref:     &pb.Feed{Uuid: otherUUID.String(), Id: "owner"},
			profile: owner,
			want:    false,
		},
		{
			name:    "empty ref uuid never falls back to id",
			ref:     &pb.Feed{Id: "owner"},
			profile: owner,
			want:    false,
		},
		{
			name:    "malformed ref uuid never falls back to id",
			ref:     &pb.Feed{Uuid: "not-a-uuid", Id: "owner"},
			profile: owner,
			want:    false,
		},
		{
			name:    "nil ref",
			ref:     nil,
			profile: owner,
			want:    false,
		},
		{
			name:    "nil profile",
			ref:     &pb.Feed{Uuid: ownerUUID.String()},
			profile: nil,
			want:    false,
		},
		{
			name:    "empty profile uuid fails safe",
			ref:     &pb.Feed{Uuid: ownerUUID.String(), Id: "owner"},
			profile: &pb.Profile{Id: "owner"},
			want:    false,
		},
		{
			name:    "malformed profile uuid fails safe",
			ref:     &pb.Feed{Uuid: ownerUUID.String(), Id: "owner"},
			profile: &pb.Profile{Uuid: "not-a-uuid", Id: "owner"},
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := permOwnedBy(tc.ref, tc.profile); got != tc.want {
				t.Errorf("permOwnedBy() = %t; want %t", got, tc.want)
			}
		})
	}
}
