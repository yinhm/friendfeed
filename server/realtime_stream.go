package server

import (
	"strings"

	"github.com/yinhm/friendfeed/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func realtimeEventFromHint(hint realtimeHint) *pb.RealtimeEvent {
	return &pb.RealtimeEvent{
		ViewerUuid:   hint.Viewer.String(),
		Type:         hint.Type,
		ObjectUuid:   hint.Object.String(),
		ActivityAtMs: hint.At.UnixMilli(),
	}
}

func (s *ApiServer) SubscribeRealtimeEvents(req *pb.SubscribeRealtimeEventsRequest, stream pb.Realtime_SubscribeRealtimeEventsServer) error {
	if req == nil || strings.TrimSpace(req.GetSubscriberId()) == "" {
		return status.Error(codes.InvalidArgument, "subscriber_id is required")
	}
	if !s.beginBackgroundJob() {
		return status.Error(codes.Unavailable, "server is shutting down")
	}
	defer s.wg.Done()

	bus := s.realtimeBus()
	sub, err := bus.subscribe(req.GetSubscriberId())
	if err != nil {
		if err == errRealtimeStopped {
			return status.Error(codes.Unavailable, "realtime stream is shutting down")
		}
		return status.Error(codes.Internal, "subscribe realtime stream")
	}
	defer bus.unsubscribe(sub)

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-bus.done:
			return nil
		case hint := <-sub.ch:
			if err := stream.Send(realtimeEventFromHint(hint)); err != nil {
				return err
			}
		}
	}
}
