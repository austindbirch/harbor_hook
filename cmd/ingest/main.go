package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/nsqio/go-nsq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/austindbirch/harbor_hook/internal/config"
	"github.com/austindbirch/harbor_hook/internal/db"
	"github.com/austindbirch/harbor_hook/internal/health"
	"github.com/austindbirch/harbor_hook/internal/ingest"
	"github.com/austindbirch/harbor_hook/internal/metrics"
	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpc_health "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	cfg := config.FromEnv()
	ctx := context.Background()

	// DB connect (for now, just ping)
	pool, err := db.Connect(ctx, cfg.DSN())
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	// NSQ producer
	nsqConf := nsq.NewConfig()
	prod, err := nsq.NewProducer(cfg.NSQ.NsqdTCPAddr, nsqConf)
	if err != nil {
		log.Fatalf("nsq producer: %v", err)
	}
	defer prod.Stop()

	// gRPC server
	grpcSrv := grpc.NewServer()
	hs := grpc_health.NewServer()
	healthpb.RegisterHealthServer(grpcSrv, hs)

	svc := ingest.NewServer(pool, prod)
	webhookv1.RegisterWebhookServiceServer(grpcSrv, svc)

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

	// HTTP mux: health, metrics, grpc-gateway
	reg := prometheus.NewRegistry()
	metrics.MustRegister(reg)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", health.HTTPHandler(pool))
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	gwmux := runtime.NewServeMux()
	dialOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := webhookv1.RegisterWebhookServiceHandlerFromEndpoint(ctx, gwmux, "localhost"+cfg.GRPCPort, dialOpts); err != nil {
		log.Fatalf("grpc-gateway register: %v", err)
	}
	mux.Handle("/", gwmux)

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
