package health

import (
	"context"
	"fmt"
	"net/http"
)

type Checker interface {
	Check(ctx context.Context, addr string) error
}

type httpHeathChecker struct {
	client *http.Client

	expectedStatus int
	httpHeaders    map[string]string
}

func NewHttpHeathChecker(expectedStatus int, httpHeaders map[string]string) Checker {
	return &httpHeathChecker{
		client:         &http.Client{},
		expectedStatus: expectedStatus,
		httpHeaders:    httpHeaders,
	}
}

func (h httpHeathChecker) Check(ctx context.Context, addr string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", addr, nil)
	if err != nil {
		return err
	}

	for k, v := range h.httpHeaders {
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != h.expectedStatus {
		return fmt.Errorf("expected http status %d, got %d", h.expectedStatus, resp.StatusCode)
	}

	return nil
}
