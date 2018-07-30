package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// KubeJobManager holds all Kubernetes job resources managed by buildkite-job-manager
type KubeJobManager struct {
	jobTemplate batchv1.Job
	Client      *kubernetes.Clientset
	Jobs        map[string]*batchv1.Job
	JobsMutex   sync.RWMutex
	namespace   string
	org         string
	pipeline    string
}

// NewKubeJobManager creates a new KubeJobManager object for managing jobs
func NewKubeJobManager(ctx context.Context, wg *sync.WaitGroup, templateFilename string, kubeconfig string, kubeNamespace string, kubeTimeout int, org string, pipeline string) (*KubeJobManager, error) {
	var err error

	k := new(KubeJobManager)

	k.namespace = kubeNamespace
	k.org = org
	k.pipeline = pipeline

	k.Jobs = make(map[string]*batchv1.Job)

	if kubeconfig == "" {
		log.Info("No kubeconfig was provided; using in-cluster config.")
		log.Info("If you're not running kubekite within Kubernetes, please run with -kubeconfig flag or set KUBECONFIG environment variable")
	}

	k.Client, err = NewKubeClientSet(kubeconfig, kubeTimeout)
	if err != nil {
		return nil, err
	}

	jobTemplate, err := ioutil.ReadFile(templateFilename)
	if err != nil {
		return nil, fmt.Errorf("could not open job template: %v", err)
	}

	// Marshalling a YAML pod template into a PodTemplateSpec is problematic,
	// so we marshall it to JSON and then into the struct.
	jsonSpec, err := yaml.YAMLToJSON(jobTemplate)

	err = json.Unmarshal(jsonSpec, &k.jobTemplate)
	if err != nil {
		log.Fatal(err)
	}

	k.StartJobCleaner(ctx, wg)

	return k, nil
}

// StartJobCleaner starts a monitor that watches for completed build jobs and cleans up the dangling Kube job resources
func (k *KubeJobManager) StartJobCleaner(ctx context.Context, wg *sync.WaitGroup) {
	go k.cleanCompletedJobs(ctx, wg)

	log.Info("Kube job cleaner started.")
}

// LaunchJob launches a Kubernetes job for a given Buildkite job ID
func (k *KubeJobManager) LaunchJob(uuid string) error {
	var err error

	jobLabels := make(map[string]string)
	jobLabels["kubekite-managed"] = "true"
	jobLabels["kubekite-org"] = k.org
	jobLabels["kubekite-pipeline"] = k.pipeline

	t := k.jobTemplate

	// Set our labels on both the job and the pod that it generates
	t.SetLabels(jobLabels)
	t.Spec.Template.SetLabels(jobLabels)

	t.Name = "buildkite-agent-" + uuid

	runningJob, err := k.Client.BatchV1().Jobs(k.namespace).Get(t.Name, metav1.GetOptions{})
	if err == nil {
		log.Infof("Job %v already exists, not launching.\n", runningJob.Name)
		return nil
	}

	k.JobsMutex.Lock()
	defer k.JobsMutex.Unlock()
	k.Jobs[uuid] = new(batchv1.Job)
	k.Jobs[uuid] = &t

	k.Jobs[uuid], err = k.Client.BatchV1().Jobs(k.namespace).Create(k.Jobs[uuid])
	if err != nil {
		return fmt.Errorf("could not launch job: %v", err)
	}

	log.Infof("Launched job: %v", k.Jobs[uuid].Name)
	return nil
}

func (k *KubeJobManager) cleanCompletedJobs(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	selector := fmt.Sprintf("kubekite-managed=true,kubekite-org=%v,kubekite-pipeline=%v", k.org, k.pipeline)

	for {

		log.Info("Cleaning completed jobs...")

		pods, err := k.Client.CoreV1().Pods(k.namespace).List(metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			log.Errorf("Could not list pods: %v", err)
		}

		for _, pod := range pods.Items {
			for _, container := range pod.Status.ContainerStatuses {
				if container.State.Terminated != nil && container.Name == "buildkite-agent" {
					jobName := pod.Labels["job-name"]
					log.Infof("Deleting job: %v", jobName)

					policy := metav1.DeletePropagationForeground

					err := k.Client.BatchV1().Jobs(k.namespace).Delete(jobName, &metav1.DeleteOptions{PropagationPolicy: &policy})
					if err != nil {
						log.Error("Error deleting job:", err)
					}
				}
			}
		}

		time.Sleep(15 * time.Second)

	}

}
