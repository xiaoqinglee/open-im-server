// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package startrpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/tools/utils/datautil"
	"github.com/openimsdk/tools/utils/runtimeenv"
	"google.golang.org/grpc/status"

	"strconv"

	kdisc "github.com/openimsdk/open-im-server/v3/pkg/common/discoveryregister"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mw"
	"github.com/openimsdk/tools/utils/network"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Start rpc server.
func Start[T any](ctx context.Context, discovery *config.Discovery, prometheusConfig *config.Prometheus, listenIP,
	registerIP string, autoSetPorts bool, rpcPorts []int, index int, rpcRegisterName string, share *config.Share, config T, rpcFn func(ctx context.Context,
	config T, client discovery.SvcDiscoveryRegistry, server *grpc.Server) error, options ...grpc.ServerOption) error {

	var (
		rpcTcpAddr string
		netDone    = make(chan struct{}, 2)
		netErr     error
	)

	runTimeEnv := runtimeenv.PrintRuntimeEnvironment()

	if !autoSetPorts {
		rpcPort, err := datautil.GetElemByIndex(rpcPorts, index)
		if err != nil {
			return err
		}
		rpcTcpAddr = net.JoinHostPort(network.GetListenIP(listenIP), strconv.Itoa(rpcPort))
	} else {
		rpcTcpAddr = net.JoinHostPort(network.GetListenIP(listenIP), "0")
	}

	// var reg *prometheus.Registry
	// var metric *grpcprometheus.ServerMetrics
	if prometheusConfig.Enable {
		// cusMetrics := prommetrics.GetGrpcCusMetrics(rpcRegisterName, share)
		// reg, metric, _ = prommetrics.NewGrpcPromObj(cusMetrics)
		// options = append(options, mw.GrpcServer(), grpc.StreamInterceptor(metric.StreamServerInterceptor()),
		//	grpc.UnaryInterceptor(metric.UnaryServerInterceptor()))
		options = append(
			options, mw.GrpcServer(),
			prommetricsUnaryInterceptor(rpcRegisterName),
			prommetricsStreamInterceptor(rpcRegisterName),
		)
		prometheusPort, err := datautil.GetElemByIndex(prometheusConfig.Ports, index)
		if err != nil {
			return err
		}
		cs := prommetrics.GetGrpcCusMetrics(rpcRegisterName, discovery)
		go func() {
			if err := prommetrics.RpcInit(cs, prometheusPort); err != nil && !errors.Is(err, http.ErrServerClosed) {
				netErr = errs.WrapMsg(err, fmt.Sprintf("rpc %s prometheus start err: %d", rpcRegisterName, prometheusPort))
				netDone <- struct{}{}
			}
			// metric.InitializeMetrics(srv)
			// Create a HTTP server for prometheus.
			// httpServer = &http.Server{Handler: promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), Addr: fmt.Sprintf("0.0.0.0:%d", prometheusPort)}
			// if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			//	netErr = errs.WrapMsg(err, "prometheus start err", httpServer.Addr)
			//	netDone <- struct{}{}
			// }
		}()
	} else {
		options = append(options, mw.GrpcServer())
	}

	listener, err := net.Listen(
		"tcp",
		rpcTcpAddr,
	)
	if err != nil {
		return errs.WrapMsg(err, "listen err", "rpcTcpAddr", rpcTcpAddr)
	}

	_, portStr, _ := net.SplitHostPort(listener.Addr().String())
	registerIP = network.GetListenIP(registerIP)
	port, _ := strconv.Atoi(portStr)

	log.CInfo(ctx, "RPC server is initializing", "runTimeEnv", runTimeEnv, "rpcRegisterName", rpcRegisterName, "rpcPort", portStr,
		"prometheusPorts", prometheusConfig.Ports)

	defer listener.Close()
	client, err := kdisc.NewDiscoveryRegister(discovery, runTimeEnv)
	if err != nil {
		return err
	}

	defer client.Close()
	client.AddOption(mw.GrpcClient(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"LoadBalancingPolicy": "%s"}`, "round_robin")))

	srv := grpc.NewServer(options...)

	err = rpcFn(ctx, config, client, srv)
	if err != nil {
		return err
	}

	err = client.Register(
		rpcRegisterName,
		registerIP,
		port,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}

	go func() {
		err := srv.Serve(listener)
		if err != nil {
			netErr = errs.WrapMsg(err, "rpc start err: ", rpcTcpAddr)
			netDone <- struct{}{}
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	select {
	case <-sigs:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := gracefulStopWithCtx(ctx, srv.GracefulStop); err != nil {
			return err
		}
		return nil
	case <-netDone:
		return netErr
	}
}

func gracefulStopWithCtx(ctx context.Context, f func()) error {
	done := make(chan struct{}, 1)
	go func() {
		f()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return errs.New("timeout, ctx graceful stop")
	case <-done:
		return nil
	}
}

func prommetricsUnaryInterceptor(rpcRegisterName string) grpc.ServerOption {
	getCode := func(err error) int {
		if err == nil {
			return 0
		}
		rpcErr, ok := err.(interface{ GRPCStatus() *status.Status })
		if !ok {
			return -1
		}
		return int(rpcErr.GRPCStatus().Code())
	}
	return grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		prommetrics.RPCCall(rpcRegisterName, info.FullMethod, getCode(err))
		return resp, err
	})
}

func prommetricsStreamInterceptor(rpcRegisterName string) grpc.ServerOption {
	return grpc.ChainStreamInterceptor()
}
