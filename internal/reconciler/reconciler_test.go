package reconciler

import (
	"context"
	"kubelb/internal/ippool"
	"kubelb/internal/lb"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

type fakeLB struct {
	addServices, updateServices, deleteServices chan *v1.Service
	addNodes, deleteNode                        chan string
}

func newFakeLB() *fakeLB {
	return &fakeLB{
		addServices:    make(chan *v1.Service, 10),
		updateServices: make(chan *v1.Service, 10),
		deleteServices: make(chan *v1.Service, 10),
		addNodes:       make(chan string, 10),
		deleteNode:     make(chan string, 10),
	}
}

func (f *fakeLB) Add(svc *v1.Service) {
	f.addServices <- svc.DeepCopy()
}
func (f *fakeLB) Update(svc *v1.Service) {
	f.updateServices <- svc.DeepCopy()
}
func (f *fakeLB) Delete(svc *v1.Service) {
	f.deleteServices <- svc.DeepCopy()
}
func (f *fakeLB) AddNode(ip string) {
	f.addNodes <- ip
}
func (f *fakeLB) DeleteNode(ip string) {
	f.deleteNode <- ip
}

func read[T *v1.Service | string](ctx context.Context, ch <-chan T) (T, error) {
	var zero T
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case item := <-ch:
		return item, nil
	}
}

func newTestReconciler(t *testing.T, ctx context.Context, lb lb.LoadBalancer, ipp ippool.Pool, client *fake.Clientset) *ServiceReconciler {
	t.Helper()

	factory := informers.NewSharedInformerFactory(client, 0)
	svcInformer := factory.Core().V1().Services().Informer()
	nodeInformer := factory.Core().V1().Nodes().Informer()

	r, err := NewServiceReconciler(lb, ipp, client, svcInformer, nodeInformer, slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	factory.Start(ctx.Done())
	return r
}

func TestReconciler_AddService(t *testing.T) {
	lbSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc-lb",
		},
		Spec: v1.ServiceSpec{
			Type:                  v1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyTypeLocal,
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					NodePort: int32(31289),
					Protocol: v1.ProtocolTCP,
				},
			},
		},
	}

	clusterIpSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc-cluster-ip",
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeClusterIP,
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					NodePort: int32(31289),
					Protocol: v1.ProtocolTCP,
				},
			},
		},
	}

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "172.0.0.1",
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ip := "1.1.1.1"
	ipp := ippool.NewStatic([]string{ip})
	lb := newFakeLB()
	client := fake.NewSimpleClientset(node, lbSvc, clusterIpSvc)
	_ = newTestReconciler(t, ctx, lb, ipp, client)

	got, err := read[*v1.Service](ctx, lb.updateServices)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(lbSvc.Spec, got.Spec); diff != "" {
		t.Errorf("Spec mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(lbSvc.ObjectMeta, got.ObjectMeta); diff != "" {
		t.Errorf("ObjectMeta mismatch (-want +got):\n%s", diff)
	}

	if got.Status.LoadBalancer.Ingress == nil {
		t.Errorf("expected ingress to not be nil")
	} else if got.Status.LoadBalancer.Ingress[0].IP != ip {
		t.Errorf("expected ingress to have IP %s, got %s", ip, got.Status.LoadBalancer.Ingress[0].IP)
	}

	nodeIp, err := read[string](ctx, lb.addNodes)
	if err != nil {
		t.Fatal(err)
	}
	if nodeIp != node.Status.Addresses[0].Address {
		t.Errorf("expected ip: %s got: %s", nodeIp, node.Status.Addresses[0].Address)
	}
}

func TestReconciler_UpdateService(t *testing.T) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc-lb",
		},
		Spec: v1.ServiceSpec{
			Type:                  v1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyTypeLocal,
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					NodePort: int32(31289),
					Protocol: v1.ProtocolTCP,
				},
			},
		},
	}

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "172.0.0.1",
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ip := "1.1.1.1"
	ipp := ippool.NewStatic([]string{ip})
	lb := newFakeLB()
	client := fake.NewSimpleClientset(node, svc)
	r := newTestReconciler(t, ctx, lb, ipp, client)
	if !cache.WaitForCacheSync(ctx.Done(), r.svcInformer.HasSynced, r.nodeInformer.HasSynced) {
		t.Fatal("cache never synced")
	}

	newPort := int32(8080)
	newSvc := svc.DeepCopy()
	newSvc.Spec.Ports[0].Port = newPort
	client.CoreV1().Services(newSvc.ObjectMeta.Namespace).Update(ctx, newSvc, metav1.UpdateOptions{})

	if !cache.WaitForCacheSync(ctx.Done(), r.svcInformer.HasSynced) {
		t.Fatal("cache never synced")
	}

	if _, err := read[*v1.Service](ctx, lb.updateServices); err != nil {
		t.Fatal(err)
	}

	got, err := read[*v1.Service](ctx, lb.updateServices)
	if err != nil {
		t.Fatal(err)
	}

	if got.Spec.Ports[0].Port != newPort {
		t.Errorf("expected port %d, got %d", newPort, got.Spec.Ports[0].Port)
	}
}

func TestReconciler_DeleteService(t *testing.T) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svc-lb",
		},
		Spec: v1.ServiceSpec{
			Type:                  v1.ServiceTypeLoadBalancer,
			ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyTypeLocal,
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Port:     80,
					NodePort: int32(31289),
					Protocol: v1.ProtocolTCP,
				},
			},
		},
	}

	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "172.0.0.1",
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ip := "1.1.1.1"
	ipp := ippool.NewStatic([]string{ip})
	lb := newFakeLB()
	client := fake.NewSimpleClientset(node, svc)
	r := newTestReconciler(t, ctx, lb, ipp, client)
	if !cache.WaitForCacheSync(ctx.Done(), r.svcInformer.HasSynced) {
		t.Fatal("cache never synced")
	}

	client.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{})
	_, err := read[*v1.Service](ctx, lb.deleteServices)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ipp.Get()
	if err != nil {
		t.Fatal("expected no err")
	}

	if got != ip {
		t.Errorf("expected %s, got %s", ip, got)
	}
}
