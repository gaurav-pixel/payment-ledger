.PHONY: all build test test-concurrency bench run audit clean docker-up docker-down

all: build test

build:
	go build -o bin/server.exe ./cmd/server
	go build -o bin/audit.exe ./cmd/audit

test:
	go test -v ./...

test-concurrency:
	go test -v -run TestConcurrentBidirectionalTransfers ./tests

bench:
	go test -bench=BenchmarkExecuteTransfer ./tests

run: build
	./bin/server.exe

audit:
	go run ./cmd/audit

docker-up:
	docker-compose up -d

docker-down:
	docker-compose down -v

clean:
	rm -rf bin/ server.exe audit.exe
