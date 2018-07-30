package kubernetes

import (
	"os"
	"time"

	logging "github.com/op/go-logging"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var log = logging.MustGetLogger("kubekite")

func init() {

	var format = logging.MustStringFormatter(
		`%{color}%{time:15:04:05.000} %{shortfile} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}`,
	)

	logBackend := logging.NewLogBackend(os.Stderr, "", 0)
	logBackendFormatter := logging.NewBackendFormatter(logBackend, format)
	logging.SetBackend(logBackend, logBackendFormatter)
}

// NewKubeClientSet creates and initializes a Kubernetes API client to manage our jobs
func NewKubeClientSet(kubeconfig string, kubeTimeout int) (*kubernetes.Clientset, error) {
	var err error
	var config *rest.Config

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	} else {
		log.Info("Using in-cluster Kube config...")
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}

	if kubeTimeout > 0 {
		config.Timeout = time.Duration(kubeTimeout) * time.Second
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}
