package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yinhm/friendfeed/pb"
)

func TestAccountPageDataOmitsCredentialsAndInternalState(t *testing.T) {
	data := accountPageDataFromProto("profile", "user-uuid", &pb.Profile{
		Uuid: "user-uuid", Id: "alice", Name: "Alice", IsSuper: true,
	}, &pb.ListFeedServicesResponse{
		Services: []*pb.FeedService{{
			Id: "twitter", Name: "Twitter", Username: "alice", Enabled: true,
			Oauth:       &pb.OAuthUser{AccessToken: "secret-token", AccessTokenSecret: "secret-token-2"},
			AddedByUuid: "internal-actor", AuthorizedByUuid: "internal-authorizer",
		}},
		States: map[string]*pb.ServiceState{"service": {
			ServiceUuid: "service", Etag: "internal-etag", LastModified: "internal-last-modified",
			LastFetchMs: 10, LastError: "safe summary",
		}},
	})
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-token", "internal-actor", "internal-authorizer", "internal-etag", "internal-last-modified", "is_super"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("account DTO exposed %q: %s", forbidden, raw)
		}
	}
	if !strings.Contains(string(raw), `"username":"alice"`) || !strings.Contains(string(raw), `"last_error":"safe summary"`) {
		t.Fatalf("account DTO lost display fields: %s", raw)
	}
}
