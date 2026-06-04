package reconciler

import (
	"context"
	"fmt"
	"kubelb/internal/ippool"
	"kubelb/internal/loadbalancer"
	"kubelb/pkg/k8s"
	"log/slog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type ServiceReconciler struct {
	loadBalancer loadbalancer.LoadBalancer
	ipPool       ippool.Pool

	clientSet    *kubernetes.Clientset
	svcInformer  cache.SharedIndexInformer
	nodeInformer cache.SharedIndexInformer
	factory      informers.SharedInformerFactory

	logger *slog.Logger
}

func NewServiceReconciler(loadBalancer loadbalancer.LoadBalancer, ipPool ippool.Pool, kubeConfig string, logger *slog.Logger) (*ServiceReconciler, error) {
	clientSet, err := k8s.BuildClientset(kubeConfig)
	if err != nil {
		return nil, err
	}

	factory := informers.NewSharedInformerFactory(clientSet, 0)
	svcInformer := factory.Core().V1().Services().Informer()
	nodeInformer := factory.Core().V1().Nodes().Informer()

	return &ServiceReconciler{
		loadBalancer: loadBalancer,
		ipPool:       ipPool,
		clientSet:    clientSet,
		svcInformer:  svcInformer,
		nodeInformer: nodeInformer,
		factory:      factory,
		logger:       logger,
	}, nil
}

func (sr *ServiceReconciler) Reconcile(stopCh <-chan struct{}) error {
	_, err := sr.svcInformer.AddEventHandler(
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
		return err
	}

	services, err := sr.clientSet.CoreV1().Services("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, svc := range services.Items {
		if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
			sr.add(&svc)
		}
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
	nodes, err := sr.clientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, node := range nodes.Items {
		sr.loadBalancer.AddNode(node.Status.Addresses[0].Address)
	}

	sr.factory.Start(stopCh)
	sr.factory.WaitForCacheSync(stopCh)

	fmt.Println("Informer synced, watching for service changes...")
	<-stopCh
	return nil
}

func (sr *ServiceReconciler) add(svc *v1.Service) {
	sr.logger.Info("adding service", "namespace", svc.Namespace, "name", svc.Name)
	if shouldSetStatus(svc) {
		sr.setStatus(svc)
	}
	sr.loadBalancer.Add(svc)
}

func (sr *ServiceReconciler) update(old, new *v1.Service) {
	sr.logger.Info("updating service", "namespace", new.Namespace, "name", new.Name)
	sr.loadBalancer.Update(new)
}

func (sr *ServiceReconciler) delete(svc *v1.Service) {
	// todo: take back ip
	sr.logger.Info("deleting service", "namespace", svc.Namespace, "name", svc.Name)
	sr.loadBalancer.Delete(svc)
}

func (sr *ServiceReconciler) setStatus(svc *v1.Service) {
	ip, err := sr.ipPool.Get()
	if err != nil {
		sr.logger.Error(err.Error())
		return
	}

	svc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: ip}}
	_, err = sr.clientSet.CoreV1().Services(svc.Namespace).UpdateStatus(context.Background(), svc, metav1.UpdateOptions{})
	if err != nil {
		sr.logger.Error(err.Error())
		return
	}
}

func shouldSetStatus(svc *v1.Service) bool {
	return svc.Status.LoadBalancer.Ingress == nil
}
