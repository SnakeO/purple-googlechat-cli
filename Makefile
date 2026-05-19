.PHONY: build test vet proto clean

BINARY := bin/gchat

build:
	go build -o $(BINARY) ./cmd/gchat

test:
	go test ./... -v

vet:
	go vet ./...

proto:
	PATH="$$HOME/go/bin:$$PATH" protoc --go_out=. --go_opt=paths=source_relative proto/googlechat.proto
	mv proto/googlechat.pb.go internal/proto/googlechat.pb.go

clean:
	rm -rf bin/

all: vet test build
