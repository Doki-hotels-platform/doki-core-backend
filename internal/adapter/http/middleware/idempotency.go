package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	IdempotencyHeader = "Idempotency-Key"
	idempLockTTL      = 30 * time.Second
	idempResponseTTL  = 24 * time.Hour
)

type CachedResponse struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       []byte            `json:"body"`
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

// IdempotencyMiddleware ensures mutating requests with an Idempotency-Key header
// execute exactly once and return cached responses on subsequent retries.
func IdempotencyMiddleware(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			idempKey := r.Header.Get(IdempotencyHeader)

			// Non-mutating methods or requests without idempotency header pass through
			if idempKey == "" || r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Validate UUID or valid string format
			if _, err := uuid.Parse(idempKey); err != nil && len(idempKey) < 8 {
				http.Error(w, `{"error":"INVALID_IDEMPOTENCY_KEY","message":"Idempotency-Key must be a valid UUID or token"}`, http.StatusBadRequest)
				return
			}

			ctx := r.Context()
			respKey := fmt.Sprintf("idemp:resp:%s", idempKey)
			lockKey := fmt.Sprintf("idemp:lock:%s", idempKey)

			// 1. Check if response is already cached from a prior successful execution
			cachedJSON, err := rdb.Get(ctx, respKey).Bytes()
			if err == nil && len(cachedJSON) > 0 {
				var cached CachedResponse
				if jsonErr := json.Unmarshal(cachedJSON, &cached); jsonErr == nil {
					for k, v := range cached.Headers {
						w.Header().Set(k, v)
					}
					w.Header().Set("X-Cache", "HIT-IDEMPOTENT")
					w.WriteHeader(cached.StatusCode)
					_, _ = w.Write(cached.Body)
					return
				}
			}

			// 2. Acquire short-lived processing lock to prevent concurrent duplicate execution
			locked, lockErr := rdb.SetNX(ctx, lockKey, "processing", idempLockTTL).Result()
			if lockErr != nil && !errors.Is(lockErr, redis.Nil) {
				// Redis unavailable: proceed with fail-open safety or return error
				next.ServeHTTP(w, r)
				return
			}

			if !locked {
				// Another request with the same idempotency key is currently in-flight
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"CONCURRENT_REQUEST_IN_FLIGHT","message":"A request with this idempotency key is currently in progress. Please retry shortly."}`))
				return
			}

			defer func() {
				_ = rdb.Del(context.Background(), lockKey)
			}()

			// 3. Intercept and execute request
			recorder := &responseRecorder{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(recorder, r)

			// 4. Cache successful responses (2xx and 4xx except 5xx errors)
			if recorder.statusCode >= 200 && recorder.statusCode < 500 {
				cached := CachedResponse{
					StatusCode: recorder.statusCode,
					Headers: map[string]string{
						"Content-Type": w.Header().Get("Content-Type"),
					},
					Body: recorder.body.Bytes(),
				}

				if data, err := json.Marshal(cached); err == nil {
					_ = rdb.Set(context.Background(), respKey, data, idempResponseTTL)
				}
			}
		})
	}
}
