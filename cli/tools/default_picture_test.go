package main

import (
	"testing"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func TestFixDefaultPictures(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	seed := map[string]string{
		"11111111-1111-1111-1111-111111111111": "http://friendfeed.com/static/images/group-large.png?v=1216",
		"22222222-2222-2222-2222-222222222222": "https://friendfeed.com/static/images/group-large.png?v=860",
		"33333333-3333-3333-3333-333333333333": "https://example.com/custom.png",
		"44444444-4444-4444-4444-444444444444": "",
		"55555555-5555-5555-5555-555555555555": defaultPictureReplacement,
	}
	for id, picture := range seed {
		raw, err := proto.Marshal(&pb.Profile{Uuid: id, Id: "u-" + id[:4], Type: "user", Picture: picture})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set(profileFixtureKey(t, id), raw); err != nil {
			t.Fatal(err)
		}
	}

	dry, err := fixDefaultPictures(db, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if dry.profiles != 5 || dry.fixed != 2 {
		t.Fatalf("dry-run stats = %+v; want profiles=5 fixed=2", dry)
	}
	if got := profileFixturePicture(t, db, "11111111-1111-1111-1111-111111111111"); got != seed["11111111-1111-1111-1111-111111111111"] {
		t.Fatalf("dry-run wrote picture: %q", got)
	}

	stats, err := fixDefaultPictures(db, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if stats.profiles != 5 || stats.fixed != 2 {
		t.Fatalf("apply stats = %+v; want profiles=5 fixed=2", stats)
	}
	for _, id := range []string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"} {
		if got := profileFixturePicture(t, db, id); got != defaultPictureReplacement {
			t.Fatalf("profile %s picture = %q; want %q", id, got, defaultPictureReplacement)
		}
	}
	for _, id := range []string{"33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444", "55555555-5555-5555-5555-555555555555"} {
		if got := profileFixturePicture(t, db, id); got != seed[id] {
			t.Fatalf("profile %s picture changed to %q; want untouched %q", id, got, seed[id])
		}
	}

	scoped, err := fixDefaultPictures(db, "no-such-user", false)
	if err != nil {
		t.Fatal(err)
	}
	if scoped.profiles != 0 || scoped.fixed != 0 {
		t.Fatalf("scoped stats = %+v; want zero", scoped)
	}
}

func profileFixtureKey(t *testing.T, id string) store.Key {
	t.Helper()
	parsed, err := uuid.FromString(id)
	if err != nil {
		t.Fatal(err)
	}
	return model.Profile.PrefixAppend(parsed.Bytes())
}

func profileFixturePicture(t *testing.T, db *store.Store, id string) string {
	t.Helper()
	raw, err := db.Get(profileFixtureKey(t, id))
	if err != nil {
		t.Fatal(err)
	}
	profile := new(pb.Profile)
	if err := proto.Unmarshal(raw, profile); err != nil {
		t.Fatal(err)
	}
	return profile.Picture
}
