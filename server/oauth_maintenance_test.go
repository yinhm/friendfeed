package server

import (
	"context"
	"encoding/json"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
)

func (s *RpcTestSuite) TestOAuthMaintenanceUnlinksOnlySelectedIdentity() {
	profileID := uuid.Must(uuid.NewV4())
	s.Require().NoError(model.UpdateProfile(s.srv.mdb, &pb.Profile{Uuid: profileID.String(), Id: "alice", Type: "user"}))
	for _, identity := range []*pb.OAuthUser{
		{Provider: "google", UserId: "google-1", Uuid: profileID.String(), Name: "Alice"},
		{Provider: "twitter", UserId: "42", Uuid: profileID.String(), Name: "alice_x"},
	} {
		_, err := model.PutOAuthUser(s.srv.mdb, identity)
		s.Require().NoError(err)
	}

	response, err := s.srv.Command(context.Background(), &pb.CommandRequest{Command: "OAuthInspect", Arg1: "twitter", Arg2: "42"})
	s.Require().NoError(err)
	var report oauthIdentityReport
	s.Require().NoError(json.Unmarshal([]byte(response.Result), &report))
	s.Equal(2, report.ProfileIdentityCount)

	response, err = s.srv.Command(context.Background(), &pb.CommandRequest{Command: "OAuthUnlink", Arg1: "twitter", Arg2: "42"})
	s.Require().NoError(err)
	s.Require().NoError(json.Unmarshal([]byte(response.Result), &report))
	s.Equal(1, report.ProfileIdentityCount)
	_, _, err = model.GetOAuthUser(s.srv.mdb, "twitter", "42")
	s.ErrorIs(err, model.ErrNotFound)
	_, remaining, err := model.GetOAuthUser(s.srv.mdb, "google", "google-1")
	s.Require().NoError(err)
	s.Equal(profileID.String(), remaining.Uuid)

	_, err = s.srv.Command(context.Background(), &pb.CommandRequest{Command: "OAuthUnlink", Arg1: "google", Arg2: "google-1"})
	s.ErrorContains(err, "last OAuth identity")
}
