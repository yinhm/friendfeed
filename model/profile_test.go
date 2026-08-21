package model

import (
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
