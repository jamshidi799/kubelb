package main

import (
	"kubelb/configs"
	"kubelb/internal/lb/general"
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

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	for _, svc := range c.GeneralLB.Services {
		general.NewGeneralLb(c.LbIp, c.GeneralLB.Protocol, c.GeneralLB.NodesIp, &svc, logger.WithGroup(svc.Name))
	}

	ch := time.After(100 * time.Second)
	<-ch
}
