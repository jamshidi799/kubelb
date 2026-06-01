package main

import (
	"kubelb/configs"
	"kubelb/internal/lb/nat"
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
	log.Printf("config: %+v\n", c)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	for _, svc := range c.GeneralLB.Services {
		nat.NewNatLb(c.LbIp, c.GeneralLB.Protocol, c.GeneralLB.NodesIp, &svc, logger.With("service", svc.Name))
	}

	ch := time.After(100 * time.Second)
	<-ch
}
