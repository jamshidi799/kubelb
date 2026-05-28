package general

import "context"

type healthChecker interface {
	check(ctx context.Context, ip string) error
}

type httpHeathChecker struct {
	path           string
	expectedStatus int
	httpHeaders    map[string]string
}

func newHttpHeathChecker(path string, expectedStatus int, httpHeaders map[string]string) healthChecker {
	return &httpHeathChecker{
		path,
		expectedStatus,
		httpHeaders,
	}
}

func (h httpHeathChecker) check(ctx context.Context, ip string) error {
	return nil
}
