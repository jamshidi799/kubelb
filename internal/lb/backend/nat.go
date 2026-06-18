package backend

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

type nat struct {
	logger *slog.Logger
}

func NewNatBackend(logger *slog.Logger) Backend {
	return &nat{
		logger: logger,
	}
}

func (i *nat) Sync(req *Request) error {
	var b strings.Builder

	i.setTable(b)
	i.preRouting(b, req)
	i.postRouting(b, req)
	i.commit(b)

	rules := b.String()
	return i.runIptablesRestore(rules)
}

func (i *nat) setTable(b strings.Builder) {
	rule := "*nat\n"
	b.WriteString(rule)
}

func (i *nat) preRouting(b strings.Builder, req *Request) {
	chain := i.preRoutingChain(req)
	i.runCreateChain(chain)
	i.addChainToPreRouting(b, req, chain)
	i.flush(b, chain)
	i.dnat(b, req, chain)
}

func (i *nat) postRouting(b strings.Builder, req *Request) {
	chain := i.postRoutingChain(req)
	i.runCreateChain(chain)
	i.addChainToPostRouting(b, req, chain)
	i.flush(b, chain)
	i.snat(b, req, chain)
}

func (i *nat) commit(b strings.Builder) {
	rule := "COMMIT\n"
	b.WriteString(rule)
}

func (i *nat) preRoutingChain(req *Request) string {
	return fmt.Sprintf(preRoutingChainNameFormat, req.ServiceName)
}

func (i *nat) postRoutingChain(req *Request) string {
	return fmt.Sprintf(postRoutingChainNameFormat, req.ServiceName)
}

func (i *nat) flush(builder strings.Builder, chain string) {
	rule := fmt.Sprintf("-F %s\n", chain)
	builder.WriteString(rule)
}

func (i *nat) runCreateChain(chain string) {
	err := i.runIptables("-t", "nat", "-N", chain)
	if err != nil {
		i.logger.Debug(err.Error())
	}
}

func (i *nat) dnat(builder strings.Builder, req *Request, chain string) {
	count := len(req.Ips)
	for i, ip := range req.Ips {
		var dnat string
		if i == count-1 {
			dnat = fmt.Sprintf("-A %s -p %s --dport %d -j DNAT --to-destination %s:%d\n",
				chain, req.Protocol, req.Port, ip, req.NodePort)
		} else {
			prob := 1.0 / float64(count-i)
			dnat = fmt.Sprintf("-A %s -p %s --dport %d -m statistic --mode random --probability %.6f -j DNAT --to-destination %s:%d\n",
				chain, req.Protocol, req.Port, prob, ip, req.NodePort)
		}

		builder.WriteString(dnat)
	}
}

func (i *nat) addChainToPreRouting(builder strings.Builder, req *Request, chain string) {
	err := i.runIptables("-t", "nat", "-C", "PREROUTING", "-p", req.Protocol, "-d", req.LbIp, "--dport", strconv.Itoa(int(req.Port)), "-j", chain)
	if err != nil {
		rule := fmt.Sprintf("-A PREROUTING -p %s -d %s --dport %d -j %s\n", req.Protocol, req.LbIp, req.Port, chain)
		builder.WriteString(rule)
	}
}

func (i *nat) addChainToPostRouting(builder strings.Builder, req *Request, chain string) {
	err := i.runIptables("-t", "nat", "-C", "POSTROUTING", "-p", req.Protocol, "--dport", strconv.Itoa(int(req.NodePort)), "-j", chain)
	if err != nil {
		appendToPostRouting := fmt.Sprintf("-A POSTROUTING -p %s --dport %d -j %s\n", req.Protocol, req.NodePort, chain)
		builder.WriteString(appendToPostRouting)
	}
}

func (i *nat) snat(builder strings.Builder, req *Request, chain string) {
	rule := fmt.Sprintf("-A %s -p %s --dport %d -j SNAT --to-source %s\n", chain, req.Protocol, req.NodePort, req.LbIp)
	builder.WriteString(rule)
}

func (i *nat) runIptablesRestore(rules string) error {
	i.logger.Debug("applying nat-restore", "rules", rules)

	cmd := exec.Command("iptables-restore", "--noflush")
	cmd.Stdin = strings.NewReader(rules)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("iptables-restore failed: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}

func (i *nat) runIptables(rule ...string) error {
	cmd := exec.Command("iptables", rule...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("iptables failed: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}
