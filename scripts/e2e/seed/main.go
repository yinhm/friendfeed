// Seed an ffdb backend with deterministic entries for the E2E smoke test.
// Entries are ingested through ForceArchiveFeed, the production archive path,
// so they also land in the public feed index.
package main

import (
	"context"
	"flag"
	"log"
	"net/http/httptest"
	"os"
	"time"

	"github.com/gofrs/uuid"
	"github.com/gorilla/sessions"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := flag.String("addr", "localhost:12119", "ffdb gRPC address")
	sessionKey := flag.String("session-key", "", "ffweb cookie signing key")
	sessionCookieFile := flag.String("session-cookie-file", "", "file to receive the signed ffweb session cookie")
	flag.Parse()

	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewApiClient(conn)
	ctx := context.Background()
	authUser := &pb.OAuthUser{
		UserId:   "e2e-user-id",
		Name:     "e2e-user",
		NickName: "E2E User",
		Provider: "google",
	}
	profile, err := client.PutOAuth(ctx, authUser)
	if err != nil {
		log.Fatalf("PutOAuth: %v", err)
	}
	if *sessionKey != "" && *sessionCookieFile != "" {
		if err := writeSessionCookie(*sessionCookieFile, *sessionKey, authUser.UserId, profile.Uuid); err != nil {
			log.Fatalf("write session cookie: %v", err)
		}
	}

	stream, err := client.ForceArchiveFeed(context.Background())
	if err != nil {
		log.Fatalf("ForceArchiveFeed: %v", err)
	}

	from := &pb.Feed{Id: "e2e-bot", Name: "E2E Bot", Type: "user"}
	owner := &pb.Feed{Id: profile.Id, Name: profile.Name, Type: profile.Type}
	rawBody := `[{"type":"p","children":[{"text":"E2E smoke "},{"text":"bold marker","bold":true}]}]`

	for _, entry := range []*pb.Entry{
		{
			Id:          uuid.Must(uuid.NewV4()).String(),
			ProfileUuid: uuid.Must(uuid.NewV4()).String(),
			From:        from,
			Body:        `<p>E2E smoke <strong>bold marker</strong></p>`,
			RawBody:     rawBody,
			Date:        time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			Commands:    []string{"comment"},
		},
		{
			Id:          uuid.Must(uuid.NewV4()).String(),
			ProfileUuid: uuid.Must(uuid.NewV4()).String(),
			From:        from,
			Body:        `<p>E2E second entry plain text</p>`,
			Date:        time.Now().UTC().Format(time.RFC3339),
			Commands:    []string{"comment"},
		},
		{
			Id:          uuid.Must(uuid.NewV4()).String(),
			ProfileUuid: profile.Uuid,
			FeedUuid:    profile.Uuid,
			From:        owner,
			Body:        `<p>E2E editable original</p>`,
			RawBody:     `[{"type":"p","children":[{"text":"E2E editable original"}]}]`,
			Date:        time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
			Commands:    []string{"comment", "edit", "delete"},
		},
	} {
		if err := stream.Send(entry); err != nil {
			log.Fatalf("send: %v", err)
		}
	}

	summary, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("close: %v", err)
	}
	log.Printf("seeded %d entries", summary.EntryCount)
}

func writeSessionCookie(path, key, userID, profileUUID string) error {
	store := sessions.NewCookieStore([]byte(key))
	request := httptest.NewRequest("GET", "/", nil)
	recorder := httptest.NewRecorder()
	session, err := store.New(request, "ffdbsess")
	if err != nil {
		return err
	}
	session.Values["user_id"] = userID
	session.Values["uuid"] = profileUUID
	if err := session.Save(request, recorder); err != nil {
		return err
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		return os.ErrInvalid
	}
	return os.WriteFile(path, []byte(cookies[0].Value), 0o600)
}
