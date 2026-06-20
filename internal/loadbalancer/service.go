package loadbalancer

import (
	"context"
	"fmt"
	"kubelb/internal/health"
	"log/slog"
	"net/http"
	"time"

	v1 "k8s.io/api/core/v1"
)

const (
	defaultInterval         = time.Duration(1) * time.Second
	defaultSuccessThreshold = 1
	defaultFailureThreshold = 1
)

type service struct {
	nodes  map[string]*node
	svc    *v1.Service
	stopCh chan struct{}

	interval         time.Duration
	successThreshold int
	failureThreshold int

	healthChecker health.Checker
}

func newService(svc *v1.Service, nodes map[string]string) *service {
	nodeMap := make(map[string]*node)
	for ip := range nodes {
		nodeMap[ip] = &node{
			ip:                  ip,
			healthCheckNodePort: svc.Spec.HealthCheckNodePort,
		}
	}
	return &service{
		nodes:            nodeMap,
		svc:              svc,
		stopCh:           make(chan struct{}),
		interval:         defaultInterval,
		successThreshold: defaultSuccessThreshold,
		failureThreshold: defaultFailureThreshold,
		healthChecker:    health.NewHttpHeathChecker(http.StatusOK, make(map[string]string)),
	}
}

func (s *service) addNode(ip string) {
	if _, ok := s.nodes[ip]; !ok {
		s.nodes[ip] = &node{
			ip:                  ip,
			healthCheckNodePort: s.svc.Spec.HealthCheckNodePort,
		}
	}
}

type node struct {
	ip                  string
	healthCheckNodePort int32
	healthy             bool
	successCount        int
	failCount           int
}

func (s *service) healthCheck(ctx context.Context, n *node) error {
	domain := fmt.Sprintf("http://%s:%d", n.ip, n.healthCheckNodePort)
	err := s.healthChecker.Check(ctx, domain)
	if err != nil {
		n.failCount++
	} else {
		n.successCount++
	}

	if n.healthy && n.failCount > s.failureThreshold {
		n.healthy = false
		n.failCount = 0
		n.successCount = 0
		return err
	}

	if !n.healthy && n.successCount > s.successThreshold {
		n.healthy = true
		n.successCount = 0
		n.failCount = 0
	}

	return err
}

func getServiceLogAttr(s *v1.Service) slog.Attr {
	return slog.Group("service", "namespace", s.Namespace, "name", s.Name)
}

func getServiceName(s *v1.Service) string {
	return fmt.Sprintf("%s-%s", s.Namespace, s.Name)
}
