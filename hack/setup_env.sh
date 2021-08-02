#!/bin/bash

yum install -y install podman genisoimage && yum clean all
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.25.0
go get -u golang.org/x/tools/cmd/goimports@v0.1.5 github.com/golang/mock/mockgen@v1.6.0
