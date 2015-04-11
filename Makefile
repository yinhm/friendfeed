all:

	protoc -I proto --go_out=plugins=grpc:proto proto/feed.proto proto/api.proto

test:

	go test ./...

