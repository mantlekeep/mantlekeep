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

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
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

	// Callers resolves WHO is calling, and is passed IN like every adapter.
	//
	// Nil means the trusted-header tier, which is fenced to loopback unless a deployment
	// names the decision — see [resolveCallers]. A binary that verifies tokens supplies a
	// resolver here from a module that carries the crypto, which is why this module contains
	// none: an organisation scanning what it downloads should not find an RSA finding in an
	// estate it took for governance.
	Callers api.CallerResolver

	// Transform rewrites a change before it is governed, and may refuse it outright.
	//
	// Nil means no transform, which is what every deployment got before this field existed.
	//
	// # Why this is an option rather than something this package builds
	//
	// [estate.ChangeTransformer] is the only seam that runs on the Apply path AND the Reconcile
	// path, before anything is submitted, and that a caller cannot skip. That makes it the place
	// a deployment puts a check it needs to be un-routable-around — an admission rule, an
	// expansion of a shorthand, a gate raised for one named app.
	//
	// Without this field a deployment had to choose between the server (and no transform) or a
	// transform (and re-implementing the floor reload, the live placer, the manifest store and
	// the read API). Neither is a choice anybody should have to make, and the one they made was
	// to go without the check.
	//
	// Passed IN like every adapter, for the same reason: this module must not learn what any
	// deployment's rules are.
	Transform estate.ChangeTransformer

	// Approvals is where gated changes wait for a person. Nil keeps the in-memory store, which is
	// what every deployment had before this field existed.
	//
	// # Why this needs to be a choice
	//
	// The in-memory store loses every pending approval on restart, and loses it SILENTLY: the
	// person who was asked to sign finds nothing and assumes they missed it. A queue that eats
	// work is a queue people stop using, and a gate nobody uses is ceremony.
	//
	// It is also the one store whose contract needs compare-and-set — two approvers may decide in
	// the same moment and exactly one must win — so a deployment that wants durability cannot get
	// there by wrapping this one. It has to supply its own.
	//
	// Passed IN like every adapter, for the same reason: this module must not learn what anyone's
	// database is.
	Approvals estate.Approvals

	// Routes mounts a deployment's OWN endpoints on the listener this package serves. Nil mounts
	// nothing, which is what every deployment had before this field existed.
	//
	// This package builds the mux and the server, so a deployment that writes a handler had
	// nowhere to put it — the choices were a second listener, or re-implementing Run. Passed IN
	// like every adapter, so this module never learns what a deployment's endpoints are. See
	// [Mounting] for what it is handed.
	Routes func(Mounting)

	// Door is where this estate submits governed changes. Nil dials -door over HTTP, which is the
	// previous behaviour exactly.
	//
	// A non-nil Submitter is used INSTEAD, and no door URL is read: the estate embeds the
	// governance core in its own process. That is the composition an environment with ONE
	// submitting service wants — one service to deploy, one credential, and no hop between the
	// estate and the door for a network policy to break or an attacker to sit on.
	//
	// What must not be lost is the DELEGATION. Over HTTP the door records the person AND the
	// service account acting for them, because the estate presented both. In process there is no
	// header to read, so an embedded Submitter MUST record the delegation itself, or every change
	// appears to have been made by the person directly and the estate disappears from the record.
	//
	// A door is still REQUIRED. Nil does not mean ungoverned; it means dialled.
	Door mantlekeep.Submitter
}

func Run(options Options) error {
	var (
		addr       = flag.String("addr", envOr("MANTLEKEEP_ESTATE_ADDR", ":8092"), "listen address")
		configPath = flag.String("config", envOr("MANTLEKEEP_ESTATE_CONFIG", ""),
			"path to the floor and ownership document")
		doorURL = flag.String("door", envOr("MANTLEKEEP_DOOR_URL", "http://localhost:8080"),
			"base URL of the door")
		account = flag.String("service-account", envOr("MANTLEKEEP_SERVICE_ACCOUNT", "mantlekeep-estate"),
			"the identity this service authenticates to the door AS")
		fleetPath = flag.String("fleet", envOr("MANTLEKEEP_ESTATE_FLEET", ""),
			"path to the cluster registry")
		ksmSpec = flag.String("ksm", envOr("MANTLEKEEP_ESTATE_KSM", ""),
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
	buildPlacer := placerBuilder(*ksmSpec)
	placer, unreadKSM := buildPlacer(clusters)

	// The fleet is held the same way the floor is, and for a sharper reason: a cluster that
	// has been drained keeps receiving placements until something says otherwise, and "cut a
	// release" is not an answer at 3am. A whole new Placer is built and swapped — never
	// mutated in place, so a request mid-flight sees one fleet or the other, never half.
	var livePlacer atomic.Pointer[estate.Placer]
	livePlacer.Store(placer)

	door := doorFor(options, func() mantlekeep.Submitter {
		return doorclient.New(*doorURL, *account)
	})
	store := estate.NewMemoryManifests()
	// Where gated changes wait for a person. In memory for now, and the warning below says so:
	// a restart forgets every pending change, and somebody who was asked to sign one off will
	// find it gone.
	approvals := approvalsFor(options)

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
	manager := managerFor(options, managerParts{
		door: door, settings: settings, service: service, store: store, approvals: approvals,
		floor: live.Floor, placer: livePlacer.Load,
	})

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
			reloadFloor(live)
			reloadFleet(*fleetPath, buildPlacer, &livePlacer)
		}
	}()

	callers, err := resolveCallers(*addr, options.Callers)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mount(mux, api.New(manager, service, callers),
		mountParts{door: door, callers: callers, footprints: service}, options.Routes)

	server := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	slog.Info("mantlekeep-estate listening",
		"addr", *addr, "door", doorLabel(options, *doorURL), "adapters", names, "config", *configPath,
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

// BrandPrefixVar names the prefix this deployment reads its environment under.
//
// The variable names ARE a contract: they appear in a deployment's manifests, its Helm
// values and its runbooks. Baking a product name into them makes rebranding a migration
// rather than a setting, which is the same reason the wire headers carry no product name.
//
// Set MANTLEKEEP_BRAND=ACME and the estate reads ACME_ESTATE_CONFIG, falling back to
// MANTLEKEEP_ESTATE_CONFIG so an existing deployment keeps working unchanged.
const BrandPrefixVar = "MANTLEKEEP_BRAND"

// defaultBrand is the prefix used when a deployment chooses none.
const defaultBrand = "MANTLEKEEP"

// brandPrefix reports the prefix in force, upper-cased so a lowercase brand still resolves.
func brandPrefix() string {
	if brand := strings.TrimSpace(os.Getenv(BrandPrefixVar)); brand != "" {
		return strings.ToUpper(brand)
	}
	return defaultBrand
}

// envOr reads a variable under the deployment's brand, then under the default, then falls
// back to the built-in value.
//
// Both are tried on purpose. A rebranded deployment sets its own names; one that has not
// rebranded keeps working with no change at all; and a deployment mid-migration can move
// one variable at a time rather than all of them in a single edit.
func envOr(name, fallback string) string {
	suffix := strings.TrimPrefix(name, defaultBrand+"_")
	for _, prefix := range []string{brandPrefix(), defaultBrand} {
		if value := strings.TrimSpace(os.Getenv(prefix + "_" + suffix)); value != "" {
			return value
		}
	}
	return fallback
}

// placerBuilder returns the function that turns a cluster set into a Placer.
//
// A named constructor rather than a closure in the middle of Run: it is called twice — once at
// boot and once on every SIGHUP — and a reader following a reload should not have to scroll back
// into startup to find out what a reload rebuilds.
//
// Capacity is REPORTED and optional. Without it placement still works; it simply cannot prefer
// the emptier of two equally permitted clusters, which is a worse answer rather than a wrong one.
// A cluster whose KSM is silent is UNKNOWN, never full.
func placerBuilder(ksmSpec string) func([]estate.Cluster) (*estate.Placer, []string) {
	return func(clusters []estate.Cluster) (*estate.Placer, []string) {
		placer := estate.NewPlacer(clusters)
		endpoints := parseKSM(ksmSpec)
		if len(endpoints) == 0 {
			return placer, nil
		}
		reports, unread := fleet.NewKSM(endpoints).Read(context.Background())
		return placer.WithCapacity(reports), unread
	}
}

// reloadFloor re-reads the floor and reports which one is now in force.
//
// A bad file changes nothing: the previous floor keeps serving and the failure is logged at ERROR
// with the revision still deciding, so an operator can see that their edit did NOT take rather
// than assuming it did.
func reloadFloor(live *config.Live) {
	reloaded, err := live.Reload()
	if err != nil {
		slog.Error("floor reload REFUSED — the previous floor is still in force",
			"error", err, "revision", reloaded.Floor.Revision)
		return
	}
	slog.Info("floor reloaded", "revision", reloaded.Floor.Revision)
}

// reloadFleet re-reads the cluster registry and swaps the placer.
//
// Separate from the floor on purpose: one signal, two decisions. A bad registry must not discard
// a good floor reload, and a bad floor must not discard a good registry — reporting them together
// would make an operator guess which half failed.
func reloadFleet(path string, build func([]estate.Cluster) (*estate.Placer, []string),
	livePlacer *atomic.Pointer[estate.Placer]) {

	refreshed, err := fleet.Load(path)
	if err != nil {
		slog.Error("fleet reload REFUSED — the previous registry is still in force", "error", err)
		return
	}
	refreshed = fleet.MarkReachable(refreshed, fleet.AssumeReachable().Reachable(refreshed))
	next, _ := build(refreshed)
	livePlacer.Store(next)
	slog.Info("fleet reloaded", "clusters", len(refreshed))
}

// managerFor builds the one manager this server governs through.
//
// Extracted from [Run] so it can be TESTED. Run parses flags, opens files and blocks, so nothing
// inside it is assertable — and the failure worth catching here is the quiet kind: an option that
// exists, is documented, and is never read. A field nobody wired looks exactly like a field nobody
// set, and both pass review.
func managerFor(options Options, built managerParts) *estate.Manager {
	return estate.NewManager(built.door, built.settings.Floor, options.Ports...).
		FloorFrom(built.floor).
		GovernFields(built.settings.Ownership).
		RememberManifestsIn(built.store).
		// The read side, so a read can be governed before it resolves. Without this every read
		// is refused — the fail-closed direction for a control whose absence is invisible.
		ReadFootprintsFrom(built.service).
		AwaitApprovalIn(built.approvals).
		PlaceOnLive(built.placer).
		TransformChangesWith(options.Transform)
}

// managerParts is what [Run] has already built by the time it builds the manager: the pieces
// that come from flags, files and the live reloaders rather than from [Options].
//
// One value rather than seven parameters, because seven positional parameters of which two are
// bare func types is a call site where swapping floor and placer still compiles. Named fields
// make each one say what it is at the call site.
type managerParts struct {
	door      mantlekeep.Submitter
	settings  config.Config
	service   *estate.Service
	store     estate.ManifestStore
	approvals estate.Approvals
	floor     func() estate.Floor
	placer    func() *estate.Placer
}

// approvalsFor chooses where gated changes wait.
//
// Extracted from [Run] for the same reason [managerFor] was: the failure worth catching is an
// option that exists, is documented, and is never read — and nothing inside Run is assertable. A
// deployment would then believe its approvals are durable while a restart still loses them.
func approvalsFor(options Options) estate.Approvals {
	if options.Approvals != nil {
		return options.Approvals
	}
	// Said out loud rather than left in a comment: a restart forgets every pending change, and
	// somebody who was asked to sign one off will find it gone.
	slog.Warn("approvals are held IN MEMORY — a restart will lose every pending approval, and " +
		"the person who was asked to sign will find it gone. Supply serve.Options.Approvals " +
		"with a durable store before this governs anything that matters.")
	return estate.NewMemoryApprovals()
}
