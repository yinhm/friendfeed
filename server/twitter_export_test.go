package server

import (
	"context"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func (s *RpcTestSuite) TestExportTwitterUsersTSVIncludesFixedBoundary() {
	feedUUID := uuid.Must(uuid.NewV4())
	profile := &pb.Profile{Uuid: feedUUID.String(), Id: "alice", Name: "Alice", Type: "user"}
	s.Require().NoError(model.UpdateProfile(s.srv.mdb, profile))
	_, err := model.PutOAuthUser(s.srv.mdb, &pb.OAuthUser{
		Uuid: feedUUID.String(), Provider: "twitter", UserId: "42", Name: "alice_x",
	})
	s.Require().NoError(err)
	published := time.Date(2020, 8, 17, 12, 34, 56, 0, time.UTC)
	_, err = model.PutArchiveEntry(s.srv.rdb, &pb.Entry{
		Id: uuid.Must(uuid.NewV4()).String(), Date: published.Format(time.RFC3339),
		ProfileUuid: feedUUID.String(), Url: "https://twitter.com/alice/statuses/1295071681511407617",
	})
	s.Require().NoError(err)

	response, err := s.srv.Command(context.Background(), &pb.CommandRequest{Command: "ExportTwitterUsersTSV"})
	s.Require().NoError(err)
	s.Contains(response.Result, "feed_id\tfeed_uuid\ttwitter_username\ttwitter_user_id\tboundary_tweet_id\tboundary_at")
	s.Contains(response.Result, strings.Join([]string{"alice", feedUUID.String(), "alice_x", "42", "1295071681511407617", published.Format(time.RFC3339)}, "\t"))
}
