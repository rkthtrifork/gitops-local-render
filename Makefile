.PHONY: build format format-check test verify

build:
	CGO_ENABLED=0 go build -trimpath -o bin/gitops-local-render ./cmd/gitops-local-render

format:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

format-check:
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"

test:
	go test ./...

verify: format-check
	go vet ./...
	go test -race ./...
	CGO_ENABLED=0 go build -o /dev/null ./cmd/gitops-local-render
