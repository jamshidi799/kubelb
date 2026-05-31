package nat

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

type iptableManager interface {
	sync(req *syncRequest) error
}

type syncRequest struct {
	svc      string
	ips      []string
	lbIp     string
	protocol string
	port     int
	nodePort int
}

type iptableImpl struct {
	logger *slog.Logger
}

func newIptableManager(logger *slog.Logger) iptableManager {
	return &iptableImpl{
		logger: logger,
	}
}

func (i *iptableImpl) sync(req *syncRequest) error {
	var b strings.Builder

	b.WriteString("*nat\n")

	preRoutingChain := fmt.Sprintf(preRoutingChainNameFormat, req.svc)
	err := i.runIptables("-t", "nat", "-N", preRoutingChain)
	if err != nil {
		i.logger.Warn(err.Error())
	}

	err = i.runIptables("-t", "nat", "-C", "PREROUTING", "-p", req.protocol, "--dport", strconv.Itoa(req.port), "-j", preRoutingChain)
	if err != nil {
		appendToPreRouting := fmt.Sprintf("-A PREROUTING -p %s --dport %d -j %s\n", req.protocol, req.port, preRoutingChain)
		b.WriteString(appendToPreRouting)
	}
	b.WriteString(i.flush(preRoutingChain))

	postRoutingChain := fmt.Sprintf(postRoutingChainNameFormat, req.svc)
	err = i.runIptables("-t", "nat", "-N", postRoutingChain)
	if err != nil {
		i.logger.Warn(err.Error())
	}

	err = i.runIptables("-t", "nat", "-C", "POSTROUTING", "-p", req.protocol, "--dport", strconv.Itoa(req.nodePort), "-j", postRoutingChain)
	if err != nil {
		appendToPostRouting := fmt.Sprintf("-A POSTROUTING -p %s --dport %d -j %s\n", req.protocol, req.nodePort, postRoutingChain)
		b.WriteString(appendToPostRouting)
	}
	b.WriteString(i.flush(postRoutingChain))

	count := len(req.ips)
	for i, ip := range req.ips {
		var dnat string
		if i == count-1 {
			dnat = fmt.Sprintf("-A %s -p %s --dport %d -j DNAT --to-destination %s:%d\n",
				preRoutingChain, req.protocol, req.port, ip, req.nodePort)
		} else {
			prob := 1.0 / float64(count-i)
			dnat = fmt.Sprintf("-A %s -p %s --dport %d -m statistic --mode random --probability %.6f -j DNAT --to-destination %s:%d\n",
				preRoutingChain, req.protocol, req.port, prob, ip, req.nodePort)
		}

		b.WriteString(dnat)
	}

	snat := fmt.Sprintf("-A %s -p %s --dport %d -j SNAT --to-source %s\n", postRoutingChain, req.protocol, req.nodePort, req.lbIp)
	b.WriteString(snat)

	b.WriteString("COMMIT\n")

	rules := b.String()
	return i.runIptablesRestore(rules)
}

func (i *iptableImpl) runIptablesRestore(rules string) error {
	i.logger.Debug("applying iptableImpl-restore", "rules", rules)

	cmd := exec.Command("iptables-restore", "--noflush")
	cmd.Stdin = strings.NewReader(rules)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("iptables-restore failed: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}

func (i *iptableImpl) runIptables(rule ...string) error {
	cmd := exec.Command("iptables", rule...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("iptables failed: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}

func (i *iptableImpl) flush(chain string) string {
	return fmt.Sprintf("-F %s\n", chain)
}
