package reconciler

import (
	"context"
	"kubelb/internal/ippool"
	"kubelb/internal/loadbalancer"
	"log/slog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type ServiceReconciler struct {
	loadBalancer loadbalancer.LoadBalancer
	ipPool       ippool.Pool

	clientSet    kubernetes.Interface
	svcInformer  cache.SharedIndexInformer
	nodeInformer cache.SharedIndexInformer

	logger *slog.Logger
}

func NewServiceReconciler(loadBalancer loadbalancer.LoadBalancer, ipPool ippool.Pool, clientSet kubernetes.Interface, svcInformer cache.SharedIndexInformer, nodeInformer cache.SharedIndexInformer, logger *slog.Logger) (*ServiceReconciler, error) {
	sr := &ServiceReconciler{
		loadBalancer: loadBalancer,
		ipPool:       ipPool,
		clientSet:    clientSet,
		svcInformer:  svcInformer,
		nodeInformer: nodeInformer,
		logger:       logger,
	}

	_, err := svcInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				svc := obj.(*v1.Service)
				if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
					sr.add(svc)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				newSvc := newObj.(*v1.Service)
				oldSvc := oldObj.(*v1.Service)

				if oldSvc.Spec.Type != v1.ServiceTypeLoadBalancer && newSvc.Spec.Type == v1.ServiceTypeLoadBalancer {
					sr.add(newSvc)
				}

				if oldSvc.Spec.Type == v1.ServiceTypeLoadBalancer && newSvc.Spec.Type == v1.ServiceTypeLoadBalancer {
					sr.update(oldSvc, newSvc)
				}

				if oldSvc.Spec.Type == v1.ServiceTypeLoadBalancer && newSvc.Spec.Type != v1.ServiceTypeLoadBalancer {
					sr.delete(oldSvc)
				}
			},
			DeleteFunc: func(obj interface{}) {
				svc := obj.(*v1.Service)
				if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
					sr.delete(svc)
				}
			},
		})

	if err != nil {
		return nil, err
	}

	_, err = sr.nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node := obj.(*v1.Node)
			sr.loadBalancer.AddNode(node.Status.Addresses[0].Address)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {},
		DeleteFunc: func(obj interface{}) {
			node := obj.(*v1.Node)
			sr.loadBalancer.DeleteNode(node.Status.Addresses[0].Address)
		},
	})

	return sr, err
}
func (sr *ServiceReconciler) add(svc *v1.Service) {
	sr.logger.Info("adding service", "namespace", svc.Namespace, "name", svc.Name)
	if shouldSetStatus(svc) {
		err := sr.setStatus(svc)
		if err != nil {
			sr.logger.Error(err.Error())
			return
		}
	}
	sr.loadBalancer.Add(svc)
}

func (sr *ServiceReconciler) update(_, new *v1.Service) {
	sr.logger.Info("updating service", "namespace", new.Namespace, "name", new.Name)
	sr.loadBalancer.Update(new)
}

func (sr *ServiceReconciler) delete(svc *v1.Service) {
	ip := svc.Status.LoadBalancer.Ingress[0].IP
	if ip != "" {
		sr.ipPool.Take(ip)
	}
	sr.logger.Info("deleting service", "namespace", svc.Namespace, "name", svc.Name)
	sr.loadBalancer.Delete(svc)
}

func (sr *ServiceReconciler) setStatus(svc *v1.Service) error {
	ip, err := sr.ipPool.Get()
	if err != nil {
		return err
	}

	svc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: ip}}
	_, err = sr.clientSet.CoreV1().Services(svc.Namespace).UpdateStatus(context.Background(), svc, metav1.UpdateOptions{})
	if err != nil {
		sr.logger.Error(err.Error())
		return nil
	}
	return nil
}

func shouldSetStatus(svc *v1.Service) bool {
	return svc.Status.LoadBalancer.Ingress == nil
}
