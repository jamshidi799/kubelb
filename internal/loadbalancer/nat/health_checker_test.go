package nat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestHttpChecker_check(t *testing.T) {
	expectedCode := http.StatusOK
	headers := map[string]string{
		"content-type": "application/json",
	}
	var addr *url.URL

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if addr.Host != r.Host {
			t.Errorf("got %s, want %s", r.Host, addr.Host)
		}
		for k, v := range headers {
			if r.Header.Get(k) != v {
				t.Error("Expected", k, "got", r.Header.Get(k))
			}
		}

		w.WriteHeader(expectedCode)
	}))
	defer server.Close()

	checker := httpHeathChecker{
		client:         server.Client(),
		expectedStatus: expectedCode,
		httpHeaders:    headers,
	}
	addr, _ = url.Parse(server.URL)

	err := checker.check(context.Background(), server.URL)
	if err != nil {
		t.Error("Expected", nil, "got", err)
	}

}

func TestHttpChecker_check_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	checker := httpHeathChecker{
		client:         server.Client(),
		expectedStatus: http.StatusOK,
		httpHeaders:    map[string]string{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := checker.check(ctx, server.URL)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("Expected", nil, "got", err)
	}
}
