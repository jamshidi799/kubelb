package reconciler

import (
	"context"
	"fmt"
	"kubelb/internal/ippool"
	"kubelb/internal/lb"
	"kubelb/pkg/k8s"
	"log/slog"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type ServiceReconciler struct {
	loadBalancer lb.LoadBalancer
	ipPool       ippool.Pool

	clientSet *kubernetes.Clientset
	informer  cache.SharedIndexInformer
	factory   informers.SharedInformerFactory

	logger *slog.Logger
}

func NewServiceReconciler(loadBalancer lb.LoadBalancer, ipPool ippool.Pool, kubeConfig string, logger *slog.Logger) (*ServiceReconciler, error) {
	clientSet, err := k8s.BuildClientset(kubeConfig)
	if err != nil {
		return nil, err
	}

	factory := informers.NewSharedInformerFactory(clientSet, 30*time.Second)
	informer := factory.Core().V1().Services().Informer()

	err = informer.AddIndexers(map[string]cache.IndexFunc{
		"by-type": func(obj interface{}) ([]string, error) {
			svc := obj.(*v1.Service)
			return []string{string(svc.Spec.Type)}, nil
		},
	})
	if err != nil {
		return nil, err
	}

	return &ServiceReconciler{
		loadBalancer: loadBalancer,
		ipPool:       ipPool,
		clientSet:    clientSet,
		informer:     informer,
		factory:      factory,
		logger:       logger,
	}, nil
}

func (s *ServiceReconciler) Reconcile(stopCh <-chan struct{}) error {
	_, err := s.informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				svc := obj.(*v1.Service)
				if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
					s.triggerSync(svc)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				newSvc := newObj.(*v1.Service)
				oldSvc := oldObj.(*v1.Service)

				if oldSvc.Spec.Type == v1.ServiceTypeLoadBalancer && newSvc.Spec.Type == v1.ServiceTypeLoadBalancer {
					s.triggerSync(newSvc)
				}

				if oldSvc.Spec.Type == v1.ServiceTypeLoadBalancer && newSvc.Spec.Type != v1.ServiceTypeLoadBalancer {
					s.triggerDelete(oldSvc)
				}
			},
			DeleteFunc: func(obj interface{}) {
				svc := obj.(*v1.Service)
				if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
					s.triggerDelete(svc)
				}
			},
		})

	if err != nil {
		return err
	}

	s.factory.Start(stopCh)
	s.factory.WaitForCacheSync(stopCh)

	fmt.Println("Informer synced, watching for service changes...")
	<-stopCh
	return nil
}

func (s *ServiceReconciler) triggerSync(svc *v1.Service) {
	if svc.Status.LoadBalancer.Ingress == nil {
		ip, err := s.ipPool.Get()
		if err != nil {
			s.logger.Error(err.Error())
			return
		}

		svc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: ip}}
		_, err = s.clientSet.CoreV1().Services(svc.Namespace).UpdateStatus(context.Background(), svc, metav1.UpdateOptions{})
		if err != nil {
			s.logger.Error(err.Error())
			return
		}
	}

	list, err := s.informer.GetIndexer().ByIndex("by-type", "LoadBalancer")
	if err != nil {
		s.logger.Error(err.Error())
	}

	svcList := make([]*v1.Service, 0, len(list))
	for _, obj := range list {
		svc := obj.(*v1.Service)
		svcList = append(svcList, svc)
	}

	s.loadBalancer.Sync(svcList)
}

func (s *ServiceReconciler) triggerDelete(svc *v1.Service) {

}
