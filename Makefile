.PHONY: build test vet proto clean

BINARY := bin/gchat
CGO_FLAGS := CGO_ENABLED=1 CGO_CFLAGS="-DSQLITE_ENABLE_FTS5"

build:
	$(CGO_FLAGS) go build -o $(BINARY) ./cmd/gchat

test:
	$(CGO_FLAGS) go test ./... -v

vet:
	$(CGO_FLAGS) go vet ./...

proto:
	PATH="$$HOME/go/bin:$$PATH" protoc --go_out=. --go_opt=paths=source_relative proto/googlechat.proto
	mv proto/googlechat.pb.go internal/proto/googlechat.pb.go

clean:
	rm -rf bin/

all: vet test build
