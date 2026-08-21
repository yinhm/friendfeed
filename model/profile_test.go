package model

import (
	"fmt"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/pb"
)

func (s *TableTestSuite) TestProfile() {
	p := &pb.Profile{
		Uuid: "c6f8dca854f011ddb489003048343a40",
		Id:   "yinhm",
		Name: "yinhm",
		Type: "user",
	}
	err := UpdateProfile(s.db, p)
	assert.Nil(s.T(), err)

	profile, err := GetProfileFromUserId(s.db, "yinhm")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "c6f8dca854f011ddb489003048343a40", profile.Uuid)

	profileUUID := uuid.Must(uuid.FromString("c6f8dca854f011ddb489003048343a40"))
	profile, err = GetProfileFromUuid(s.db, profileUUID)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "yinhm", profile.Id)
}

func (s *TableTestSuite) TestUpdateProfileRejectsTakenId() {
	first := &pb.Profile{
		Uuid: uuid.Must(uuid.NewV4()).String(),
		Id:   "taken-id",
		Name: "First",
		Type: "user",
	}
	assert.Nil(s.T(), UpdateProfile(s.db, first))

	// A different profile must not hijack the ID silently.
	second := &pb.Profile{
		Uuid: uuid.Must(uuid.NewV4()).String(),
		Id:   "taken-id",
		Name: "Second",
		Type: "user",
	}
	err := UpdateProfile(s.db, second)
	assert.ErrorIs(s.T(), err, ErrProfileIdTaken)

	// Rewriting the same profile under its own ID stays legal.
	first.Name = "First Renamed"
	assert.Nil(s.T(), UpdateProfile(s.db, first))
}

func (s *TableTestSuite) TestUpdateProfileConcurrentSameId() {
	// Concurrent creates of the same ID must serialize: exactly one wins and
	// UserMap stays consistent with the Profile table (no orphan mapping).
	const racers = 8
	errs := make(chan error, racers)
	for i := 0; i < racers; i++ {
		go func(i int) {
			errs <- UpdateProfile(s.db, &pb.Profile{
				Uuid: uuid.Must(uuid.NewV4()).String(),
				Id:   "raced-id",
				Name: fmt.Sprintf("Racer %d", i),
				Type: "user",
			})
		}(i)
	}
	successes, taken := 0, 0
	for i := 0; i < racers; i++ {
		err := <-errs
		if err == nil {
			successes++
		} else {
			assert.ErrorIs(s.T(), err, ErrProfileIdTaken)
			taken++
		}
	}
	assert.Equal(s.T(), 1, successes)
	assert.Equal(s.T(), racers-1, taken)

	// The winning mapping must resolve to an existing profile.
	winner, err := GetProfileFromUserId(s.db, "raced-id")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "raced-id", winner.Id)
}

func (s *TableTestSuite) TestGenerateProfileId() {
	for i := 0; i < 100; i++ {
		id, err := generateProfileId()
		assert.Nil(s.T(), err)
		assert.True(s.T(), strings.HasPrefix(id, "ff-"), id)
		assert.Nil(s.T(), ValidateProfileId(id), id)
	}
}

func (s *TableTestSuite) TestNewProfileFromOAuthUserGeneratesId() {
	authinfo := &pb.OAuthUser{
		Uuid:     uuid.Must(uuid.NewV4()).String(),
		UserId:   "100017389812633262146",
		Name:     "Alexander Bykov",
		NickName: "Alexander Bykov",
		Provider: "google",
	}
	profile, err := NewProfileFromOAuthUser(s.db, authinfo)
	assert.Nil(s.T(), err)

	// The provider display name is not usable as a feed slug: the profile
	// gets a system-generated ID and keeps the display name in Name only.
	assert.True(s.T(), strings.HasPrefix(profile.Id, "ff-"), profile.Id)
	assert.Nil(s.T(), ValidateProfileId(profile.Id), profile.Id)
	assert.Equal(s.T(), "Alexander Bykov", profile.Name)

	// The generated ID resolves through UserMap.
	byId, err := GetProfileFromUserId(s.db, profile.Id)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), profile.Uuid, byId.Uuid)
}

func (s *TableTestSuite) TestGetOrCreateProfileIsIdempotent() {
	authinfo := &pb.OAuthUser{
		Uuid:     uuid.Must(uuid.NewV4()).String(),
		UserId:   "idem-user",
		NickName: "Idem User",
		Provider: "google",
	}
	first, created, err := GetOrCreateProfileFromOAuthUser(s.db, authinfo)
	assert.Nil(s.T(), err)
	assert.True(s.T(), created)

	second, created, err := GetOrCreateProfileFromOAuthUser(s.db, authinfo)
	assert.Nil(s.T(), err)
	assert.False(s.T(), created)
	assert.Equal(s.T(), first.Id, second.Id)
}

func (s *TableTestSuite) TestGetOrCreateProfileConcurrentSameUuid() {
	// Concurrent first logins of the same account must not mint two ID
	// aliases for one UUID: exactly one call creates, every call returns the
	// same profile ID.
	authinfo := &pb.OAuthUser{
		Uuid:     uuid.Must(uuid.NewV4()).String(),
		UserId:   "race-user",
		NickName: "Race User",
		Provider: "google",
	}
	const racers = 8
	type result struct {
		id      string
		created bool
		err     error
	}
	results := make(chan result, racers)
	for i := 0; i < racers; i++ {
		go func() {
			profile, created, err := GetOrCreateProfileFromOAuthUser(s.db, authinfo)
			r := result{created: created, err: err}
			if profile != nil {
				r.id = profile.Id
			}
			results <- r
		}()
	}
	createdCount := 0
	ids := map[string]int{}
	for i := 0; i < racers; i++ {
		r := <-results
		assert.Nil(s.T(), r.err)
		if r.created {
			createdCount++
		}
		ids[r.id]++
	}
	assert.Equal(s.T(), 1, createdCount)
	assert.Equal(s.T(), 1, len(ids), "all racers must observe the same profile ID")
}

func (s *TableTestSuite) TestUpdateProfileRejectsReservedId() {
	profile := &pb.Profile{
		Uuid: uuid.Must(uuid.NewV4()).String(),
		Id:   "rename-me",
		Name: "Rename Me",
		Type: "user",
	}
	assert.Nil(s.T(), UpdateProfile(s.db, profile))
	profileUUID := uuid.Must(uuid.FromString(profile.Uuid))
	assert.Nil(s.T(), RenameProfileId(s.db, profileUUID, "renamed-away"))

	// The old ID is reserved by the rename; a different profile cannot take
	// it, and the error must classify as retryable ID-unavailable.
	other := &pb.Profile{
		Uuid: uuid.Must(uuid.NewV4()).String(),
		Id:   "rename-me",
		Name: "Other",
		Type: "user",
	}
	assert.ErrorIs(s.T(), UpdateProfile(s.db, other), ErrProfileIdReserved)
}
