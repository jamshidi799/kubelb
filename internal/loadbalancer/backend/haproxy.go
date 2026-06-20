package backend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	client_native "github.com/haproxytech/client-native/v6"
	"github.com/haproxytech/client-native/v6/configuration"
	cfg_opt "github.com/haproxytech/client-native/v6/configuration/options"
	"github.com/haproxytech/client-native/v6/models"
	"github.com/haproxytech/client-native/v6/options"
	"github.com/haproxytech/client-native/v6/runtime"
	runtime_options "github.com/haproxytech/client-native/v6/runtime/options"
)

const (
	frontendParentType = "frontend"
	backendParentType  = "backend"
)

type haproxy struct {
	client client_native.HAProxyClient
	mu     sync.Mutex
	logger *slog.Logger
}

func NewHaproxyBackend(logger *slog.Logger) Backend {
	client, err := newClient()
	if err != nil {
		logger.Error(err.Error())
		return nil
	}
	logger.Info("creating haproxy backend")
	h := &haproxy{
		client: client,
		logger: logger,
	}
	//if err := h.test(); err != nil {
	//	logger.Error(err.Error())
	//}
	return h
}

func (h *haproxy) Sync(req *Request) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.logger.Info("start syncing request", "service", req.ServiceName, "ips", req.Ips)
	conf, err := h.client.Configuration()
	if err != nil {
		return err
	}

	shouldCommit := false
	txId, err := h.startTransaction(conf)

	_, _, err = conf.GetFrontend(getFrontendName(req.ServiceName), "")
	if err != nil {
		// create
		if !errors.Is(err, configuration.ErrObjectDoesNotExist) {
			return err
		}
		h.logger.Debug("create triggered", "service", req.ServiceName)
		err = h.create(req, conf, txId)
		if err != nil {
			err = conf.DeleteTransaction(txId)
			return err
		}
		shouldCommit = true
	} else {
		// sync
		h.logger.Debug("sync triggered", "service", req.ServiceName)
		if h.shouldSyncBind(req, conf) {
			if err = h.syncBind(req, conf, txId); err != nil {
				return err
			}
			shouldCommit = true
		}

		if h.shouldSyncServers(req, conf, txId) {
			if err = h.syncServers(req, conf, txId); err != nil {
				return err
			}
			shouldCommit = true
		}
	}

	if shouldCommit {
		tx, err := conf.CommitTransaction(txId)
		if err != nil {
			h.logger.Error(err.Error())
			return err
		}
		h.logger.Info("committed", "service", req.ServiceName, "ips", req.Ips, "tx", tx.Status)
		if err = h.reload(); err != nil {
			return err
		}
	}

	return nil
}

func (h *haproxy) create(req *Request, conf configuration.Configuration, txId string) error {
	frontend := getFrontendName(req.ServiceName)
	backend := getBackendName(req.ServiceName)

	err := conf.CreateFrontend(&models.Frontend{
		FrontendBase: models.FrontendBase{
			Name:           frontend,
			Mode:           strings.ToLower(req.Protocol),
			DefaultBackend: backend,
		},
	}, txId, 0)
	if err != nil {
		return err
	}

	err = h.createBind(req, conf, txId)

	balance := models.BalanceAlgorithmRoundrobin
	err = conf.CreateBackend(&models.Backend{
		BackendBase: models.BackendBase{
			Name: backend,
			Mode: strings.ToLower(req.Protocol),
			Balance: &models.Balance{
				Algorithm: &balance,
			},
		},
	}, txId, 0)

	err = h.syncServers(req, conf, txId)

	return err
}

//func (h *haproxy) test() error {
//	conf, err := h.client.Configuration()
//	if err != nil {
//		return err
//	}
//	_, frontends, err := conf.GetFrontends("")
//	if err != nil {
//		return err
//	}
//
//	for _, fr := range frontends {
//		h.logger.Info("frontend", "name", fr.Name, "backend", fr.DefaultBackend, "binds", fr.Binds, "mode", fr.Mode)
//		bind, err := h.getBind(conf, "http_front")
//		if err != nil {
//			h.logger.Error(err.Error())
//		}
//		h.logger.Info("bind", "frontend", "http_front", "name", bind.Nice, "address", bind.Address, "port", bind.Port)
//	}
//
//	return nil
//}

func (h *haproxy) getBind(conf configuration.Configuration, frontName string) (*models.Bind, error) {
	_, binds, err := conf.GetBinds(frontendParentType, frontName, "")
	if err != nil {
		return nil, err
	}
	if len(binds) == 0 {
		return nil, errors.New(fmt.Sprintf("invalid bind count: %d", len(binds)))
	}
	return binds[0], nil

}

func (h *haproxy) startTransaction(conf configuration.Configuration) (string, error) {
	version, err := conf.GetVersion("")
	if err != nil {
		return "", err
	}
	txID, err := conf.StartTransaction(version)
	if err != nil {
		return "", err
	}
	return txID.ID, nil
}

func (h *haproxy) shouldSyncBind(req *Request, conf configuration.Configuration) bool {
	frontendName := getFrontendName(req.ServiceName)
	bind, err := h.getBind(conf, frontendName)
	if err != nil {
		h.logger.Error("error getting bind", "err", err.Error())
		return true
	}

	if bind.Address != req.LbIp || *bind.Port != int64(req.Port) {
		return true
	}

	return false
}

func (h *haproxy) syncBind(req *Request, conf configuration.Configuration, txId string) error {
	frontend := getFrontendName(req.ServiceName)
	bind, err := h.getBind(conf, frontend)
	if err != nil {
		return h.createBind(req, conf, txId)
	}

	return h.editBind(bind, req, conf, txId)
}

func (h *haproxy) createBind(req *Request, conf configuration.Configuration, txId string) error {
	frontend := getFrontendName(req.ServiceName)
	port := int64(req.Port)
	err := conf.CreateBind(frontendParentType, frontend, &models.Bind{
		Address: req.LbIp,
		Port:    &port,
	}, txId, 0)
	if err != nil {
		return err
	}

	return nil
}

func (h *haproxy) editBind(bind *models.Bind, req *Request, conf configuration.Configuration, txId string) error {
	bind.Address = req.LbIp
	bind.Port = models.Ptr(int64(req.Port))
	err := conf.EditBind(bind.Name, frontendParentType, getFrontendName(req.ServiceName), bind, txId, 0)
	return err
}

func (h *haproxy) shouldSyncServers(req *Request, conf configuration.Configuration, txId string) bool {
	backend := getBackendName(req.ServiceName)
	_, servers, err := conf.GetServers(backendParentType, backend, txId)
	if err != nil {
		h.logger.Error("error getting servers", "err", err.Error())
		return true
	}
	if len(servers) != len(req.Ips) {
		return true
	}

	ips := map[string]struct{}{}
	for _, ip := range req.Ips {
		ips[ip] = struct{}{}
	}

	for _, server := range servers {
		if _, ok := ips[server.Address]; !ok {
			return true
		}

		if *(server.Port) != int64(req.NodePort) {
			return true
		}
	}

	return false
}

func (h *haproxy) syncServers(req *Request, conf configuration.Configuration, txId string) error {
	backend := getBackendName(req.ServiceName)
	_, servers, err := conf.GetServers(backendParentType, backend, txId)
	if err != nil {
		h.logger.Error("error getting servers", "err", err.Error())
		return err
	}

	serverMap := map[string]*models.Server{}
	for _, server := range servers {
		serverMap[server.Address] = server
	}

	for _, ip := range req.Ips {
		err = conf.CreateOrEditServer(backendParentType, backend, &models.Server{
			ServerParams: models.ServerParams{
				SendProxy: "enabled",
			},
			Address: ip,
			Name:    fmt.Sprintf("node-%s", ip),
			Port:    models.Ptr(int64(req.NodePort)),
		}, txId, 0)
		if err != nil {
			return err
		}
		delete(serverMap, ip)
	}

	for _, server := range serverMap {
		err = conf.DeleteServer(server.Name, backendParentType, backend, txId, 0)
	}

	return nil
}

func (h *haproxy) reload() error {
	h.logger.Info("reloading configuration")
	r, err := h.client.Runtime()
	if err != nil {
		return err
	}
	logs, err := r.Reload()
	h.logger.Debug("startup logs after reload", "logs", logs)
	return err
}

func newClient() (client_native.HAProxyClient, error) {
	ctx := context.Background()

	// 1. Configuration client
	confClient, err := configuration.New(ctx,
		cfg_opt.ConfigurationFile("/etc/haproxy/haproxy.cfg"),
		cfg_opt.HAProxyBin("/usr/sbin/haproxy"),
		cfg_opt.Backups(3),
		cfg_opt.UsePersistentTransactions,
		cfg_opt.TransactionsDir("/tmp/haproxy-tx"),
	)
	if err != nil {
		return nil, err
	}

	// 2. Runtime client (stats socket)
	runtimeClient, err := runtime.New(ctx,
		runtime_options.MasterSocket("/var/run/haproxy-master.sock"),
	)
	if err != nil {
		return nil, err
	}

	// 3. Combine into top-level client
	client, err := client_native.New(ctx,
		options.Configuration(confClient),
		options.Runtime(runtimeClient),
	)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func getFrontendName(serviceName string) string {
	return fmt.Sprintf("ft-%s", serviceName)
}

func getBackendName(serviceName string) string {
	return fmt.Sprintf("bk-%s", serviceName)
}
