package model

import (
	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/pb"
)

func (s *TableTestSuite) TestOAuthUser() {
	// "Given OAuth User, should save
	ptu := &pb.OAuthUser{
		UserId:      "12345",
		Name:        "foobar",
		NickName:    "foo bar",
		Email:       "foo@bar.com",
		AccessToken: "f o o b a r",
		Provider:    "twitter",
	}

	_, err := PutOAuthUser(s.db, ptu)
	assert.Nil(s.T(), err)

	key := _oauthUserIdFrom(ptu.Provider, ptu.UserId)
	msg := new(pb.OAuthUser)
	err = OAuth.Get(s.db, key, msg)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "12345", msg.UserId)
}
