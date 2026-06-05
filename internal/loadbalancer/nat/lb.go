package nat

import (
	"context"
	"fmt"
	"kubelb/internal/loadbalancer"
	"log/slog"
	"sync"
	"time"

	"k8s.io/api/core/v1"
)

type natLB struct {
	services map[string]*service
	nodes    map[string]string
	iptable  iptableManager
	logger   *slog.Logger
}

func NewNatLb(logger *slog.Logger) loadbalancer.LoadBalancer {
	lb := &natLB{
		logger:   logger,
		services: make(map[string]*service),
		nodes:    make(map[string]string),
		iptable:  newIptableManager(logger.With("service", "natLB.iptable")),
	}

	return lb
}

func (lb *natLB) Add(svc *v1.Service) {
	s := newService(svc, lb.nodes)
	lb.logger.Info("Adding service", getServiceLogAttr(svc))
	lb.services[getServiceName(svc)] = s
	go lb.syncService(s)
}

func (lb *natLB) Update(svc *v1.Service) {
	s, ok := lb.services[getServiceName(svc)]
	if !ok {
		lb.logger.Warn("service not found. adding it.", getServiceLogAttr(svc))
		lb.Add(svc)
		return
	}

	s.svc = svc
}

func (lb *natLB) Delete(svc *v1.Service) {
	serviceName := getServiceName(svc)
	s, ok := lb.services[serviceName]
	if !ok {
		lb.logger.Warn("invalid delete request. service not found.", getServiceLogAttr(svc))
	}
	delete(lb.services, serviceName)
	s.stopCh <- struct{}{}
}

func (lb *natLB) AddNode(ip string) {
	lb.logger.Info("adding node to lb", "ip", ip)
	lb.nodes[ip] = ip
	for _, svc := range lb.services {
		svc.addNode(ip)
	}
}

func (lb *natLB) DeleteNode(ip string) {
	lb.logger.Info("deleting node", "ip", ip)
	delete(lb.nodes, ip)
	for _, svc := range lb.services {
		delete(svc.nodes, ip)
	}
}

func (lb *natLB) syncService(service *service) {
	ticker := time.NewTicker(service.interval)
	defer ticker.Stop()

	for range ticker.C {
		lb.logger.Debug("synchronization is starting", getServiceLogAttr(service.svc))

		var wg sync.WaitGroup
		ctx, cancel := context.WithTimeout(context.Background(), service.interval)

		for _, node := range service.nodes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				lb.checkNodePort(ctx, service, node)
			}()
		}

		wg.Wait()
		cancel()
		lb.logger.Debug("synchronization completed", getServiceLogAttr(service.svc))

		select {
		case <-service.stopCh:
			lb.logger.Info("stopping sync loop", getServiceLogAttr(service.svc))
			break
		default:

		}
	}
}

func (lb *natLB) checkNodePort(ctx context.Context, svc *service, n *node) {
	lb.logger.Debug("checking node", "node", n.ip, "port", n.HealthCheckNodePort)
	status := n.healthy
	err := svc.healthCheck(ctx, n)
	if err != nil {
		lb.logger.Debug("health check failed", "node", n.ip, "port", n.HealthCheckNodePort)
	}
	if status != n.healthy {
		lb.sync(svc)
	}
}

func (lb *natLB) sync(service *service) {
	ips := make([]string, 0, len(service.nodes))
	for _, n := range service.nodes {
		if n.healthy {
			ips = append(ips, n.ip)
		}
	}

	if len(ips) == 0 {
		lb.logger.Debug("no healthy nodes found", getServiceLogAttr(service.svc))
		return
	}

	for _, port := range service.svc.Spec.Ports {
		lb.logger.Debug("applying nodes", getServiceLogAttr(service.svc), "port", port.Name)

		err := lb.iptable.sync(&request{
			serviceName: fmt.Sprintf("%s-%s-%d", service.svc.Namespace, service.svc.Name, port.Port),
			ips:         ips,
			lbIp:        service.svc.Status.LoadBalancer.Ingress[0].IP,
			protocol:    string(port.Protocol),
			port:        port.Port,
			nodePort:    port.NodePort,
		})

		if err != nil {
			lb.logger.Warn("Failed to apply nodes", "err", err, getServiceLogAttr(service.svc), "port", port.Name)
		}
	}

}
