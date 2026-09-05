// Package serve is the composition root for an estate service: it reads the floor and the fleet,
// wires the door client, the manager and the read side, and serves the HTTP API.
//
// It exists as its own package for one reason: WHICH ADAPTERS a binary carries decides which
// third-party trees it links, and that decision must not reach this module's dependency graph.
// The in-memory build links neither a Kubernetes client nor a database driver, so a CVE in
// either cannot stop it building — a property that survives only while the adapters are passed
// IN rather than imported here.
//
// A binary is therefore small: construct its adapters, call [Run].
package serve

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/api"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/config"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/doorclient"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/fleet"
)

// Options are what a binary chooses that is not read from configuration.
type Options struct {
	// Ports are the adapters this binary carries. Empty is legal and logged: an asset with no
	// adapter is reported per change rather than refused at boot.
	Ports []estate.Port
	// Name identifies the binary in logs, so an operator can tell which build is running.
	Name string
}

func Run(options Options) error {
	var (
		addr       = flag.String("addr", envOr("MANTLEKEEP_ESTATE_ADDR", ":8092"), "listen address")
		configPath = flag.String("config", os.Getenv("MANTLEKEEP_ESTATE_CONFIG"),
			"path to the floor and ownership document")
		doorURL = flag.String("door", envOr("MANTLEKEEP_DOOR_URL", "http://localhost:8080"),
			"base URL of the door")
		account = flag.String("service-account", envOr("MANTLEKEEP_SERVICE_ACCOUNT", "mantlekeep-estate"),
			"the identity this service authenticates to the door AS")
		fleetPath = flag.String("fleet", os.Getenv("MANTLEKEEP_ESTATE_FLEET"),
			"path to the cluster registry")
		ksmSpec = flag.String("ksm", os.Getenv("MANTLEKEEP_ESTATE_KSM"),
			"cluster=url,cluster=url — kube-state-metrics endpoints, one per cluster")
	)
	flag.Parse()

	// The floor is the sealed floor's data. There is deliberately no fallback: a service that
	// silently governs under a built-in default is a service whose limits nobody chose, and the
	// operator would have no way to tell.
	live, err := config.OpenLive(*configPath)
	if err != nil {
		return err
	}
	settings := live.Current()
	slog.Info("floor loaded", "revision", settings.Floor.Revision, "path", live.Path())

	// The fleet. A control plane with no registry can place nothing, so this is refused rather
	// than defaulted — an empty fleet would refuse every app with a message about placement
	// when the real fault is a missing file.
	clusters, err := fleet.Load(*fleetPath)
	if err != nil {
		return err
	}

	// Reachability is MEASURED, not declared — Load returns every cluster unreachable and
	// something has to say what actually answered. With no prober built yet this ASSUMES, and
	// says so, because every placement it then makes trusts a file rather than a cluster.
	clusters = fleet.MarkReachable(clusters, fleet.AssumeReachable().Reachable(clusters))
	slog.Warn("cluster reachability is ASSUMED, not probed — placement is trusting the " +
		"registry file rather than the clusters themselves")

	// Capacity is REPORTED and optional. Without it placement still works; it simply cannot
	// prefer the emptier of two equally permitted clusters, which is a worse answer rather
	// than a wrong one. A cluster whose KSM is silent is UNKNOWN, never full.
	buildPlacer := func(clusters []estate.Cluster) (*estate.Placer, []string) {
		placer := estate.NewPlacer(clusters)
		if endpoints := parseKSM(*ksmSpec); len(endpoints) > 0 {
			reports, unread := fleet.NewKSM(endpoints).Read(context.Background())
			return placer.WithCapacity(reports), unread
		}
		return placer, nil
	}
	placer, unreadKSM := buildPlacer(clusters)

	// The fleet is held the same way the floor is, and for a sharper reason: a cluster that
	// has been drained keeps receiving placements until something says otherwise, and "cut a
	// release" is not an answer at 3am. A whole new Placer is built and swapped — never
	// mutated in place, so a request mid-flight sees one fleet or the other, never half.
	var livePlacer atomic.Pointer[estate.Placer]
	livePlacer.Store(placer)

	door := doorclient.New(*doorURL, *account)
	store := estate.NewMemoryManifests()
	// Where gated changes wait for a person. In memory for now, and the warning below says so:
	// a restart forgets every pending change, and somebody who was asked to sign one off will
	// find it gone.
	approvals := estate.NewMemoryApprovals()

	// Adapters are chosen by the BINARY, passed in rather than imported here — which is what
	// keeps client-go and database drivers out of this module's dependency graph. An asset with
	// no adapter is REPORTED per change rather than refused at boot: a deployment that governs
	// Kafka today and Postgres next quarter must not be unable to start in between.
	ports := options.Ports
	names := make([]string, 0, len(ports))
	for _, port := range ports {
		names = append(names, port.Asset())
	}

	service := estate.NewService(settings.Floor, store, ports...).FloorFrom(live.Floor).PlaceOnLive(livePlacer.Load)
	manager := estate.NewManager(door, settings.Floor, ports...).
		FloorFrom(live.Floor).
		GovernFields(settings.Ownership).
		RememberManifestsIn(store).
		AwaitApprovalIn(approvals).
		PlaceOnLive(livePlacer.Load)

	// SIGHUP re-reads the floor. Operationally the cases are ordinary and urgent — a quota is
	// wrong at 3am, a shared DEV env turns out to need a gate — and making each of them cost a
	// release is how a platform earns a workaround.
	//
	// A bad file changes nothing: the previous floor keeps serving and the error is logged at
	// ERROR with the revision still in force, so an operator can see that their edit did NOT
	// take rather than assuming it did.
	reloads := make(chan os.Signal, 1)
	signal.Notify(reloads, syscall.SIGHUP)
	go func() {
		for range reloads {
			reloaded, err := live.Reload()
			if err != nil {
				slog.Error("floor reload REFUSED — the previous floor is still in force",
					"error", err, "revision", reloaded.Floor.Revision)
			} else {
				slog.Info("floor reloaded", "revision", reloaded.Floor.Revision)
			}

			// The fleet reloads independently. A bad registry must not discard a good floor
			// reload, and vice versa — one signal, two decisions, each reported on its own.
			refreshed, err := fleet.Load(*fleetPath)
			if err != nil {
				slog.Error("fleet reload REFUSED — the previous registry is still in force",
					"error", err)
				continue
			}
			refreshed = fleet.MarkReachable(refreshed, fleet.AssumeReachable().Reachable(refreshed))
			next, _ := buildPlacer(refreshed)
			livePlacer.Store(next)
			slog.Info("fleet reloaded", "clusters", len(refreshed))
		}
	}()

	mux := http.NewServeMux()
	api.New(manager, service, api.HeaderCallers{}).Routes(mux)

	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	slog.Info("mantlekeep-estate listening",
		"addr", *addr, "door", *doorURL, "adapters", names, "config", *configPath,
		"clusters", len(clusters))
	if len(unreadKSM) > 0 {
		// Worth saying out loud. Placement still works, but it is ranking blind on these, and
		// silence here would look like a capacity decision rather than a monitoring gap.
		slog.Warn("capacity could not be read for some clusters — placement will rank them "+
			"last rather than treat them as full", "clusters", unreadKSM)
	}
	if len(ports) == 0 {
		// Worth saying out loud. The service will govern every change correctly and then
		// report that nothing could execute it, which looks like a product fault rather than
		// a build that included no adapters.
		// Naming the binary matters: the advice is "run a different one", and an operator who
		// does not know which one they are running cannot act on that.
		// Said out loud, because a person asked to approve something will find it gone after a
		// restart and will reasonably conclude the platform lost their decision.
		slog.Warn("approvals are held IN MEMORY — a restart forgets every change waiting for a " +
			"person; this is a demo store, not a deployment one")

		slog.Warn("no adapters were supplied — every change will be governed and then reported "+
			"as unexecutable; run a binary that carries them, e.g. mantlekeep-estate-k8s",
			"binary", options.Name)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	errs := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-stop:
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		slog.Info("shutting down")
		return server.Shutdown(shutdown)
	}
}

// parseKSM reads "cluster=url,cluster=url". A malformed pair is SKIPPED with a warning rather
// than failing the boot: losing capacity for one cluster degrades placement, while refusing to
// start loses the whole control plane over a typo in an optional field.
func parseKSM(spec string) map[string]string {
	endpoints := map[string]string{}
	for _, pair := range strings.Split(spec, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		cluster, url, ok := strings.Cut(pair, "=")
		if !ok || cluster == "" || url == "" {
			slog.Warn("ignoring malformed kube-state-metrics entry", "entry", pair)
			continue
		}
		endpoints[cluster] = url
	}
	return endpoints
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
