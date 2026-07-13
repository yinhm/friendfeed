all:

	protoc -I pb --go_out=plugins=grpc:pb pb/feed.proto pb/api.proto pb/stock.proto

web:
	cd httpd/app && pnpm install --frozen-lockfile
	cd httpd/app && pnpm run build
	cd httpd && go build

test:

	go test ./...
