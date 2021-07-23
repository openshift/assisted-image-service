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

run: certs
	podman run --rm -v $(PWD)/data:/data:Z -v $(PWD)/certs:/certs:Z -p$(PORT):$(PORT) -e PORT=$(PORT) -e HTTPS_KEY_FILE=/certs/tls.key -e HTTPS_CERT_FILE=/certs/tls.crt $(IMAGE)

.PHONY: certs
certs:
	openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout certs/tls.key -out certs/tls.crt -subj "/CN=localhost"

all: lint test build-image run
