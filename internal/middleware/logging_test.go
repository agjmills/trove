package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoggingMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		handler        http.HandlerFunc
		expectedStatus int
	}{
		{
			name:   "GET request",
			method: http.MethodGet,
			path:   "/dashboard",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:   "POST request",
			method: http.MethodPost,
			path:   "/upload",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte("Created"))
			},
			expectedStatus: http.StatusCreated,
		},
		{
			name:   "error response",
			method: http.MethodGet,
			path:   "/nonexistent",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("Not Found"))
			},
			expectedStatus: http.StatusNotFound,
		},
		{
			name:   "internal error",
			method: http.MethodGet,
			path:   "/error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Internal Server Error"))
			},
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("User-Agent", "test-agent")
			rec := httptest.NewRecorder()

			// Wrap handler with logging middleware
			wrappedHandler := LoggingMiddleware(tt.handler)
			wrappedHandler.ServeHTTP(rec, req)

			// Check status code
			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			// Verify handler was called
			if rec.Body.Len() == 0 {
				t.Error("Handler did not write response")
			}
		})
	}
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: rec,
		statusCode:     0,
	}

	// Write custom status code
	rw.WriteHeader(http.StatusCreated)

	if rw.statusCode != http.StatusCreated {
		t.Errorf("Expected statusCode %d, got %d", http.StatusCreated, rw.statusCode)
	}

	if rec.Code != http.StatusCreated {
		t.Errorf("Expected underlying recorder code %d, got %d", http.StatusCreated, rec.Code)
	}
}

func TestResponseWriter_Write(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: rec,
		statusCode:     http.StatusOK,
		written:        0,
	}

	data := []byte("Hello, World!")
	n, err := rw.Write(data)

	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}

	if rw.written != int64(len(data)) {
		t.Errorf("Expected written count %d, got %d", len(data), rw.written)
	}

	if rec.Body.String() != string(data) {
		t.Errorf("Expected body %q, got %q", string(data), rec.Body.String())
	}
}

func TestResponseWriter_WriteMultipleTimes(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: rec,
		statusCode:     http.StatusOK,
		written:        0,
	}

	// Write multiple times
	data1 := []byte("Hello, ")
	data2 := []byte("World!")

	n1, err1 := rw.Write(data1)
	if err1 != nil {
		t.Fatalf("First write failed: %v", err1)
	}

	n2, err2 := rw.Write(data2)
	if err2 != nil {
		t.Fatalf("Second write failed: %v", err2)
	}

	totalWritten := n1 + n2
	expectedTotal := len(data1) + len(data2)

	if totalWritten != expectedTotal {
		t.Errorf("Expected total write %d bytes, wrote %d", expectedTotal, totalWritten)
	}

	if rw.written != int64(expectedTotal) {
		t.Errorf("Expected written count %d, got %d", expectedTotal, rw.written)
	}
}

func TestResponseWriter_Flush(t *testing.T) {
	// Test with a flusher
	rec := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: rec,
		statusCode:     http.StatusOK,
	}

	// This should not panic even if the underlying writer supports Flush
	rw.Flush()

	// Write some data and flush again
	rw.Write([]byte("test"))
	rw.Flush()
}

func TestLoggingMiddleware_CapturesMetrics(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate some processing time
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrappedHandler := LoggingMiddleware(handler)
	start := time.Now()
	wrappedHandler.ServeHTTP(rec, req)
	duration := time.Since(start)

	// Ensure request took at least the sleep time
	if duration < 10*time.Millisecond {
		t.Error("Request completed faster than expected sleep time")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestLoggingMiddleware_BytesWritten(t *testing.T) {
	testData := "This is a test response with some content"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testData))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrappedHandler := LoggingMiddleware(handler)
	wrappedHandler.ServeHTTP(rec, req)

	if rec.Body.Len() != len(testData) {
		t.Errorf("Expected %d bytes written, got %d", len(testData), rec.Body.Len())
	}
}

func TestLoggingMiddleware_RequestID(t *testing.T) {
	// Test that logging middleware works with chi request ID middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrappedHandler := LoggingMiddleware(handler)
	wrappedHandler.ServeHTTP(rec, req)

	// Should not panic even without request ID
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestLoggingMiddleware_PreservesUserAgent(t *testing.T) {
	userAgent := "Mozilla/5.0 (Test Browser)"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != userAgent {
			t.Errorf("User agent not preserved: expected %q, got %q", userAgent, r.UserAgent())
		}
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("User-Agent", userAgent)
	rec := httptest.NewRecorder()

	wrappedHandler := LoggingMiddleware(handler)
	wrappedHandler.ServeHTTP(rec, req)
}

func TestLoggingMiddleware_LargeResponse(t *testing.T) {
	// Test with a large response body
	largeData := strings.Repeat("x", 10000)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeData))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrappedHandler := LoggingMiddleware(handler)
	wrappedHandler.ServeHTTP(rec, req)

	if rec.Body.Len() != len(largeData) {
		t.Errorf("Expected %d bytes, got %d", len(largeData), rec.Body.Len())
	}
}
