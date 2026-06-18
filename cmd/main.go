package main

import (
	"context"
	"kubelb/configs"
	"kubelb/internal/ippool"
	"kubelb/internal/lb"
	"kubelb/internal/lb/backend"
	"kubelb/internal/reconciler"
	"kubelb/pkg/k8s"
	"log"
	"log/slog"
	"os"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

func main() {
	c, err := configs.GetConfig()
	if err != nil {
		log.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Debug("config loaded", "config", c)

	ipPool := ippool.NewStatic([]string{c.LbIp})
	b := backend.NewNatBackend(logger.With("service", "natLB.backend"))
	lb := lb.NewLb(b, logger.With("service", "nat-lb"))

	clientSet, err := k8s.BuildClientset(c.KubeConfig)
	if err != nil {
		log.Fatal(err)
	}

	factory := informers.NewSharedInformerFactory(clientSet, 0)
	svcInformer := factory.Core().V1().Services().Informer()
	nodeInformer := factory.Core().V1().Nodes().Informer()

	_, err = reconciler.NewServiceReconciler(lb, ipPool, clientSet, svcInformer, nodeInformer, logger)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	factory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(),
		factory.Core().V1().Services().Informer().HasSynced,
		factory.Core().V1().Nodes().Informer().HasSynced,
	)

	time.Sleep(100 * time.Second)
	cancel()
	logger.Info("shutting down...")
}
