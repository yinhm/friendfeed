package model

import (
	"errors"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/yinhm/friendfeed/pb"
)

func (s *TableTestSuite) TestOAuthUserUniqueKey() {
	key := oauthUserIDFrom("Twitter", "12345")
	assert.Equal(s.T(), "twitter:12345", string(key))
	key = oauthUserIDFrom("Google", "54321")
	assert.Equal(s.T(), "google:54321", string(key))
	key = oauthUserIDFrom("WeChat", "233")
	assert.Equal(s.T(), "wechat:233", string(key))
}

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

	key := oauthUserIDFrom(ptu.Provider, ptu.UserId)
	msg := new(pb.OAuthUser)
	err = OAuth.Get(s.db, key, msg)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "12345", msg.UserId)
}

func (s *TableTestSuite) TestGetOAuthUserNotFound() {
	_, _, err := GetOAuthUser(s.db, "twitter", "no-such-user")
	assert.True(s.T(), errors.Is(err, ErrNotFound))
}

// 重复登录：uuid 必须保持不变，token 被刷新；传入空 uuid 时继承已存值。
func (s *TableTestSuite) TestPutOAuthUserRelogin() {
	first := &pb.OAuthUser{
		UserId:      "oauth-relogin",
		Name:        "relogin",
		AccessToken: "token-1",
		Provider:    "twitter",
	}
	saved, err := PutOAuthUser(s.db, first)
	assert.Nil(s.T(), err)
	assert.NotEmpty(s.T(), saved.Uuid)

	// relogin with a fresh token and no uuid
	second := &pb.OAuthUser{
		UserId:      "oauth-relogin",
		Name:        "relogin",
		AccessToken: "token-2",
		Provider:    "twitter",
	}
	saved, err = PutOAuthUser(s.db, second)
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "token-2", saved.AccessToken)

	_, msg, err := GetOAuthUser(s.db, "twitter", "oauth-relogin")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), first.Uuid, msg.Uuid)
	assert.Equal(s.T(), "token-2", msg.AccessToken)
}

// 已存 uuid 与传入 uuid 冲突时必须拒绝，防止串号。
func (s *TableTestSuite) TestPutOAuthUserRejectsUuidMismatch() {
	first := &pb.OAuthUser{
		UserId:   "oauth-mismatch",
		Provider: "twitter",
	}
	saved, err := PutOAuthUser(s.db, first)
	assert.Nil(s.T(), err)

	conflict := &pb.OAuthUser{
		UserId:   "oauth-mismatch",
		Provider: "twitter",
		Uuid:     uuid.Must(uuid.NewV4()).String(),
	}
	_, err = PutOAuthUser(s.db, conflict)
	assert.NotNil(s.T(), err)
	assert.Equal(s.T(), "user mismatch", err.Error())

	// 显式传入相同 uuid 是合法的
	same := &pb.OAuthUser{
		UserId:   "oauth-mismatch",
		Provider: "twitter",
		Uuid:     saved.Uuid,
	}
	_, err = PutOAuthUser(s.db, same)
	assert.Nil(s.T(), err)
}

// 新用户 uuid 由 provider+userId 确定性生成；不同 provider 的同名 userId 互不覆盖。
func (s *TableTestSuite) TestPutOAuthUserProviderIsolation() {
	tw := &pb.OAuthUser{UserId: "oauth-iso", Provider: "twitter"}
	gg := &pb.OAuthUser{UserId: "oauth-iso", Provider: "google"}

	twSaved, err := PutOAuthUser(s.db, tw)
	assert.Nil(s.T(), err)
	ggSaved, err := PutOAuthUser(s.db, gg)
	assert.Nil(s.T(), err)

	assert.Equal(s.T(), UniqueKeyFrom("twitter", "oauth-iso").String(), twSaved.Uuid)
	assert.Equal(s.T(), UniqueKeyFrom("google", "oauth-iso").String(), ggSaved.Uuid)
	assert.NotEqual(s.T(), twSaved.Uuid, ggSaved.Uuid)

	_, twMsg, err := GetOAuthUser(s.db, "twitter", "oauth-iso")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "twitter", twMsg.Provider)
	_, ggMsg, err := GetOAuthUser(s.db, "google", "oauth-iso")
	assert.Nil(s.T(), err)
	assert.Equal(s.T(), "google", ggMsg.Provider)
}
