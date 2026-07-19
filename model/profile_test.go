package model

import (
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

	// req := &pb.OAuthUser{
	// 	Uuid:              "",
	// 	Name:              "demo",
	// 	NickName:          "demouser",
	// 	UserId:            "6666666",
	// 	AccessToken:       "",
	// 	AccessTokenSecret: "",
	// 	Provider:          "Twitter",
	// }
	// user, err := store.PutOAuthUser(s.mdb, authinfo)
}
