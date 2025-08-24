package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Status struct {
	OK       bool   `json:"ok"`
	Message  string `json:"message,omitempty"`
	Database bool   `json:"database,omitempty"`
}

// HTTPHandler returns an HTTP handler that reports the health status of the service
func HTTPHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := Status{OK: true, Message: "ok", Database: true}

		if pool != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
			defer cancel()
			if err := pool.Ping(ctx); err != nil {
				st.OK = false
				st.Message = "db ping failed"
				st.Database = false
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st)
	}
}
