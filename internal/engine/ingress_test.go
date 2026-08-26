package engine

import (
	"strings"
	"testing"

	"github.com/sanketsaurav/bastion/internal/config"
)

// ingressInput is the fixture with ingress enabled: web's endpoint goes
// public under a base domain.
func ingressInput(t *testing.T) *Input {
	t.Helper()
	in := fixtureInput(t)
	in.Box.Ingress = &config.Ingress{BaseDomain: "apps.example.com", ACMEEmail: "ops@example.com"}
	svc := in.Box.Services["web"]
	ep := svc.Endpoints["http"]
	ep.Visibility = "public"
	ep.Auth = "none"
	ep.VMPort = 0
	svc.Endpoints["http"] = ep
	in.Box.Services["web"] = svc
	if issues := config.ValidateBox(in.Box, in.Dir); len(issues) != 0 {
		t.Fatalf("ingress fixture must validate: %v", issues)
	}
	return in
}

func TestGenIngressGolden(t *testing.T) {
	in := ingressInput(t)
	caddyfile, compose, digest, err := GenIngress(in)
	if err != nil {
		t.Fatal(err)
	}
	if digest == "" || !strings.Contains(string(compose), digest) {
		t.Fatalf("digest must be stamped into the compose labels")
	}
	checkGolden(t, "ingress-Caddyfile", caddyfile)
	checkGolden(t, "ingress-compose.yaml", compose)

	// Same definition, same bytes: route changes are the only digest input.
	_, _, digest2, err := GenIngress(in)
	if err != nil || digest2 != digest {
		t.Fatalf("generation must be deterministic (%s vs %s, %v)", digest, digest2, err)
	}
}

func TestPlanIngressLifecycle(t *testing.T) {
	in := ingressInput(t)
	_, _, digest, err := GenIngress(in)
	if err != nil {
		t.Fatal(err)
	}

	fresh, err := BuildPlan(in, mustParseFacts(t, freshFactLines()))
	if err != nil {
		t.Fatal(err)
	}
	// Ingress deploys last: a private endpoint migrating off 80/443 must
	// release the host port before the proxy binds it.
	ids := actionIDs(fresh)
	if len(ids) == 0 || ids[len(ids)-1] != "ingress" {
		t.Errorf("fresh plan must deploy ingress after service actions, got %v", ids)
	}

	// Converged: running container, current digest, ports bound → no action.
	lines := append(convergedFactLines(t, in), "@f ingx present running "+digest+" bound")
	plan, err := BuildPlan(in, mustParseFacts(t, lines))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(actionIDs(plan), "|"); strings.Contains(got, "ingress") {
		t.Errorf("converged ingress must not replan, got %v", actionIDs(plan))
	}

	// Running with the right digest but unprogrammed bindings → recreate.
	lines = append(convergedFactLines(t, in), "@f ingx present running "+digest+" unbound")
	plan, err = BuildPlan(in, mustParseFacts(t, lines))
	if err != nil {
		t.Fatal(err)
	}
	rebound := false
	for _, act := range plan.Actions {
		if act.Kind == KindIngress && strings.Contains(act.Summary, "port bindings missing") {
			rebound = true
		}
	}
	if !rebound {
		t.Errorf("an unbound proxy must be recreated, got %v", actionIDs(plan))
	}

	// Route drift: stale digest → replace.
	lines = append(convergedFactLines(t, in), "@f ingx present running olddigest")
	plan, err = BuildPlan(in, mustParseFacts(t, lines))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, act := range plan.Actions {
		if act.Kind == KindIngress && strings.Contains(act.Summary, "routes changed") {
			found = true
		}
	}
	if !found {
		t.Errorf("digest drift must replace the proxy, got %v", actionIDs(plan))
	}

	// No public endpoints left but the container exists → destructive removal.
	bare := fixtureInput(t)
	lines = append(convergedFactLines(t, bare), "@f ingx present running "+digest)
	plan, err = BuildPlan(bare, mustParseFacts(t, lines))
	if err != nil {
		t.Fatal(err)
	}
	var removal *Action
	for i := range plan.Actions {
		if plan.Actions[i].Kind == KindIngressRemove {
			removal = &plan.Actions[i]
		}
	}
	if removal == nil || !removal.Destructive {
		t.Fatalf("stale ingress must be removed destructively, got %v", actionIDs(plan))
	}
	if !strings.Contains(removal.Summary, "certificate state is retained") {
		t.Errorf("removal must promise cert retention: %s", removal.Summary)
	}
}
