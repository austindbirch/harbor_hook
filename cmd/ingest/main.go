package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/austindbirch/harbor_hook/internal/config"
	"github.com/austindbirch/harbor_hook/internal/db"
	"github.com/austindbirch/harbor_hook/internal/health"

	"google.golang.org/grpc"
	grpc_health "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	cfg := config.FromEnv()

	// DB connect (for now, just ping)
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DSN())
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	// gRPC server (for now, health only)
	grpcSrv := grpc.NewServer()
	hs := grpc_health.NewServer()
	healthpb.RegisterHealthServer(grpcSrv, hs)

	lis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		log.Fatalf("gRPC listen: %v", err)
	}
	go func() {
		log.Printf("ingest gRPC listening on %s", cfg.GRPCPort)
		if err := grpcSrv.Serve(lis); err != nil {
			log.Fatalf("gRPC serve: %v", err)
		}
	}()

	// HTTP health
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", health.HTTPHandler(pool))
	httpSrv := &http.Server{Addr: cfg.HTTPPort, Handler: mux}
	go func() {
		log.Printf("ingest HTTP listening on %s", cfg.HTTPPort)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP serve: %v", err)
		}
	}()

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop
	grpcSrv.GracefulStop()
	_ = httpSrv.Shutdown(context.Background())
	log.Println("ingest stopped")
}
