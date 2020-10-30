/*
Copyright 2020 The Knative Authors

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

package envoy

import (
	"context"
	"fmt"
	"net"

	envoyv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	xds "github.com/envoyproxy/go-control-plane/pkg/server"
	"google.golang.org/grpc"
)

const (
	grpcMaxConcurrentStreams = 1000000
)

type XdsServer struct {
	managementPort uint
	ctx            context.Context
	server         xds.Server
	snapshotCache  cache.SnapshotCache
}

// hasher returns node ID as an ID
type hasher struct {
}

func (h hasher) ID(node *core.Node) string {
	if node == nil {
		return "unknown"
	}
	return node.Id
}

func NewXdsServer(managementPort uint, callbacks xds.Callbacks) *XdsServer {
	ctx := context.Background()
	snapshotCache := cache.NewSnapshotCache(true, hasher{}, nil)
	srv := xds.NewServer(ctx, snapshotCache, callbacks)

	return &XdsServer{
		managementPort: managementPort,
		ctx:            ctx,
		server:         srv,
		snapshotCache:  snapshotCache,
	}
}

// RunManagementServer starts an xDS server at the given Port.
func (envoyXdsServer *XdsServer) RunManagementServer() error {
	port := envoyXdsServer.managementPort
	server := envoyXdsServer.server

	grpcServer := grpc.NewServer(grpc.MaxConcurrentStreams(grpcMaxConcurrentStreams))
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	// register services
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	envoyv2.RegisterClusterDiscoveryServiceServer(grpcServer, server)
	envoyv2.RegisterListenerDiscoveryServiceServer(grpcServer, server)
	envoyv2.RegisterRouteDiscoveryServiceServer(grpcServer, server)

	errCh := make(chan error)
	go func() {
		if err = grpcServer.Serve(lis); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-envoyXdsServer.ctx.Done():
		grpcServer.GracefulStop()
		return nil
	case err := <-errCh:
		return fmt.Errorf("failed to serve: %w", err)
	}
}

func (envoyXdsServer *XdsServer) GetSnapshot(nodeid string) (cache.Snapshot, error) {
	return envoyXdsServer.snapshotCache.GetSnapshot(nodeid)
}

func (envoyXdsServer *XdsServer) SetSnapshot(snapshot *cache.Snapshot, nodeID string) error {
	return envoyXdsServer.snapshotCache.SetSnapshot(nodeID, *snapshot)
}
