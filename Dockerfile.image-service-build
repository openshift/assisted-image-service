# Used to build external packages:
FROM quay.io/centos/centos:stream9 AS packages

# Install the build tools and the required libraries:
RUN \
  dnf install -y \
  \
  gcc \
  libtool \
  make \
  \
  libselinux-devel \
  libuuid-devel \
  libzstd-devel \
  lz4-devel \
  zlib-devel \
  \
  && \
  dnf clean all && \
  update-crypto-policies --set DEFAULT:PQ

# Build the packages:
COPY packages /packages
RUN cd /packages && ./build.sh

FROM registry.access.redhat.com/ubi9/go-toolset:1.26 AS golang

ENV GOFLAGS=""

ARG GOLANGCI_LINT_VERSION=v2.15.0

RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/${GOLANGCI_LINT_VERSION}/install.sh | sh -s -- -b $(go env GOPATH)/bin ${GOLANGCI_LINT_VERSION} && \
    go install golang.org/x/tools/cmd/goimports@v0.34.0 && \
    go install go.uber.org/mock/mockgen@v0.6.0

FROM quay.io/centos/centos:stream9

RUN dnf install -y podman \
    genisoimage \
    make \
    openssl-devel \
    gcc \
    git \
    cpio \
    squashfs-tools \
    && dnf clean all && \
    update-crypto-policies --set DEFAULT:PQ

ENV GOROOT=/usr/lib/golang
ENV GOPATH=/opt/app-root/src/go
ENV GO111MODULE=on
ENV PATH=$PATH:$GOROOT/bin:$GOPATH/bin

COPY --from=golang $GOPATH $GOPATH
COPY --from=golang $GOROOT $GOROOT

# Copy the very minimum that we need from the external packages container. That is the 'dump.erofs'
# binary and the compression library (from the 'xz' package) that it needs.
COPY --from=packages /usr/local/lib/liblzma.so.5 /usr/local/lib
COPY --from=packages /usr/local/bin/dump.erofs /usr/local/bin

RUN chmod 775 -R $GOPATH && chmod 775 -R $GOROOT
