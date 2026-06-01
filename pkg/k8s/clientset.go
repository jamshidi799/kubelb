package k8s

import (
	"log"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func BuildClientset(kubeconfig string) (*kubernetes.Clientset, error) {
	if kubeconfig == "" {
		kubeconfig = getKubeconfig()
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

func getKubeconfig() string {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig != "" {
		return kubeconfig
	}

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	kubeconfig = filepath.Join(home, ".kube", "config")
	return kubeconfig
}
