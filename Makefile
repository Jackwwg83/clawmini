.PHONY: build test client server clean

build: client server

test:
	go test -race ./... -v

client:
	mkdir -p bin
	go build -o bin/clawmini-client ./cmd/client

server:
	mkdir -p bin
	go build -o bin/clawmini-server ./cmd/server

clean:
	rm -f bin/clawmini-client bin/clawmini-server
