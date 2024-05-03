PROJECT_BASE_DIR := $(realpath $(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
# prepend the project-local bin/ folder to $PATH so that we find build-time tools there
PATH := ${PROJECT_BASE_DIR}/bin:${PATH}

TEST_RACE ?= -race
TEST_OPTS ?=

.PHONY: all
all: protos bin
	$(info TODO)

.PHONY: check
check: lint check-goreleaser-config test

.PHONY: protos
protos: install-tools check-buf-install
	$(info Generating Go code from Protobuf definitions...)
	@buf generate ./perseusapi
	@buf build ./perseusapi -o ./perseusapi/perseusapi.protoset

.PHONY: test
test:
	@go test ${TEST_OPTS} ${TEST_RACE} ./...

BUILD_TIME_TOOLS =\
	github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway\
	github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2 \
    google.golang.org/protobuf/cmd/protoc-gen-go \
    google.golang.org/grpc/cmd/protoc-gen-go-grpc \
	connectrpc.com/connect/cmd/protoc-gen-connect-go

.PHONY: install-tools
install-tools: ensure-local-bin-dir
	@GOBIN=${PROJECT_BASE_DIR}/bin go install ${BUILD_TIME_TOOLS}

.PHONY: ensure-local-bin-dir
ensure-local-bin-dir:
	@mkdir -p ${PROJECT_BASE_DIR}/bin/

.PHONY: bin
bin: ensure-local-bin-dir
	@go build -o ${PROJECT_BASE_DIR}/bin/perseus -ldflags='-X main.BuildDate=$(shell date -u +'%FT%R:%S') -X main.BuildVersion=v0.0.0-localdev.$(shell whoami).$(shell date -u +'%Y%m%d%H%M%S')' .

.PHONY: install
install:
	@go install -ldflags='-X main.BuildDate=$(shell date -u +'%FT%R:%S') -X main.BuildVersion=v0.0.0-localdev.$(shell whoami).$(shell date -u +'%Y%m%d%H%M%S')' .

.PHONY: lint
lint: lint-protos lint-go

.PHONY: lint-go
lint-go: check-golangci-lint-install
	$(info Linting Go code ...)
	@golangci-lint run ./...

.PHONY: lint-protos
lint-protos: check-buf-install
	$(info Linting Protobuf files ...)
	@buf lint ./perseusapi

.PHONY: check-goreleaser-config
check-goreleaser-config:
	$(info Validating goreleaser config ...)
	@goreleaser check

.PHONY: snapshot
snapshot: check-goreleaser-install
	@goreleaser release --snapshot --rm-dist --skip-publish

.PHONY: update-changelog
update-changelog: check-git-cliff-install
ifeq ("${NEXT_VERSION}", "")
	$(error Must specify the next version via $$NEXT_VERSION)
else
	git cliff --unreleased --tag ${NEXT_VERSION} --prepend CHANGELOG.md
endif

.PHONY: check-buf-install
check-buf-install:
ifeq ("$(shell command -v buf)", "")
	$(error buf was not found.  Please install it using the method of your choice. (https://docs.buf.build/installation))
endif

.PHONY: check-golangci-lint-install
check-golangci-lint-install:
ifeq ("$(shell command -v golangci-lint)", "")
	$(error golangci-lint was not found.  Please install it using the method of your choice. (https://golangci-lint.run/usage/install/#local-installation))
endif

.PHONY: check-goreleaser-install
check-goreleaser-install:
ifeq ("$(shell command -v goreleaser)", "")
	$(error goreleaser was not found.  Please install it using the method of your choice. (https://goreleaser.com/install))
endif

.PHONY: check-git-cliff-install
check-git-cliff-install:
ifeq ("$(shell command -v git-cliff)", "")
	$(error git-cliff was not found.  Please install it using the method of your choice. (https://git-cliff.org/docs/installation/))
endif
