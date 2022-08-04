PROJECT_BASE_DIR := $(realpath $(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
# prepend the project-local bin/ folder to $PATH so that we find build-time tools there
PATH := ${PROJECT_BASE_DIR}/bin:${PATH}

TEST_OPTS ?= -race -v

.PHONY: all
all: protos bin
	$(info TODO)

.PHONY: protos
protos: install-tools
	$(info Generating Go code from Protobuf definitions...)
	@buf generate ./perseusapi
	@buf build ./perseusapi -o ./perseusapi/perseusapi.protoset

.PHONY: lint-protos
lint-protos:
	@buf lint ./perseusapi

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
	go build -o ${PROJECT_BASE_DIR}/bin/perseus .
