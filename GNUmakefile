default: fmt lint install generate

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	golangci-lint run

generate:
	cd tools; go generate ./...

fmt:
	gofmt -s -w -e .

test:
	go test -v -cover -timeout=120s -parallel=10 ./...

testacc:
	TF_ACC=1 go test -v -cover -timeout 120m ./...

.PHONY: fmt lint test testacc build install generate

gen-client:
	rm -rf internal/client/* 
	# Fetch latest version from main repo
	curl -s https://raw.githubusercontent.com/marmotdata/marmot/main/docs/swagger.yaml -o swagger.yaml
	swagger generate client -f swagger.yaml -A marmot --target internal/client
