package loadbalancer

import v1 "k8s.io/api/core/v1"

type LoadBalancer interface {
	Add(svc *v1.Service)
	Update(svc *v1.Service)
	Delete(svc *v1.Service)

	AddNode(ip string)
	DeleteNode(ip string)
}
