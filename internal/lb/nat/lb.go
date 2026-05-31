package nat

import (
	"context"
	"kubelb/configs"
	"log/slog"
	"sync"
	"time"
)

type Lb struct {
	logger *slog.Logger

	loadBalancerIp string
	nodes          []*node

	svcName  string
	protocol string
	port     int
	nodePort int

	interval         time.Duration
	successThreshold int
	failureThreshold int

	healthChecker healthChecker
	iptable       iptableManager
}

type node struct {
	ip           string
	healthy      bool
	successCount int
	failCount    int
}

func NewNatLb(loadBalancerIp string, protocol string, nodesIp []string, svc *configs.Service, logger *slog.Logger) *Lb {
	lb := &Lb{
		logger:           logger,
		loadBalancerIp:   loadBalancerIp,
		nodes:            make([]*node, 0, len(nodesIp)),
		protocol:         protocol,
		svcName:          svc.Name,
		port:             svc.LbPort,
		nodePort:         svc.NodePort,
		interval:         svc.HealthCheck.Interval,
		successThreshold: svc.HealthCheck.SuccessThreshold,
		failureThreshold: svc.HealthCheck.FailureThreshold,

		healthChecker: newHttpHeathChecker(svc.HealthCheck.Port, svc.HealthCheck.Path, svc.HealthCheck.ExpectedStatus, svc.HealthCheck.HttpHeaders),
		iptable:       newIptableManager(logger.With("service", "general-lb.iptableImpl")),
	}

	for _, b := range nodesIp {
		lb.nodes = append(lb.nodes, &node{
			ip:      b,
			healthy: false,
		})
	}

	go lb.loop()

	return lb
}

func (lb *Lb) loop() {
	ticker := time.NewTicker(lb.interval)
	defer ticker.Stop()

	for range ticker.C {
		lb.logger.Info("new synchronization is starting...")

		var wg sync.WaitGroup
		ctx, cancel := context.WithTimeout(context.Background(), lb.interval)

		for _, node := range lb.nodes {
			wg.Add(1)
			go lb.checkNode(ctx, node, &wg)
		}

		wg.Wait()
		cancel()
		lb.logger.Info("synchronization completed")
	}
}

func (lb *Lb) checkNode(ctx context.Context, n *node, wg *sync.WaitGroup) {
	defer wg.Done()
	status := n.healthy
	lb.healthCheck(ctx, n)
	if status != n.healthy {
		lb.sync()
	}
}

func (lb *Lb) healthCheck(ctx context.Context, n *node) {
	lb.logger.Info("checking node", "ip", n.ip)

	err := lb.healthChecker.check(ctx, n.ip)
	if err != nil {
		lb.logger.Error("Health Check Error", "node", n.ip, "err", err)
		n.failCount++
	} else {
		n.successCount++
	}

	if n.healthy && n.failCount > lb.failureThreshold {
		n.healthy = false
		n.failCount = 0
		n.successCount = 0
		return
	}

	if !n.healthy && n.successCount > lb.successThreshold {
		n.healthy = true
		n.successCount = 0
		n.failCount = 0
	}
}

func (lb *Lb) sync() {
	ips := make([]string, 0, len(lb.nodes))
	for _, n := range lb.nodes {
		if n.healthy {
			ips = append(ips, n.ip)
		}
	}

	if len(ips) == 0 {
		lb.logger.Info("no healthy nodes found")
		return
	}

	lb.logger.Info("applying nodes", "ips", ips)
	err := lb.iptable.sync(&syncRequest{
		svc:      lb.svcName,
		ips:      ips,
		lbIp:     lb.loadBalancerIp,
		protocol: lb.protocol,
		port:     lb.port,
		nodePort: lb.nodePort,
	})

	if err != nil {
		lb.logger.Warn("Failed to apply nodes", "err", err)
	}
}
