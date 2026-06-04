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
	sync(req *request) error
}

type request struct {
	serviceName string
	ips         []string
	lbIp        string
	protocol    string
	port        int32
	nodePort    int32
}

type iptableImpl struct {
	logger *slog.Logger
}

func newIptableManager(logger *slog.Logger) iptableManager {
	return &iptableImpl{
		logger: logger,
	}
}

func (i *iptableImpl) sync(req *request) error {
	var b strings.Builder

	i.setTable(b)
	i.preRouting(b, req)
	i.postRouting(b, req)
	i.commit(b)

	rules := b.String()
	return i.runIptablesRestore(rules)
}

func (i *iptableImpl) setTable(b strings.Builder) {
	rule := "*nat\n"
	b.WriteString(rule)
}

func (i *iptableImpl) preRouting(b strings.Builder, req *request) {
	chain := i.preRoutingChain(req)
	i.runCreateChain(chain)
	i.addChainToPreRouting(b, req, chain)
	i.flush(b, chain)
	i.dnat(b, req, chain)
}

func (i *iptableImpl) postRouting(b strings.Builder, req *request) {
	chain := i.postRoutingChain(req)
	i.runCreateChain(chain)
	i.addChainToPostRouting(b, req, chain)
	i.flush(b, chain)
	i.snat(b, req, chain)
}

func (i *iptableImpl) commit(b strings.Builder) {
	rule := "COMMIT\n"
	b.WriteString(rule)
}

func (i *iptableImpl) preRoutingChain(req *request) string {
	return fmt.Sprintf(preRoutingChainNameFormat, req.serviceName)
}

func (i *iptableImpl) postRoutingChain(req *request) string {
	return fmt.Sprintf(postRoutingChainNameFormat, req.serviceName)
}

func (i *iptableImpl) flush(builder strings.Builder, chain string) {
	rule := fmt.Sprintf("-F %s\n", chain)
	builder.WriteString(rule)
}

func (i *iptableImpl) runCreateChain(chain string) {
	err := i.runIptables("-t", "nat", "-N", chain)
	if err != nil {
		i.logger.Debug(err.Error())
	}
}

func (i *iptableImpl) dnat(builder strings.Builder, req *request, chain string) {
	count := len(req.ips)
	for i, ip := range req.ips {
		var dnat string
		if i == count-1 {
			dnat = fmt.Sprintf("-A %s -p %s --dport %d -j DNAT --to-destination %s:%d\n",
				chain, req.protocol, req.port, ip, req.nodePort)
		} else {
			prob := 1.0 / float64(count-i)
			dnat = fmt.Sprintf("-A %s -p %s --dport %d -m statistic --mode random --probability %.6f -j DNAT --to-destination %s:%d\n",
				chain, req.protocol, req.port, prob, ip, req.nodePort)
		}

		builder.WriteString(dnat)
	}
}

func (i *iptableImpl) addChainToPreRouting(builder strings.Builder, req *request, chain string) {
	err := i.runIptables("-t", "nat", "-C", "PREROUTING", "-p", req.protocol, "-d", req.lbIp, "--dport", strconv.Itoa(int(req.port)), "-j", chain)
	if err != nil {
		rule := fmt.Sprintf("-A PREROUTING -p %s -d %s --dport %d -j %s\n", req.protocol, req.lbIp, req.port, chain)
		builder.WriteString(rule)
	}
}

func (i *iptableImpl) addChainToPostRouting(builder strings.Builder, req *request, chain string) {
	err := i.runIptables("-t", "nat", "-C", "POSTROUTING", "-p", req.protocol, "--dport", strconv.Itoa(int(req.nodePort)), "-j", chain)
	if err != nil {
		appendToPostRouting := fmt.Sprintf("-A POSTROUTING -p %s --dport %d -j %s\n", req.protocol, req.nodePort, chain)
		builder.WriteString(appendToPostRouting)
	}
}

func (i *iptableImpl) snat(builder strings.Builder, req *request, chain string) {
	rule := fmt.Sprintf("-A %s -p %s --dport %d -j SNAT --to-source %s\n", chain, req.protocol, req.nodePort, req.lbIp)
	builder.WriteString(rule)
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
