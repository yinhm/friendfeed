// Code generated from realtime.proto. DO NOT EDIT.

package pb

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

const _ = grpc.SupportPackageIsVersion7

const Realtime_SubscribeRealtimeEvents_FullMethodName = "/pb.Realtime/SubscribeRealtimeEvents"

type RealtimeClient interface {
	SubscribeRealtimeEvents(ctx context.Context, in *SubscribeRealtimeEventsRequest, opts ...grpc.CallOption) (Realtime_SubscribeRealtimeEventsClient, error)
}

type realtimeClient struct {
	cc grpc.ClientConnInterface
}

func NewRealtimeClient(cc grpc.ClientConnInterface) RealtimeClient {
	return &realtimeClient{cc}
}

func (c *realtimeClient) SubscribeRealtimeEvents(ctx context.Context, in *SubscribeRealtimeEventsRequest, opts ...grpc.CallOption) (Realtime_SubscribeRealtimeEventsClient, error) {
	stream, err := c.cc.NewStream(ctx, &Realtime_ServiceDesc.Streams[0], Realtime_SubscribeRealtimeEvents_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &realtimeSubscribeRealtimeEventsClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Realtime_SubscribeRealtimeEventsClient interface {
	Recv() (*RealtimeEvent, error)
	grpc.ClientStream
}

type realtimeSubscribeRealtimeEventsClient struct {
	grpc.ClientStream
}

func (x *realtimeSubscribeRealtimeEventsClient) Recv() (*RealtimeEvent, error) {
	m := new(RealtimeEvent)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

type RealtimeServer interface {
	SubscribeRealtimeEvents(*SubscribeRealtimeEventsRequest, Realtime_SubscribeRealtimeEventsServer) error
}

type UnimplementedRealtimeServer struct{}

func (UnimplementedRealtimeServer) SubscribeRealtimeEvents(*SubscribeRealtimeEventsRequest, Realtime_SubscribeRealtimeEventsServer) error {
	return status.Errorf(codes.Unimplemented, "method SubscribeRealtimeEvents not implemented")
}

func RegisterRealtimeServer(s grpc.ServiceRegistrar, srv RealtimeServer) {
	s.RegisterService(&Realtime_ServiceDesc, srv)
}

func _Realtime_SubscribeRealtimeEvents_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(SubscribeRealtimeEventsRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(RealtimeServer).SubscribeRealtimeEvents(m, &realtimeSubscribeRealtimeEventsServer{stream})
}

type Realtime_SubscribeRealtimeEventsServer interface {
	Send(*RealtimeEvent) error
	grpc.ServerStream
}

type realtimeSubscribeRealtimeEventsServer struct {
	grpc.ServerStream
}

func (x *realtimeSubscribeRealtimeEventsServer) Send(m *RealtimeEvent) error {
	return x.ServerStream.SendMsg(m)
}

var Realtime_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "pb.Realtime",
	HandlerType: (*RealtimeServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "SubscribeRealtimeEvents",
			Handler:       _Realtime_SubscribeRealtimeEvents_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "realtime.proto",
}
