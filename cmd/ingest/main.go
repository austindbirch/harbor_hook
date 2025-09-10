package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	grpc_health "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	cfg := config.FromEnv()
	ctx := context.Background()

	// DB connect
	pool, err := db.Connect(ctx, cfg.DSN())
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	// Create NSQ producer
	nsqConf := nsq.NewConfig()
	prod, err := nsq.NewProducer(cfg.NSQ.NsqdTCPAddr, nsqConf)
	if err != nil {
		log.Fatalf("nsq producer: %v", err)
	}
	defer prod.Stop()

	// Setup TLS if enabled
	var grpcOpts []grpc.ServerOption
	var httpTLSConfig *tls.Config

	if enableTLS := os.Getenv("ENABLE_TLS"); enableTLS == "true" {
		certFile := os.Getenv("TLS_CERT_PATH")
		keyFile := os.Getenv("TLS_KEY_PATH")
		caFile := os.Getenv("CA_CERT_PATH")

		if certFile == "" || keyFile == "" {
			log.Fatal("TLS enabled but cert/key paths not provided")
		}

		// Load server certificate
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			log.Fatalf("Failed to load server certificate: %v", err)
		}

		// Setup TLS config
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
		}

		// Load CA certificate for client verification
		if caFile != "" {
			caCert, err := os.ReadFile(caFile)
			if err != nil {
				log.Fatalf("Failed to read CA certificate: %v", err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				log.Fatal("Failed to append CA certificate")
			}
			tlsConfig.ClientCAs = caCertPool
		}

		// Configure gRPC with TLS
		creds := credentials.NewTLS(tlsConfig)
		grpcOpts = append(grpcOpts, grpc.Creds(creds))

		// Configure HTTP TLS
		httpTLSConfig = tlsConfig.Clone()
		httpTLSConfig.ClientAuth = tls.NoClientCert // HTTP doesn't require client certs from Envoy
	}

	// Setup JWT configuration
	if jwtIssuer := os.Getenv("JWT_ISSUER"); jwtIssuer != "" {
		jwtAudience := os.Getenv("JWT_AUDIENCE")
		if jwtAudience == "" {
			jwtAudience = "harborhook-api"
		}

		// For development, we'll skip JWT validation as Envoy handles it
		// TODO: implement JWT validation in the service as well as the HTTP edge
		log.Printf("JWT validation configured for issuer: %s, audience: %s (handled by Envoy)", jwtIssuer, jwtAudience)
	}

	// Start gRPC server
	grpcSrv := grpc.NewServer(grpcOpts...)
	hs := grpc_health.NewServer()
	healthpb.RegisterHealthServer(grpcSrv, hs)

	svc := ingest.NewServer(pool, prod)
	webhookv1.RegisterWebhookServiceServer(grpcSrv, svc)

	lis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		log.Fatalf("gRPC listen: %v", err)
	}
	go func() {
		log.Printf("ingest gRPC listening on %s (TLS: %v)", cfg.GRPCPort, httpTLSConfig != nil)
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

	// Configure grpc-gateway dial options based on TLS
	var dialOpts []grpc.DialOption
	if httpTLSConfig != nil {
		// Use TLS for internal communication with gRPC
		dialOpts = []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			InsecureSkipVerify: true, // Skip verification for internal communication
		}))}
	} else {
		dialOpts = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}

	if err := webhookv1.RegisterWebhookServiceHandlerFromEndpoint(ctx, gwmux, "localhost"+cfg.GRPCPort, dialOpts); err != nil {
		log.Fatalf("Err in registering service handler for grpc-gateway: %v", err)
	}
	mux.Handle("/", gwmux)

	// Start HTTP server
	httpSrv := &http.Server{
		Addr:      cfg.HTTPPort,
		Handler:   mux,
		TLSConfig: httpTLSConfig,
	}

	go func() {
		log.Printf("ingest HTTP listening on %s (TLS: %v)", cfg.HTTPPort, httpTLSConfig != nil)
		var err error
		if httpTLSConfig != nil {
			err = httpSrv.ListenAndServeTLS("", "") // Cert/key already in TLSConfig
		} else {
			err = httpSrv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
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
