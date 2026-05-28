package general

import (
	"context"
	"fmt"
	"kubelb/configs"
	"kubelb/internal/iptable"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

type Lb struct {
	logger *slog.Logger

	loadBalancerIp string
	nodes          []*node

	protocol string
	port     int
	nodePort int

	interval         time.Duration
	successThreshold int
	failureThreshold int

	healthChecker healthChecker
	iptable       iptable.Iptable
}

type node struct {
	ip           string
	healthy      bool
	successCount int
	failCount    int
}

func NewGeneralLb(loadBalancerIp string, protocol string, nodesIp []string, svc *configs.Service, logger *slog.Logger) *Lb {
	lb := &Lb{
		logger:           logger,
		loadBalancerIp:   loadBalancerIp,
		nodes:            make([]*node, 0, len(nodesIp)),
		protocol:         protocol,
		port:             svc.LbPort,
		nodePort:         svc.NodePort,
		interval:         svc.HealthCheck.Interval,
		successThreshold: svc.HealthCheck.SuccessThreshold,
		failureThreshold: svc.HealthCheck.FailureThreshold,

		healthChecker: newHttpHeathChecker(svc.HealthCheck.Path, svc.HealthCheck.ExpectedStatus, svc.HealthCheck.HttpHeaders),
		iptable:       iptable.NewIptable(),
	}

	for _, b := range nodesIp {
		lb.nodes = append(lb.nodes, &node{
			ip:      b,
			healthy: true, // todo: set it to false
		})
	}

	go lb.loop()

	return lb
}

func (lb *Lb) loop() {
	lb.logger.Info("iteration is starting...")

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), lb.interval)
	defer cancel()

	for _, node := range lb.nodes {
		wg.Add(1)
		go lb.checkNode(ctx, node, &wg)
	}
	wg.Wait()
	lb.logger.Info("iteration completed")
}

func (lb *Lb) checkNode(ctx context.Context, n *node, wg *sync.WaitGroup) {
	defer wg.Done()

	lb.healthCheck(ctx, n)

	if n.healthy {
		err := lb.addNode(n.ip)
		if err != nil {
			lb.logger.Error(err.Error(), "node", n.ip)
		}
	} else {
		err := lb.deleteRule(n.ip)
		if err != nil {
			lb.logger.Error(err.Error(), "node", n.ip)
		}
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
	} else if !n.healthy && n.successCount > lb.successThreshold {
		n.healthy = true
		n.successCount = 0
		n.failCount = 0
	}
}

func (lb *Lb) addNode(ip string) error {
	destination := fmt.Sprintf("%s:%d", ip, lb.nodePort)
	err := lb.iptable.Exec(
		"-t", "nat",
		"-I", "PREROUTING",
		"-p", lb.protocol,
		"--dport", strconv.Itoa(lb.port),
		"-j", "DNAT",
		"--to-destination", destination)

	if err != nil {
		return err
	}

	err = lb.iptable.Exec(
		"-t", "nat",
		"-I", "POSTROUTING",
		"-p", lb.protocol,
		"--dport", strconv.Itoa(lb.nodePort),
		"-j", "SNAT",
		"--to-source", lb.loadBalancerIp)

	lb.logger.Info("add node", "node", ip)
	return err
}

func (lb *Lb) deleteRule(ip string) error {
	return nil
}
