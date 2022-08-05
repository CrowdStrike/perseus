PROJECT_BASE_DIR := $(realpath $(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
# prepend the project-local bin/ folder to $PATH so that we find build-time tools there
PATH := ${PROJECT_BASE_DIR}/bin:${PATH}

TEST_OPTS ?= -race -v

.PHONY: all
all: protos bin
	$(info TODO)

.PHONY: protos
protos: install-tools check-buf-install
	$(info Generating Go code from Protobuf definitions...)
	@buf generate ./perseusapi
	@buf build ./perseusapi -o ./perseusapi/perseusapi.protoset

.PHONY: test
test:
	@go test ${TEST_OPTS} ./...

BUILD_TIME_TOOLS =\
	github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway\
	github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2 \
    google.golang.org/protobuf/cmd/protoc-gen-go \
    google.golang.org/grpc/cmd/protoc-gen-go-grpc

.PHONY: install-tools
install-tools: ensure-local-bin-dir
	@GOBIN=${PROJECT_BASE_DIR}/bin go install ${BUILD_TIME_TOOLS}

.PHONY: ensure-local-bin-dir
ensure-local-bin-dir:
	@mkdir -p ${PROJECT_BASE_DIR}/bin/

.PHONY: bin
bin: ensure-local-bin-dir
	@go build -o ${PROJECT_BASE_DIR}/bin/perseus .

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

.PHONY: snapshot
snapshot: check-goreleaser-install
	@goreleaser release --snapshot --rm-dist

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
