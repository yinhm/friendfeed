all:

	protoc -I proto --go_out=plugins=grpc:proto proto/feed.proto proto/api.proto proto/stock.proto

test:

	go test ./...

