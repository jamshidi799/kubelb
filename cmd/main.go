package main

import (
	"kubelb/configs"
	"kubelb/internal/ippool"
	"kubelb/internal/loadbalancer/nat"
	"kubelb/internal/reconciler"
	"log"
	"log/slog"
	"os"
	"time"
)

func main() {
	c, err := configs.GetConfig()
	if err != nil {
		log.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Debug("config loaded", "config", c)

	ipPool := ippool.NewStatic([]string{c.LbIp})
	lb := nat.NewNatLb(logger.With("service", "nat-lb"))

	r, err := reconciler.NewServiceReconciler(lb, ipPool, c.KubeConfig, logger)
	if err != nil {
		log.Fatal(err)
	}

	ch := make(chan struct{}, 1)

	err = r.Reconcile(ch)
	if err != nil {
		log.Fatal(err)
	}

	time.Sleep(100 * time.Second)
	ch <- struct{}{}

	logger.Info("shutting down...")
}
