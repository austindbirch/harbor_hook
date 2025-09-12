package ingest

import (
	"context"
	"crypto/rand"
	"database/sql"
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
	"github.com/austindbirch/harbor_hook/internal/tracing"
	webhookv1 "github.com/austindbirch/harbor_hook/protogen/go/api/webhook/v1"

	"go.opentelemetry.io/otel/attribute"
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
	// Start tracing span
	ctx, span := tracing.StartSpan(ctx, "ingest.PublishEvent",
		attribute.String("tenant_id", req.GetTenantId()),
		attribute.String("event_type", req.GetEventType()),
		attribute.String("idempotency_key", req.GetIdempotencyKey()),
	)
	defer span.End()

	// Ensure required fields are present
	if req.GetTenantId() == "" || req.GetEventType() == "" || req.GetPayload() == nil {
		err := errors.New("tenant_id, event_type, and payload are required")
		tracing.SetSpanError(ctx, err)
		return nil, err
	}

	// Insert event
	var eventID string
	var fanout int32
	payloadMap := req.GetPayload().AsMap()
	// Marshal once, pass as TEXT and cast to ::jsonb in SQL (avoids some driver type ambiguity issues)
	payloadJSON, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	// Try insert; if conflict on idempotency, fetch existing id and DO NOT fanout
	if req.GetIdempotencyKey() != "" {
		// 1) Insert-or-ignore (no RETURNING here)
		tracing.AddSpanEvent(ctx, "db.insert_event_idempotent")
		ct, err := s.pool.Exec(ctx, `
			INSERT INTO harborhook.events(tenant_id, event_type, payload, idempotency_key)
			VALUES ($1, $2, $3::jsonb, $4)
			ON CONFLICT ON CONSTRAINT uq_events_tenant_idem DO NOTHING`,
			req.GetTenantId(), req.GetEventType(), string(payloadJSON), req.GetIdempotencyKey(),
		)
		if err != nil {
			tracing.SetSpanError(ctx, err)
			return nil, fmt.Errorf("insert events (idempotent): %w", err)
		}

		// 2) Fetch the event id whether inserted now or already existed
		tracing.AddSpanEvent(ctx, "db.select_event_id")
		if err := s.pool.QueryRow(ctx, `
			SELECT id FROM harborhook.events
		 	WHERE tenant_id = $1 AND idempotency_key = $2
		 	LIMIT 1`,
			req.GetTenantId(), req.GetIdempotencyKey(),
		).Scan(&eventID); err != nil {
			tracing.SetSpanError(ctx, err)
			return nil, fmt.Errorf("select event id (idempotent): %w", err)
		}

		// 3) If we did NOT insert now (rows affected == 0), check if deliveries already exist.
		//    If they do, treat as duplicate publish → no fanout.
		if ct.RowsAffected() == 0 {
			tracing.AddSpanEvent(ctx, "db.check_duplicate_deliveries")
			var existingCount int
			if err := s.pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM harborhook.deliveries WHERE event_id = $1`,
				eventID,
			).Scan(&existingCount); err != nil {
				tracing.SetSpanError(ctx, err)
				return nil, fmt.Errorf("count existing deliveries: %w", err)
			}
			if existingCount > 0 {
				tracing.AddSpanEvent(ctx, "duplicate_event_detected")
				span.SetAttributes(attribute.String("event_id", eventID))
				return &webhookv1.PublishEventResponse{
					EventId:     eventID,
					FanoutCount: 0,
				}, nil
			}
		}
	} else {
		// No idempotency key → always create a new event
		tracing.AddSpanEvent(ctx, "db.insert_event_new")
		if err := s.pool.QueryRow(ctx, `
			INSERT INTO harborhook.events(tenant_id, event_type, payload)
			VALUES ($1, $2, $3::jsonb)
			RETURNING id`,
			req.GetTenantId(), req.GetEventType(), string(payloadJSON),
		).Scan(&eventID); err != nil {
			tracing.SetSpanError(ctx, err)
			return nil, fmt.Errorf("insert events (no-idem): %w", err)
		}
	}
	
	// Add event ID to span attributes
	span.SetAttributes(attribute.String("event_id", eventID))

	// Fetch subscribers + insert deliveries (pending), then enqueue
	tracing.AddSpanEvent(ctx, "db.query_subscribers")
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
		tracing.SetSpanError(ctx, err)
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
		// Create queued delivery
		batch.Queue(`
			INSERT INTO harborhook.deliveries(event_id, endpoint_id, status)
			VALUES ($1, $2, 'queued')
			RETURNING id`,
			eventID, r.EndpointID)
	}
	if err := rows.Err(); err != nil {
		tracing.SetSpanError(ctx, err)
		return nil, err
	}

	// Add subscriber count to tracing
	span.SetAttributes(attribute.Int("subscribers_count", len(targets)))
	
	if len(targets) > 0 {
		tracing.AddSpanEvent(ctx, "db.create_deliveries_batch", attribute.Int("delivery_count", len(targets)))
		br := s.pool.SendBatch(ctx, batch)
		defer br.Close()

		// Extract trace headers for NSQ propagation
		traceHeaders := tracing.PropagateTraceToNSQ(ctx)
		
		for _, t := range targets {
			var deliveryID string
			if err := br.QueryRow().Scan(&deliveryID); err != nil {
				tracing.SetSpanError(ctx, err)
				return nil, err
			}
			task := delivery.Task{
				DeliveryID:   deliveryID,
				EventID:      eventID,
				TenantID:     req.GetTenantId(),
				EndpointID:   t.EndpointID,
				EndpointURL:  t.URL,
				EventType:    req.GetEventType(),
				Payload:      payloadMap,
				Attempt:      0,
				PublishedAt:  time.Now().UTC().Format(time.RFC3339),
				TraceHeaders: traceHeaders,
			}
			b, _ := json.Marshal(task)
			if err := s.prod.Publish(deliveriesTopic, b); err != nil {
				tracing.SetSpanError(ctx, err)
				return nil, fmt.Errorf("nsq publish: %w", err)
			}
			fanout++
		}
		
		tracing.AddSpanEvent(ctx, "nsq.published_tasks", 
			attribute.Int("task_count", int(fanout)),
			attribute.String("topic", deliveriesTopic))
	}

	// Increment Prometheus counter with tenant_id label
	metrics.RecordEventPublished(req.GetTenantId())

	// Add final span attributes
	span.SetAttributes(
		attribute.Int("fanout_count", int(fanout)),
		attribute.Bool("has_idempotency_key", req.GetIdempotencyKey() != ""),
	)

	// Return API response
	return &webhookv1.PublishEventResponse{
		EventId:     eventID,
		FanoutCount: fanout,
	}, nil
}

// GetDeliveryStatus returns delivery attempts for a given event, with optional filters
func (s *Server) GetDeliveryStatus(ctx context.Context, req *webhookv1.GetDeliveryStatusRequest) (*webhookv1.GetDeliveryStatusResponse, error) {
    if req.GetEventId() == "" {
        return nil, errors.New("event_id is required")
    }

    // Build dynamic WHERE clause
    args := []any{req.GetEventId()}
    where := "d.event_id = $1"
    argn := 1
    if eid := req.GetEndpointId(); eid != "" {
        argn++
        where += fmt.Sprintf(" AND d.endpoint_id = $%d", argn)
        args = append(args, eid)
    }
    if from := req.GetFrom(); from != nil && from.Seconds != 0 {
        argn++
        where += fmt.Sprintf(" AND d.enqueued_at >= $%d", argn)
        args = append(args, from.AsTime())
    }
    if to := req.GetTo(); to != nil && to.Seconds != 0 {
        argn++
        where += fmt.Sprintf(" AND d.enqueued_at <= $%d", argn)
        args = append(args, to.AsTime())
    }
    limit := int32(10)
    if req.GetLimit() > 0 {
        limit = req.GetLimit()
    }
    argn++
    args = append(args, limit)

    q := fmt.Sprintf(`
        SELECT d.id, d.event_id, d.endpoint_id, d.replay_of, d.status, d.http_status,
               COALESCE(d.error_reason, d.last_error) AS err,
               d.enqueued_at, d.dequeued_at, d.sent_at, d.delivered_at, d.failed_at, d.dlq_at
        FROM harborhook.deliveries d
        WHERE %s
        ORDER BY d.enqueued_at ASC
        LIMIT $%d`, where, argn)

    rows, err := s.pool.Query(ctx, q, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var out []*webhookv1.DeliveryAttempt
    for rows.Next() {
        var (
            id, eventID, endpointID string
            replayOf sql.NullString
            statusStr sql.NullString
            httpStatus sql.NullInt32
            errReason sql.NullString
            enq, deq, sent, deliv, fail, dlq sql.NullTime
        )
        if err := rows.Scan(&id, &eventID, &endpointID, &replayOf, &statusStr, &httpStatus, &errReason,
            &enq, &deq, &sent, &deliv, &fail, &dlq,
        ); err != nil {
            return nil, err
        }
        out = append(out, &webhookv1.DeliveryAttempt{
            DeliveryId:  id,
            EventId:     eventID,
            EndpointId:  endpointID,
            ReplayOf:    nullStr(replayOf),
            Status:      mapStatus(nullStr(statusStr)),
            HttpStatus:  nullI32(httpStatus),
            ErrorReason: nullStr(errReason),
            EnqueuedAt:  toTS(enq),
            DequeuedAt:  toTS(deq),
            SentAt:      toTS(sent),
            DeliveredAt: toTS(deliv),
            FailedAt:    toTS(fail),
            DlqAt:       toTS(dlq),
        })
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return &webhookv1.GetDeliveryStatusResponse{Attempts: out}, nil
}

// ReplayDelivery enqueues a new delivery referencing a previous attempt
func (s *Server) ReplayDelivery(ctx context.Context, req *webhookv1.ReplayDeliveryRequest) (*webhookv1.ReplayDeliveryResponse, error) {
    if req.GetDeliveryId() == "" {
        return nil, errors.New("delivery_id is required")
    }

    // Fetch source delivery + event/endpoint details
    var (
        eventID, endpointID, tenantID, eventType, endpointURL string
        payloadJSON string
    )
    err := s.pool.QueryRow(ctx, `
        SELECT d.event_id, d.endpoint_id, ev.tenant_id, ev.event_type, ev.payload::text, ep.url
        FROM harborhook.deliveries d
        JOIN harborhook.events ev ON ev.id = d.event_id
        JOIN harborhook.endpoints ep ON ep.id = d.endpoint_id
        WHERE d.id = $1
    `, req.GetDeliveryId()).Scan(&eventID, &endpointID, &tenantID, &eventType, &payloadJSON, &endpointURL)
    if err != nil {
        return nil, fmt.Errorf("source delivery not found: %w", err)
    }

    // Insert new delivery referencing replay_of
    var newID string
    err = s.pool.QueryRow(ctx, `
        INSERT INTO harborhook.deliveries(event_id, endpoint_id, status, replay_of, replay_reason)
        VALUES ($1,$2,'queued',$3,$4)
        RETURNING id
    `, eventID, endpointID, req.GetDeliveryId(), req.GetReason()).Scan(&newID)
    if err != nil {
        return nil, fmt.Errorf("insert replay: %w", err)
    }

    // Publish the new task
    var payload map[string]any
    _ = json.Unmarshal([]byte(payloadJSON), &payload)
    task := delivery.Task{
        DeliveryID:  newID,
        EventID:     eventID,
        TenantID:    tenantID,
        EndpointID:  endpointID,
        EndpointURL: endpointURL,
        EventType:   eventType,
        Payload:     payload,
        Attempt:     0,
        PublishedAt: time.Now().UTC().Format(time.RFC3339),
    }
    b, _ := json.Marshal(task)
    if err := s.prod.Publish(deliveriesTopic, b); err != nil {
        return nil, fmt.Errorf("nsq publish: %w", err)
    }

    // Return the newly queued attempt
    return &webhookv1.ReplayDeliveryResponse{
        NewAttempt: &webhookv1.DeliveryAttempt{
            DeliveryId: newID,
            EventId:    eventID,
            EndpointId: endpointID,
            ReplayOf:   req.GetDeliveryId(),
            Status:     webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_QUEUED,
        },
    }, nil
}

// ListDLQ returns deliveries present in the DLQ
func (s *Server) ListDLQ(ctx context.Context, req *webhookv1.ListDLQRequest) (*webhookv1.ListDLQResponse, error) {
    limit := int32(10)
    if req.GetLimit() > 0 {
        limit = req.GetLimit()
    }

    args := []any{}
    where := "1=1"
    if eid := req.GetEndpointId(); eid != "" {
        where += " AND d.endpoint_id = $1"
        args = append(args, eid)
    }
    // Use DLQ table ordering
    q := fmt.Sprintf(`
        SELECT d.id, d.event_id, d.endpoint_id, d.replay_of, d.status, d.http_status,
               COALESCE(d.error_reason, d.last_error) AS err,
               d.enqueued_at, d.dequeued_at, d.sent_at, d.delivered_at, d.failed_at, d.dlq_at
        FROM harborhook.deliveries d
        JOIN harborhook.dlq q ON q.delivery_id = d.id
        WHERE %s
        ORDER BY q.created_at DESC
        LIMIT %d`, where, limit)

    rows, err := s.pool.Query(ctx, q, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []*webhookv1.DeliveryAttempt
    for rows.Next() {
        var (
            id, eventID, endpointID string
            replayOf sql.NullString
            statusStr sql.NullString
            httpStatus sql.NullInt32
            errReason sql.NullString
            enq, deq, sent, deliv, fail, dlq sql.NullTime
        )
        if err := rows.Scan(&id, &eventID, &endpointID, &replayOf, &statusStr, &httpStatus, &errReason,
            &enq, &deq, &sent, &deliv, &fail, &dlq,
        ); err != nil {
            return nil, err
        }
        out = append(out, &webhookv1.DeliveryAttempt{
            DeliveryId:  id,
            EventId:     eventID,
            EndpointId:  endpointID,
            ReplayOf:    nullStr(replayOf),
            Status:      mapStatus(nullStr(statusStr)),
            HttpStatus:  nullI32(httpStatus),
            ErrorReason: nullStr(errReason),
            EnqueuedAt:  toTS(enq),
            DequeuedAt:  toTS(deq),
            SentAt:      toTS(sent),
            DeliveredAt: toTS(deliv),
            FailedAt:    toTS(fail),
            DlqAt:       toTS(dlq),
        })
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return &webhookv1.ListDLQResponse{Dead: out}, nil
}

// --- helpers ---

func nullStr(ns sql.NullString) string { if ns.Valid { return ns.String }; return "" }
func nullI32(ni sql.NullInt32) int32 { if ni.Valid { return ni.Int32 }; return 0 }
func toTS(nt sql.NullTime) *timestamppb.Timestamp { if nt.Valid { return timestamppb.New(nt.Time) }; return nil }

func mapStatus(s string) webhookv1.DeliveryAttemptStatus {
    switch s {
    case "queued", "pending":
        return webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_QUEUED
    case "inflight":
        return webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_IN_FLIGHT
    case "delivered", "ok":
        return webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_DELIVERED
    case "failed":
        return webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_FAILED
    case "dead":
        return webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_DEAD_LETTERED
    default:
        return webhookv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_UNSPECIFIED
    }
}
