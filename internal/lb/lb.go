package lb

import v1 "k8s.io/api/core/v1"

type LoadBalancer interface {
	Sync(svcList []*v1.Service)
	Delete(svc *v1.Service)
}
