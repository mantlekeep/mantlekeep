// Command mantlekeep-estate-cli validates and submits an estate manifest from a terminal.
//
// # Why a CLI exists
//
// The engine speaks JSON and holds no YAML parser, which is what keeps it dependency-free.
// That leaves the person who actually writes a manifest with nowhere to check their work:
// the first thing that reads their file is a service, over HTTP, after they have already
// asked for a change. A mistyped key or a misplaced indent becomes a refusal from a server
// instead of a message beside the line that caused it.
//
// This is the edge that converts. It accepts YAML — including KYAML, which is a restricted
// YAML dialect and therefore parses with any YAML reader — turns it into JSON, and hands it
// to the SAME strict parser the service uses. What passes here is what the service accepts,
// because it is the identical code path, not a reimplementation of the rules.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/internal/safepath"
	"sigs.k8s.io/yaml"
)

const usage = `mantlekeep-estate-cli — check an estate manifest before a service sees it

  validate <file>            parse and check a manifest, printing what it resolves to
  submit   <file>            validate, then POST it to an estate
  json     <file>            print the manifest as the JSON the service receives

Flags:
  -estate  URL of the estate service (submit only)
  -team    team the manifest belongs to (submit only; defaults to the manifest's own)
  -user    caller identity, sent as the gateway would set it (submit only)

A file of "-" reads standard input, so this works in a pipe.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		// One line, to stderr, no stack. A person running this in a terminal wants the
		// problem, not a trace of the program that found it.
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		fmt.Print(usage)
		return nil
	}
	command, rest := arguments[0], arguments[1:]

	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	estateURL := flags.String("estate", "", "URL of the estate service")
	team := flags.String("team", "", "team the manifest belongs to")
	user := flags.String("user", "", "caller identity")
	// Flags are lifted out before parsing, so they work on EITHER side of the filename.
	// Go's flag package stops at the first non-flag argument, which would make
	// `submit app.yaml -user me` silently ignore -user — a CLI that quietly drops the
	// identity it refuses to run without is worse than one that cannot parse at all.
	positional, flagArguments := splitArguments(rest)
	if err := flags.Parse(flagArguments); err != nil {
		return err
	}
	file := ""
	if len(positional) > 0 {
		file = positional[0]
	}

	switch command {
	case "validate":
		return validate(file)
	case "json":
		return printJSON(file)
	case "submit":
		return submit(file, *estateURL, *team, *user)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q — run with no arguments for usage", command)
	}
}

// readManifest turns a YAML or JSON file into the manifest the service would parse.
//
// The conversion is YAML to JSON first, then the SERVICE's own strict parser. Going through
// the same ParseManifest is the whole point: a CLI that validated with its own rules would
// drift from the service, and the drift would show up as a manifest this accepts and the
// door refuses.
func readManifest(path string) (estate.Manifest, []byte, error) {
	document, err := readDocument(path)
	if err != nil {
		return estate.Manifest{}, nil, err
	}

	// YAMLToJSON accepts JSON unchanged, so a JSON manifest needs no separate path — and
	// KYAML, being a restricted YAML dialect, parses here like any other YAML.
	asJSON, err := yaml.YAMLToJSON(document)
	if err != nil {
		return estate.Manifest{}, nil, fmt.Errorf("%s is not valid YAML: %w", describe(path), err)
	}

	manifest, err := estate.ParseManifest(asJSON)
	if err != nil {
		// The service's own words. An unknown field is named here rather than paraphrased,
		// because the name IS the fix.
		return estate.Manifest{}, nil, fmt.Errorf("%s: %w", describe(path), err)
	}
	return manifest, asJSON, nil
}

func readDocument(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("no manifest given — pass a file, or \"-\" to read stdin")
	}
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	clean, err := safepath.Clean(path)
	if err != nil {
		return nil, err
	}
	// #nosec G304,G703 -- the path is typed by the person running this command, and
	// safepath.Clean has already refused one that walks upward.
	return os.ReadFile(clean)
}

func describe(path string) string {
	if path == "-" {
		return "standard input"
	}
	return path
}

// validate reports what the manifest says, so a person can see that the file they wrote is
// the estate they meant. Parsing alone proves the syntax; printing the result proves the
// meaning.
func validate(path string) error {
	manifest, _, err := readManifest(path)
	if err != nil {
		return err
	}
	fmt.Printf("ok — team %q, tier %q, owns %q\n", manifest.Team, manifest.Tier, manifest.Owns)
	for _, app := range manifest.Apps {
		fmt.Printf("  app  %-24s runtime=%-12s env=%-6s purpose=%-6s residency=%s\n",
			app.Name, app.Runtime, app.Placement.Env, app.Placement.Purpose,
			app.Placement.Residency)
	}
	if len(manifest.Apps) == 0 {
		// Not an error: an empty estate is a legal thing to declare. Saying so out loud
		// stops it being mistaken for a file that failed to load.
		fmt.Println("  (no apps declared)")
	}
	return nil
}

// printJSON shows exactly what the service receives, which is what makes a disagreement
// between this and the door diagnosable rather than mysterious.
func printJSON(path string) error {
	_, asJSON, err := readManifest(path)
	if err != nil {
		return err
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, asJSON, "", "  "); err != nil {
		return err
	}
	fmt.Println(indented.String())
	return nil
}

// submit validates locally, then sends the manifest to an estate.
//
// Validation first, always. Sending a file that cannot parse spends a governed request to
// learn something a local parse would have said instantly, and puts a refusal on the chain
// that records a typo rather than a decision.
func submit(path, estateURL, team, user string) error {
	if estateURL == "" {
		return fmt.Errorf("submit needs -estate <url>")
	}
	if user == "" {
		return fmt.Errorf("submit needs -user <id>: the estate records WHO asked, and this " +
			"CLI will not send a change with nobody's name on it")
	}
	manifest, asJSON, err := readManifest(path)
	if err != nil {
		return err
	}
	if team == "" {
		team = manifest.Team
	}
	if team == "" {
		return fmt.Errorf("the manifest names no team, and none was given with -team")
	}

	base, err := estateEndpoint(estateURL)
	if err != nil {
		return err
	}
	endpoint := base + "/api/estate/" + team
	// #nosec G704 -- the destination is an operator-supplied flag, checked by
	// estateEndpoint: http or https only, with a host. A CLI whose whole purpose is to
	// post where the operator says cannot avoid taking the address from the operator.
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(asJSON))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(estateWebUserHeader, user)

	client := &http.Client{Timeout: 30 * time.Second}
	// #nosec G704 -- same operator-supplied address, already checked by estateEndpoint.
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("the estate at %s did not answer: %w", estateURL, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, _ := io.ReadAll(response.Body)
	var indented bytes.Buffer
	if json.Indent(&indented, body, "", "  ") == nil {
		body = indented.Bytes()
	}
	fmt.Println(string(body))

	// The three outcomes are DIFFERENT and the exit code says which. A pending approval is
	// not a failure — scripting this must be able to tell "waiting for a person" apart from
	// "refused", or a pipeline will treat an approval as an outage.
	switch {
	case response.StatusCode == http.StatusConflict:
		fmt.Fprintln(os.Stderr, "pending: a person must approve this before it applies")
		os.Exit(2)
	case response.StatusCode >= 400:
		return fmt.Errorf("the estate refused this change (HTTP %d)", response.StatusCode)
	}
	return nil
}

// estateWebUserHeader mirrors the service's caller header. Named here rather than imported
// because api is the service's transport and this is a client of it.
const estateWebUserHeader = "X-Caller"

// estateEndpoint checks the address before anything is sent to it.
//
// The URL is a flag, so it is only as good as what the operator typed — and a CLI that will
// happily POST a manifest to file:// or to a scheme nobody expected is a credential and a
// change looking for somewhere to go. Restricting it to http and https, and requiring a
// host, is the smallest check that makes the flag mean what it looks like it means.
func estateEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSuffix(raw, "/"))
	if err != nil {
		return "", fmt.Errorf("-estate %q is not a URL: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("-estate must be http or https, not %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("-estate %q names no host", raw)
	}
	return parsed.String(), nil
}

// splitArguments separates positional arguments from flags and their values.
//
// It knows which flags take a value from the flag set's own defaults rather than a second
// list, so adding a flag cannot silently break the split.
func splitArguments(arguments []string) (positional, flagArguments []string) {
	valueExpected := false
	for _, argument := range arguments {
		switch {
		case valueExpected:
			flagArguments = append(flagArguments, argument)
			valueExpected = false
		case strings.HasPrefix(argument, "-"):
			flagArguments = append(flagArguments, argument)
			// "-flag=value" carries its value; "-flag value" takes the next argument.
			valueExpected = !strings.Contains(argument, "=")
		default:
			positional = append(positional, argument)
		}
	}
	return positional, flagArguments
}
