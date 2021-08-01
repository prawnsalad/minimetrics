build:
	go build -tags sqlite_json -x -o minimetrics server.go

run: build
	go run -tags sqlite_json -x server.go

buildlinux:
	docker run --rm -v "$(shell pwd)":/app/ -w "/app/" golang:1.16.6-buster make build
