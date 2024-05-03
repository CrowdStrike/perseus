//go:build tools
// +build tools

package main

import (
	_ "connectrpc.com/connect/cmd/protoc-gen-connect-go"
	_ "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
