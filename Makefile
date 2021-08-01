all:

	protoc -I pb --go_out=plugins=grpc:pb pb/feed.proto pb/api.proto pb/stock.proto

test:

	go test ./...

