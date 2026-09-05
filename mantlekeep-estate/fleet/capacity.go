package fleet

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// KSM reads free capacity from kube-state-metrics.
//
// Plain HTTP and the Prometheus text format, parsed with the standard library: no client-go, no
// Prometheus client, and therefore no version coupling to any particular Kubernetes
// distribution or cloud provider. The two metrics used have been stable for years.
//
// What it produces is REPORTED, never verified — KSM tells us this and can be stale or wrong.
// It ranks candidates that residency has already permitted; it can never admit one residency
// refused.
type KSM struct {
	// endpoints maps a cluster name to its kube-state-metrics URL. One per cluster: KSM runs
	// inside the cluster it describes, which is also why an unreachable KSM says nothing about
	// any other cluster.
	endpoints map[string]string
	client    *http.Client
}

// NewKSM builds a reader over per-cluster endpoints.
func NewKSM(endpoints map[string]string) *KSM {
	return &KSM{
		endpoints: endpoints,
		// A capacity read must never hold up a reconcile pass. Ranking without capacity is a
		// worse answer; waiting forever is no answer at all.
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Read returns free capacity per cluster, and the clusters it could not read.
//
// A cluster whose KSM did not answer is OMITTED rather than reported as zero. Zero means full,
// which would exclude the cluster from placement entirely — so a metrics outage would silently
// stop apps being placed anywhere, and the cause would look like a capacity problem. Omission
// means unknown, and the placer ranks unknown last while still considering it.
func (k *KSM) Read(ctx context.Context) ([]estate.Capacity, []string) {
	var reports []estate.Capacity
	var unread []string

	for cluster, endpoint := range k.endpoints {
		free, err := k.free(ctx, endpoint)
		if err != nil {
			unread = append(unread, cluster)
			continue
		}
		reports = append(reports, estate.Capacity{Cluster: cluster, AllocatablePct: free})
	}
	return reports, unread
}

// free fetches one cluster's metrics and returns the free share of allocatable memory.
//
// Memory rather than CPU: CPU requests are routinely over-committed and a cluster can sit above
// 100% requested CPU while scheduling happily, so it is a poor signal for "will a pod fit".
// Memory is not over-committed — a pod that does not fit does not schedule.
func (k *KSM) free(ctx context.Context, endpoint string) (float64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	response, err := k.client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("ksm: %s returned %d", endpoint, response.StatusCode)
	}

	allocatable, requested := 0.0, 0.0
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 32<<20))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		switch {
		case strings.HasPrefix(line, "kube_node_status_allocatable"):
			if v, ok := memoryValue(line); ok {
				allocatable += v
			}
		case strings.HasPrefix(line, "kube_pod_container_resource_requests"):
			if v, ok := memoryValue(line); ok {
				requested += v
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if allocatable <= 0 {
		// No allocatable memory reported is not an empty cluster — it is a metrics problem.
		// Returning 1.0 here would advertise infinite room on a cluster nobody measured.
		return 0, fmt.Errorf("ksm: %s reported no allocatable memory", endpoint)
	}

	free := (allocatable - requested) / allocatable
	if free < 0 {
		free = 0 // over-committed: full, and honestly so
	}
	return free, nil
}

// memoryValue pulls the sample value from a metric line, if it is about memory.
//
// Hand-parsed rather than pulling a Prometheus client: two metric names and one number does not
// justify a dependency tree on a module that must build in an air-gapped zone.
func memoryValue(line string) (float64, bool) {
	if !strings.Contains(line, `resource="memory"`) {
		return 0, false
	}
	space := strings.LastIndex(line, " ")
	if space < 0 {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(line[space+1:]), 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
