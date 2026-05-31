package nat

import (
	"context"
	"fmt"
	"net/http"
)

type healthChecker interface {
	check(ctx context.Context, ip string) error
}

type httpHeathChecker struct {
	port           int
	path           string
	expectedStatus int
	httpHeaders    map[string]string
}

func newHttpHeathChecker(port int, path string, expectedStatus int, httpHeaders map[string]string) healthChecker {
	return &httpHeathChecker{
		port,
		path,
		expectedStatus,
		httpHeaders,
	}
}

func (h httpHeathChecker) check(ctx context.Context, ip string) error {
	url := fmt.Sprintf("http://%s:%d%s", ip, h.port, h.path)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	for k, v := range h.httpHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != h.expectedStatus {
		return fmt.Errorf("expected http status %d, got %d", h.expectedStatus, resp.StatusCode)
	}

	return nil
}
