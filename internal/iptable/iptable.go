package iptable

import (
	"bytes"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
)

const (
	preRoutingChainNameFormat  = "PRE-SVC-%s"
	postRoutingChainNameFormat = "POST-SVC-%s"
)

type Iptable interface {
	Sync(req *SyncRequest) error
}

type SyncRequest struct {
	Svc      string
	Ips      []string
	LbIp     string
	Protocol string
	Port     int
	NodePort int
}

type iptable struct {
	logger *slog.Logger
}

func NewIptable(logger *slog.Logger) Iptable {
	return &iptable{
		logger: logger,
	}
}

func (i *iptable) Sync(req *SyncRequest) error {
	var b strings.Builder

	b.WriteString("*nat\n")

	preRoutingChain := fmt.Sprintf(preRoutingChainNameFormat, req.Svc)
	err := i.runIptables("-t", "nat", "-N", preRoutingChain)
	if err != nil {
		i.logger.Warn(err.Error())
	}

	err = i.runIptables("-t", "nat", "-C", "PREROUTING", "-p", req.Protocol, "--dport", strconv.Itoa(req.Port), "-j", preRoutingChain)
	if err != nil {
		appendToPreRouting := fmt.Sprintf("-A PREROUTING -p %s --dport %d -j %s\n", req.Protocol, req.Port, preRoutingChain)
		b.WriteString(appendToPreRouting)
	}
	b.WriteString(i.flush(preRoutingChain))

	postRoutingChain := fmt.Sprintf(postRoutingChainNameFormat, req.Svc)
	err = i.runIptables("-t", "nat", "-N", postRoutingChain)
	if err != nil {
		i.logger.Warn(err.Error())
	}

	err = i.runIptables("-t", "nat", "-C", "POSTROUTING", "-p", req.Protocol, "--dport", strconv.Itoa(req.NodePort), "-j", postRoutingChain)
	if err != nil {
		appendToPostRouting := fmt.Sprintf("-A POSTROUTING -p %s --dport %d -j %s\n", req.Protocol, req.NodePort, postRoutingChain)
		b.WriteString(appendToPostRouting)
	}
	b.WriteString(i.flush(postRoutingChain))

	count := len(req.Ips)
	for i, ip := range req.Ips {
		var dnat string
		if i == count-1 {
			dnat = fmt.Sprintf("-A %s -p %s --dport %d -j DNAT --to-destination %s:%d\n",
				preRoutingChain, req.Protocol, req.Port, ip, req.NodePort)
		} else {
			prob := 1.0 / float64(count-i)
			dnat = fmt.Sprintf("-A %s -p %s --dport %d -m statistic --mode random --probability %.6f -j DNAT --to-destination %s:%d\n",
				preRoutingChain, req.Protocol, req.Port, prob, ip, req.NodePort)
		}

		b.WriteString(dnat)
	}

	snat := fmt.Sprintf("-A %s -p %s --dport %d -j SNAT --to-source %s\n", postRoutingChain, req.Protocol, req.NodePort, req.LbIp)
	b.WriteString(snat)

	b.WriteString("COMMIT\n")

	rules := b.String()
	return i.runIptablesRestore(rules)
}

func (i *iptable) runIptablesRestore(rules string) error {
	i.logger.Debug("applying iptable-restore", "rules", rules)

	cmd := exec.Command("iptables-restore", "--noflush")
	cmd.Stdin = strings.NewReader(rules)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("iptables-restore failed: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}

func (i *iptable) runIptables(rule ...string) error {
	cmd := exec.Command("iptables", rule...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("iptables failed: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}

func (i *iptable) flush(chain string) string {
	return fmt.Sprintf("-F %s\n", chain)
}
