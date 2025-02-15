/*
Copyright 2021 SPIRE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package spireapi

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"google.golang.org/grpc"
)

type Client interface {
	EntryClient
	TrustDomainClient
	io.Closer
}

func DialSocket(ctx context.Context, path string) (Client, error) {
	var target string
	if filepath.IsAbs(path) {
		target = "unix://" + path
	} else {
		target = "unix:" + path
	}
	grpcClient, err := grpc.DialContext(ctx, target, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("failed to dial API socket: %w", err)
	}

	return struct {
		EntryClient
		TrustDomainClient
		io.Closer
	}{
		EntryClient:       NewEntryClient(grpcClient),
		TrustDomainClient: NewTrustDomainClient(grpcClient),
		Closer:            grpcClient,
	}, nil
}
