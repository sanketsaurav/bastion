.PHONY: build test fmt vet check

build:
	go build -o bastion ./cmd/bastion

test:
	go test ./...

fmt:
	gofmt -l -w .

vet:
	go vet ./...

check:
	@fmtout="$$(gofmt -l .)"; if [ -n "$$fmtout" ]; then echo "gofmt needed:"; echo "$$fmtout"; exit 1; fi
	go vet ./...
	go test ./...
