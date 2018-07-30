package buildkite

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/buildkite/go-buildkite/buildkite"
	logging "github.com/op/go-logging"
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

// NewBuildkiteClient creates and initializes a Buildkite API client to watch for build jobs
func NewBuildkiteClient(bkAPIToken string, debug bool) (*buildkite.Client, error) {

	bkconfig, err := buildkite.NewTokenConfig(bkAPIToken, debug)
	if err != nil {
		return nil, fmt.Errorf("unable to configure a new Buildkite client: %v", err)
	}

	c := buildkite.NewClient(bkconfig.Client())

	return c, nil

}

// StartBuildkiteWatcher starts a watcher that monitors a pipeline for new jobs
func StartBuildkiteWatcher(ctx context.Context, wg *sync.WaitGroup, client *buildkite.Client, org string, pipeline string) chan string {
	c := make(chan string, 10)

	go watchBuildkiteJobs(ctx, wg, client, org, pipeline, c)

	log.Info("Buildkite job watcher started.")

	return c
}

func watchBuildkiteJobs(ctx context.Context, wg *sync.WaitGroup, client *buildkite.Client, org string, pipeline string, jobChan chan<- string) {
	wg.Add(1)
	defer wg.Done()

	for {

		log.Info("Checking Buildkite API for builds and jobs...")

		builds, _, err := client.Builds.ListByPipeline(org, pipeline, &buildkite.BuildsListOptions{})
		if err != nil {
			log.Error("Error fetching builds from Buildkite API:", err)
		}

		for _, build := range builds {

			// log.Printf("Build --> %v [%v]\n", *build.ID, *build.State)

			for _, job := range build.Jobs {

				// if job.State != nil {
				// 	log.Printf("Job --> %v [%v]\n", *job.ID, *job.State)
				// }

				if job.State != nil && *job.State == "scheduled" {
					jobChan <- *job.ID
				}

			}

		}

		time.Sleep(15 * time.Second)

	}

}
