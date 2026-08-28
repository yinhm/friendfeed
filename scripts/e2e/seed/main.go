// Seed an ffdb backend with deterministic entries for the E2E smoke test.
// Entries are ingested through ForceArchiveFeed, the production archive path.
// The public timeline only admits entries whose target feed resolves to a
// non-private profile, so the bot author is seeded as a real profile and the
// entries carry its UUID instead of a throwaway one.
package main

import (
	"context"
	"flag"
	"fmt"
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
	renameSessionCookieFile := flag.String("rename-session-cookie-file", "", "file to receive the rename-test session cookie")
	flag.Parse()

	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewApiClient(conn)
	ctx := context.Background()

	// PutOAuth now mints random "ff-" IDs for new profiles, so pin the
	// fixture slugs through the production rename RPC; several specs address
	// these feeds by ID (e.g. /feed/e2e-user).
	renameTo := func(profile *pb.Profile, id string) *pb.Profile {
		renamed, err := client.PostFeedinfo(ctx, &pb.Feedinfo{
			Uuid: profile.Uuid,
			Id:   id,
			Name: profile.Name,
			Type: profile.Type,
		})
		if err != nil {
			log.Fatalf("rename %s to %s: %v", profile.Uuid, id, err)
		}
		return renamed
	}

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
	profile = renameTo(profile, "e2e-user")
	if *sessionKey != "" && *sessionCookieFile != "" {
		if err := writeSessionCookie(*sessionCookieFile, *sessionKey, authUser.UserId, profile.Uuid); err != nil {
			log.Fatalf("write session cookie: %v", err)
		}
	}
	renameAuthUser := &pb.OAuthUser{
		UserId:   "e2e-rename-user-id",
		Name:     "e2e-rename-user",
		NickName: "E2E Rename User",
		Provider: "google",
	}
	renameProfile, err := client.PutOAuth(ctx, renameAuthUser)
	if err != nil {
		log.Fatalf("PutOAuth rename user: %v", err)
	}
	// Do NOT pin this user's slug: rename.spec renames it exactly once, and
	// RenameProfileId enforces one active rename per profile. The spec reads
	// the generated "ff-" ID from the profile form instead.
	if *sessionKey != "" && *renameSessionCookieFile != "" {
		if err := writeSessionCookie(*renameSessionCookieFile, *sessionKey, renameAuthUser.UserId, renameProfile.Uuid); err != nil {
			log.Fatalf("write rename session cookie: %v", err)
		}
	}

	// The bot feed must resolve to a real, non-private profile or its
	// entries are excluded from the public timeline.
	botProfile, err := client.PutOAuth(ctx, &pb.OAuthUser{
		UserId:   "e2e-bot-id",
		Name:     "e2e-bot",
		NickName: "E2E Bot",
		Provider: "google",
	})
	if err != nil {
		log.Fatalf("PutOAuth bot user: %v", err)
	}

	stream, err := client.ForceArchiveFeed(context.Background())
	if err != nil {
		log.Fatalf("ForceArchiveFeed: %v", err)
	}

	from := &pb.Feed{Id: botProfile.Id, Name: botProfile.Name, Type: botProfile.Type}
	owner := &pb.Feed{Id: profile.Id, Name: profile.Name, Type: profile.Type}
	rawBody := `[{"type":"p","children":[{"text":"E2E smoke "},{"text":"bold marker","bold":true}]}]`

	// Pagination fixtures: 35 bot entries ingested BEFORE the smoke entries.
	// The public timeline is bump-ordered, so seeding them first keeps the
	// smoke entries on page 1 while the 39-entry total still spills the
	// oldest fillers onto page 2.
	var entries []*pb.Entry
	for i := 1; i <= 35; i++ {
		entries = append(entries, &pb.Entry{
			Id:          uuid.Must(uuid.NewV4()).String(),
			ProfileUuid: botProfile.Uuid,
			From:        from,
			Body:        fmt.Sprintf(`<p>E2E page filler %02d</p>`, i),
			Date:        time.Now().Add(-time.Hour - time.Duration(i)*10*time.Minute).UTC().Format(time.RFC3339),
			Commands:    []string{"comment"},
		})
	}

	editableEntry := &pb.Entry{
		Id:          uuid.Must(uuid.NewV4()).String(),
		ProfileUuid: profile.Uuid,
		FeedUuid:    profile.Uuid,
		From:        owner,
		Body:        `<p>E2E editable original</p>`,
		RawBody:     `[{"type":"p","children":[{"text":"E2E editable original"}]}]`,
		Date:        time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
		Commands:    []string{"comment", "edit", "delete"},
	}
	entries = append(entries, []*pb.Entry{
		{
			Id:          uuid.Must(uuid.NewV4()).String(),
			ProfileUuid: botProfile.Uuid,
			From:        from,
			Body:        `<p>E2E smoke <strong>bold marker</strong></p>`,
			RawBody:     rawBody,
			Date:        time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
			Commands:    []string{"comment"},
		},
		{
			Id:          uuid.Must(uuid.NewV4()).String(),
			ProfileUuid: botProfile.Uuid,
			From:        from,
			Body:        `<p>E2E second entry plain text</p>`,
			Date:        time.Now().UTC().Format(time.RFC3339),
			Commands:    []string{"comment"},
		},
		editableEntry,
		{
			Id:          uuid.Must(uuid.NewV4()).String(),
			ProfileUuid: profile.Uuid,
			FeedUuid:    profile.Uuid,
			From:        owner,
			Body:        `<p>E2E deletable original</p>`,
			RawBody:     `[{"type":"p","children":[{"text":"E2E deletable original"}]}]`,
			Date:        time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339),
			Commands:    []string{"comment", "edit", "delete"},
		},
	}...)

	for _, entry := range entries {
		if err := stream.Send(entry); err != nil {
			log.Fatalf("send: %v", err)
		}
	}

	summary, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("close: %v", err)
	}
	log.Printf("seeded %d entries", summary.EntryCount)

	// Give the authenticated fixture one deterministic notification so the
	// browser baseline covers the real notification page and mark-read path.
	if _, err := client.LikeEntry(ctx, &pb.LikeRequest{
		Entry: editableEntry.Id,
		User:  botProfile.Uuid,
		Like:  true,
	}); err != nil {
		log.Fatalf("seed notification like: %v", err)
	}
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
