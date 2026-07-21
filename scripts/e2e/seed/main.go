// Seed an ffdb backend with deterministic entries for the E2E smoke test.
// Entries are ingested through ForceArchiveFeed, the production archive path,
// so they also land in the public feed index.
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := flag.String("addr", "localhost:12119", "ffdb gRPC address")
	flag.Parse()

	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewApiClient(conn)
	stream, err := client.ForceArchiveFeed(context.Background())
	if err != nil {
		log.Fatalf("ForceArchiveFeed: %v", err)
	}

	from := &pb.Feed{Id: "e2e-bot", Name: "E2E Bot", Type: "user"}
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
