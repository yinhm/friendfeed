package main

import (
	"context"
	"fmt"
	"log"

	"github.com/cockroachdb/pebble/v2"
	"github.com/gofrs/uuid"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/server"
	"github.com/yinhm/friendfeed/store"
	taskqueue "github.com/yinhm/friendfeed/task"
	"google.golang.org/protobuf/proto"
)

func runInspectServiceCommand(db *store.Store) {
	serviceID, err := uuid.FromString(inspectID)
	if err != nil || serviceID == uuid.Nil {
		log.Fatal("-id must be a Service UUID")
	}
	service, err := model.GetService(db, serviceID)
	if err != nil {
		log.Fatal(err)
	}
	state, stateErr := model.GetServiceState(db, serviceID)
	bindings, err := model.ListServiceFeedBindings(db, serviceID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("uuid=%s kind=%s canonical_url=%q fetch_url=%q title=%q site_url=%q bindings=%d\n",
		service.Uuid, service.Kind, service.CanonicalUrl, model.ServiceFetchURL(service), service.Title, service.SiteUrl, len(bindings))
	if stateErr == nil {
		fmt.Printf("status=%s last_fetch_ms=%d last_success_ms=%d next_fetch_ms=%d failures=%d permanent_failures=%d permanent_failure_since_ms=%d delivery_failures=%d http_status=%d last_error=%q\n",
			state.Status, state.LastFetchMs, state.LastSuccessMs, state.NextFetchMs, state.ConsecutiveFailures, state.PermanentFailures, state.PermanentFailureSinceMs, state.DeliveryFailures, state.HttpStatus, state.LastError)
	}
	for _, binding := range bindings {
		feedService, err := model.GetFeedService(db, binding.TargetFeedUUID, binding.ServiceID)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("target=%s service_id=%s enabled=%t name=%q\n",
			binding.TargetFeedUUID, binding.ServiceID, feedService.Enabled, feedService.Name)
	}
}

func serviceBindingArgs(db *store.Store) (uuid.UUID, *pb.FeedService) {
	target, err := uuid.FromString(timelineUser)
	if err != nil || target == uuid.Nil || inspectID == "" {
		log.Fatal("-user target Feed UUID and -id FeedService ID are required")
	}
	binding, err := model.GetFeedService(db, target, inspectID)
	if err != nil {
		log.Fatal(err)
	}
	return target, binding
}

func runDisableFeedServiceCommand(db *store.Store) {
	target, binding := serviceBindingArgs(db)
	if !binding.Enabled {
		fmt.Printf("target=%s service_id=%s already_disabled=true\n", target, binding.Id)
		return
	}
	var updated *pb.FeedService
	err := db.ApplyBatch(func(batch *pebble.Batch) error {
		var stageErr error
		updated, stageErr = model.StageSetFeedServiceEnabled(db, batch, target, binding.Id, false)
		return stageErr
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("target=%s service_id=%s enabled=%t\n", target, updated.Id, updated.Enabled)
}

func runRefetchFeedServiceCommand(db *store.Store) {
	target, binding := serviceBindingArgs(db)
	if !binding.Enabled {
		log.Fatal("FeedService is disabled")
	}
	payload, err := proto.Marshal(&pb.FeedServiceSeedPayload{
		ServiceUuid: binding.ServiceUuid, TargetFeedUuid: target.String(), ServiceId: binding.Id,
	})
	if err != nil {
		log.Fatal(err)
	}
	registry, err := server.NewTaskRegistry(nil)
	if err != nil {
		log.Fatal(err)
	}
	queue, err := taskqueue.NewQueue(db, registry, taskqueue.Options{})
	if err != nil {
		log.Fatal(err)
	}
	result, err := queue.Enqueue(context.Background(), taskqueue.Spec{
		Type: "feed_service.seed", Payload: payload, PayloadVersion: 1,
		IdempotencyKey: target.String() + ":" + binding.Id,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("target=%s service_id=%s task=%s already_exists=%t\n",
		target, binding.Id, result.Task.Id, result.AlreadyExists)
}
