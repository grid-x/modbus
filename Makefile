GO_FILES := $(shell find . -type f -name "*.go")
GO_BUILD := CGO_ENABLED=0 go build -ldflags "-w -s"
GO_TOOLS := gridx/golang-tools:master-839443d
DOCKER_RUN := docker run --rm -v $$PWD:/go/src/github.com/grid-x/modbus -w /go/src/github.com/grid-x/modbus
GO_RUN := ${DOCKER_RUN} ${GO_TOOLS} bash -c

BRANCH := $(shell echo ${BUILDKITE_BRANCH} | sed 's/\//_/g')

all: bin/

.PHONY: test
test:
	go test -v $(shell glide nv)

.PHONY: lint
lint:
	golint -set_exit_status $(shell glide nv)

.PHONY: build
build:
	go build $(shell glide nv)

ci_test:
	${GO_RUN} "make test"

ci_lint:
	${GO_RUN} "make lint"

ci_build:
	${GO_RUN} "make build"
