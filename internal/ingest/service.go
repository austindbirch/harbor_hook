package ingest

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nsqio/go-nsq"

	"github.com/austindbirch/harbor_hook/internal/delivery"
	"github.com/austindbirch/harbor_hook/internal/metrics"
	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const deliveriesTopic = "deliveries"

type Server struct {
	webhookv1.UnimplementedWebhookServiceServer
	pool *pgxpool.Pool
	prod *nsq.Producer
}

// NewServer inits and returns a new Server struct, containing a webhookv1 Server, a pgxpool.Pool, and an nsq.Producer
func NewServer(pool *pgxpool.Pool, prod *nsq.Producer) *Server {
	return &Server{pool: pool, prod: prod}
}

// Ping attempts to ping the server, returning "pong" if successful
func (s *Server) Ping(ctx context.Context, _ *webhookv1.PingRequest) (*webhookv1.PingResponse, error) {
	return &webhookv1.PingResponse{Message: "pong"}, nil
}

// generateSecret generates a random base64-encoded string of length n
func generateSecret(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateEndpoint creates a new webhook endpoint
func (s *Server) CreateEndpoint(ctx context.Context, req *webhookv1.CreateEndpointRequest) (*webhookv1.CreateEndpointResponse, error) {
	// Ensure required fields are present
	if req.GetTenantId() == "" || req.GetUrl() == "" {
		return nil, errors.New("tenant_id and url are required")
	}
	if _, err := url.ParseRequestURI(req.GetUrl()); err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}

	// Check for secret; if not present, generate one
	secret := req.GetSecret()
	if secret == "" {
		var err error
		secret, err = generateSecret(32) // 256-bit
		if err != nil {
			return nil, err
		}
	}

	// Insert into database
	var id string
	var createdAt time.Time
	// This is some funky formatting, but it makes sense given the db query
	// In a real system, we'd NEVER return the secret after creation
	err := s.pool.QueryRow(ctx, `
		INSERT INTO harborhook.endpoints(tenant_id, url, secret)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`,
		req.GetTenantId(), req.GetUrl(), secret,
	).Scan(&id, &createdAt)
	if err != nil {
		return nil, err
	}

	// Return API response
	return &webhookv1.CreateEndpointResponse{
		Endpoint: &webhookv1.Endpoint{
			Id:        id,
			TenantId:  req.GetTenantId(),
			Url:       req.GetUrl(),
			CreatedAt: timestamppb.New(createdAt),
		},
	}, nil
}

// CreateSubscription creates a new webhook subscription and associates it with an endpoint
func (s *Server) CreateSubscription(ctx context.Context, req *webhookv1.CreateSubscriptionRequest) (*webhookv1.CreateSubscriptionResponse, error) {
	// Ensure required fields are present
	if req.GetTenantId() == "" || req.GetEventType() == "" || req.GetEndpointId() == "" {
		return nil, errors.New("tenant_id, event_type, and endpoint_id are required")
	}

	// Ensure endpoint exists and belongs to tenant
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM harborhook.endpoints
			WHERE id = $1 AND tenant_id = $2)`,
		req.GetEndpointId(), req.GetTenantId(),
	).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("endpoint %s not found for tenant %s", req.GetEndpointId(), req.GetTenantId())
	}

	// Insert into database
	var id string
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		INSERT INTO harborhook.subscriptions(tenant_id, event_type, endpoint_id)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`,
		req.GetTenantId(), req.GetEventType(), req.GetEndpointId(),
	).Scan(&id, &createdAt)
	if err != nil {
		return nil, err
	}

	// Return API response
	return &webhookv1.CreateSubscriptionResponse{
		Subscription: &webhookv1.Subscription{
			Id:         id,
			TenantId:   req.GetTenantId(),
			EventType:  req.GetEventType(),
			EndpointId: req.GetEndpointId(),
			CreatedAt:  timestamppb.New(createdAt),
		},
	}, nil
}

// Publish event publishes an arbitrary JSON payload to all subscribed endpoints
func (s *Server) PublishEvent(ctx context.Context, req *webhookv1.PublishEventRequest) (*webhookv1.PublishEventResponse, error) {
	// Ensure required fields are present
	if req.GetTenantId() == "" || req.GetEventType() == "" || req.GetPayload() == nil {
		return nil, errors.New("tenant_id, event_type, and payload are required")
	}

	// Insert event
	var eventID string
	var fanout int32
	payloadMap := req.GetPayload().AsMap()

	// Try insert; if conflict on idempotency, fetch existing id and DO NOT fanout
	if req.GetIdempotencyKey() != "" {
		err := s.pool.QueryRow(ctx, `
			INSERT INTO harborhook.events(tenant_id, event_type, payload, idempotency_key)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (tenant_id, idempotency_key) DO UPDATE SET event_type = EXCLUDED.event_type,
			RETURNING id`,
			req.GetTenantId(), req.GetEventType(), payloadMap, req.GetIdempotencyKey(),
		).Scan(&eventID)
		if err != nil {
			return nil, err
		}

		// Check if this was an existing row (no trivial way from INSERT ... DO UPDATE),
		// so gate fanout on whether this was the first time we saw this (by checking deliveries count)
		var existingCount int
		if err := s.pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM harborhook.deliveries
			WHERE event_id = $1`,
			eventID,
		).Scan(&existingCount); err != nil {
			return nil, err
		}
		if existingCount > 0 {
			// Idempotent re-publish: return 200 with fanout = 0
			return &webhookv1.PublishEventResponse{
				EventId:     eventID,
				FanoutCount: 0,
			}, nil
		}
	} else {
		// No idempotency key, always create a new event
		if err := s.pool.QueryRow(ctx, `
			INSERT INTO harborhook.events(tenant_id, event_type, payload)
			VALUES ($1, $2, $3)
			RETURNING id`,
			req.GetTenantId(), req.GetEventType(), payloadMap,
		).Scan(&eventID); err != nil {
			return nil, err
		}
	}

	// Fetch subscribers + insert deliveries (pending), then enqueue
	type subRow struct {
		EndpointID string
		URL        string
	}
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.url
		FROM harborhook.subscriptions s
		JOIN harborhook.endpoints e ON e.id = s.endpoint_id
		WHERE s.tenant_id = $1 AND s.event_type = $2`,
		req.GetTenantId(), req.GetEventType(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	batch := &pgx.Batch{}
	var targets []subRow
	for rows.Next() {
		var r subRow
		if err := rows.Scan(&r.EndpointID, &r.URL); err != nil {
			return nil, err
		}
		targets = append(targets, r)
		// Create pending delivery
		batch.Queue(`
			INSERT INTO harborhook.deliveries(event_id, endpoint_id, status)
			VALUES ($1, $2, 'pending')
			RETURNING id`, eventID, r.EndpointID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	br := s.pool.SendBatch(ctx, batch)
	defer br.Close()

	for _, t := range targets {
		var deliveryID string
		if err := br.QueryRow().Scan(&deliveryID); err != nil {
			return nil, err
		}
		task := delivery.Task{
			DeliveryID:  deliveryID,
			EventID:     eventID,
			TenantID:    req.GetTenantId(),
			EndpointID:  t.EndpointID,
			EndpointURL: t.URL,
			EventType:   req.GetEventType(),
			Payload:     payloadMap,
			Attempt:     0,
			PublishedAt: time.Now().UTC().Format(time.RFC3339),
		}
		b, _ := json.Marshal(task)
		if err := s.prod.Publish(deliveriesTopic, b); err != nil {
			return nil, fmt.Errorf("nsq publish: %w", err)
		}
		fanout++
	}

	// Increment Prometheus counter
	metrics.EventsPublishedTotal.Inc()

	// Return API response
	return &webhookv1.PublishEventResponse{
		EventId:     eventID,
		FanoutCount: fanout,
	}, nil
}
