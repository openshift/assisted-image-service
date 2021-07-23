IMAGE := $(or ${IMAGE}, quay.io/ocpmetal/assisted-image-service:latest)
PWD = $(shell pwd)
PORT := $(or ${PORT}, 8080)

build:
	CGO_ENABLED=0 go build -o build/assisted-image-service main.go

build-image:
	podman build -f Dockerfile.image-service . -t $(IMAGE)

lint:
	golangci-lint run -v

test:
	go test ./...

generate:
	go generate $(shell go list ./...)
	$(MAKE) format

format:
	@goimports -w -l main.go internal pkg || /bin/true

run:
	podman run --rm -v $(PWD)/data:/data:Z -p$(PORT):$(PORT) -e PORT=$(PORT) $(IMAGE)

all: lint test build-image run
