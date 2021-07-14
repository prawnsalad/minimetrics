build:
	go build -tags sqlite_json -o minimetrics server.go

run: build
	./minimetrics

