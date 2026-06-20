BINARY := pgproof
VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/shaxzodbek-uzb/pgproof/internal/buildinfo.Version=$(VERSION) \
	-X github.com/shaxzodbek-uzb/pgproof/internal/buildinfo.Commit=$(COMMIT) \
	-X github.com/shaxzodbek-uzb/pgproof/internal/buildinfo.Date=$(DATE)

.PHONY: build install test race vet fmt fmtcheck check clean tidy

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

fmtcheck:
	@out="$$(gofmt -s -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

check: fmtcheck vet test

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)
	rm -rf dist
