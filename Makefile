IMAGE := $(or ${IMAGE}, quay.io/ocpmetal/assisted-image-service:latest)
PWD = $(shell pwd)

build:
	CGO_ENABLED=0 go build -o build/assisted-image-service main.go

build-image:
	podman build -f Dockerfile.image-service . -t $(IMAGE)

lint:
	golangci-lint run -v

run:
	podman run --rm -v $(PWD)/data:/data:Z -p8080:8080 $(IMAGE)

all: lint build-image run
