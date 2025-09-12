package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	"github.com/austindbirch/harbor_hook/internal/logging"
	"github.com/austindbirch/harbor_hook/internal/metrics"
	"github.com/austindbirch/harbor_hook/internal/tracing"
	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	grpc_health "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	cfg := config.FromEnv()
	ctx := context.Background()

	// Initialize structured logging
	logger := logging.New("harborhook-ingest")

	// Initialize OpenTelemetry tracing
	shutdown, err := tracing.InitTracing(ctx, "harborhook-ingest")
	if err != nil {
		logger.Plain().WithError(err).Fatal("Failed to initialize tracing")
	}
	defer shutdown()

	// DB connect
	pool, err := db.Connect(ctx, cfg.DSN())
	if err != nil {
		logger.Plain().WithError(err).Fatal("db connect failed")
	}
	defer pool.Close()

	// Create NSQ producer
	nsqConf := nsq.NewConfig()
	prod, err := nsq.NewProducer(cfg.NSQ.NsqdTCPAddr, nsqConf)
	if err != nil {
		logger.Plain().WithError(err).Fatal("nsq producer creation failed")
	}
	defer prod.Stop()

	// Setup TLS if enabled
	var grpcOpts []grpc.ServerOption
	var httpTLSConfig *tls.Config

	// Add OpenTelemetry gRPC stats handler
	grpcOpts = append(grpcOpts,
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)

	if enableTLS := os.Getenv("ENABLE_TLS"); enableTLS == "true" {
		certFile := os.Getenv("TLS_CERT_PATH")
		keyFile := os.Getenv("TLS_KEY_PATH")
		caFile := os.Getenv("CA_CERT_PATH")

		if certFile == "" || keyFile == "" {
			logger.Plain().Fatal("TLS enabled but cert/key paths not provided")
		}

		// Load server certificate
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			logger.Plain().WithError(err).Fatal("Failed to load server certificate")
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
				logger.Plain().WithError(err).Fatal("Failed to read CA certificate")
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				logger.Plain().Fatal("Failed to append CA certificate")
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
		logger.Plain().WithFields(map[string]any{
			"issuer":   jwtIssuer,
			"audience": jwtAudience,
		}).Info("JWT validation configured (handled by Envoy)")
	}

	// Start gRPC server
	grpcSrv := grpc.NewServer(grpcOpts...)
	hs := grpc_health.NewServer()
	healthpb.RegisterHealthServer(grpcSrv, hs)

	svc := ingest.NewServer(pool, prod)
	webhookv1.RegisterWebhookServiceServer(grpcSrv, svc)

	lis, err := net.Listen("tcp", cfg.GRPCPort)
	if err != nil {
		logger.Plain().WithError(err).Fatal("gRPC listen failed")
	}
	go func() {
		logger.Plain().WithFields(map[string]any{
			"port": cfg.GRPCPort,
			"tls":  httpTLSConfig != nil,
		}).Info("ingest gRPC server starting")
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Plain().WithError(err).Fatal("gRPC serve failed")
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
		logger.Plain().WithError(err).Fatal("Failed to register service handler for grpc-gateway")
	}
	mux.Handle("/", gwmux)

	// Start HTTP server
	httpSrv := &http.Server{
		Addr:      cfg.HTTPPort,
		Handler:   mux,
		TLSConfig: httpTLSConfig,
	}

	go func() {
		logger.Plain().WithFields(map[string]any{
			"port": cfg.HTTPPort,
			"tls":  httpTLSConfig != nil,
		}).Info("ingest HTTP server starting")
		var err error
		if httpTLSConfig != nil {
			err = httpSrv.ListenAndServeTLS("", "") // Cert/key already in TLSConfig
		} else {
			err = httpSrv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			logger.Plain().WithError(err).Fatal("HTTP serve failed")
		}
	}()

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop
	
	logger.Plain().Info("Shutting down ingest service")
	grpcSrv.GracefulStop()
	_ = httpSrv.Shutdown(context.Background())
	logger.Plain().Info("ingest service stopped")
}
