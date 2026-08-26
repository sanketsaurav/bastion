package engine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/sanketsaurav/bastion/internal/config"
)

// renderTemplate renders a template-mode managed file locally at plan time.
// The context is non-secret box metadata only — secret values are never
// available to templates (SPEC.md §8.5).
func renderTemplate(in *Input, source string, content []byte) ([]byte, error) {
	tmpl, err := template.New(filepath.Base(source)).Option("missingkey=error").Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("parsing template %q: %w", source, err)
	}
	data := map[string]any{
		"Box": map[string]any{
			"Name":   in.Box.Metadata.Name,
			"Labels": in.Box.Metadata.Labels,
		},
		"Provider": map[string]any{
			"Project":  in.Box.Provider.Project,
			"Zone":     in.Box.Provider.Zone,
			"Instance": in.Box.Provider.Instance,
		},
		"Workspace": map[string]any{
			"Mount":    in.Box.Workspace.Mount,
			"DataRoot": in.Box.Workspace.DataRoot,
		},
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering template %q: %w", source, err)
	}
	return buf.Bytes(), nil
}

// BuildPlan diffs the declared definition against observed facts and returns
// the ordered action list. facts == nil means the guest was not inspected
// (box stopped): the plan reports guest state as unknown instead of guessing.
func BuildPlan(in *Input, facts *Facts) (*Plan, error) {
	plan := &Plan{BoxID: in.BoxID}
	box := in.Box

	if facts == nil {
		plan.GuestUnknown = true
		return plan, nil
	}
	if !facts.Complete {
		return nil, fmt.Errorf("guest inspection output was truncated; rerun with --verbose to see the raw transport output")
	}
	if err := facts.GuestSupported(); err != nil {
		return nil, err
	}
	if !facts.SudoOK {
		plan.Warnings = append(plan.Warnings,
			"non-interactive sudo is unavailable; actions marked (root) will fail — grant NOPASSWD sudo or remove root-requiring configuration")
	}

	var actions []Action
	declaredFeatures := map[string]bool{}
	declaredLocalFeatures := map[string]bool{}

	// 1. Packages.
	if box.Host != nil {
		var missing []string
		for _, pkg := range box.Host.Packages {
			if !facts.Packages[pkg] {
				missing = append(missing, pkg)
			}
		}
		if len(missing) > 0 {
			actions = append(actions, Action{
				ID: "packages", Kind: KindPackages, RequiresRoot: true,
				Summary: fmt.Sprintf("install %d system package(s)", len(missing)),
				Detail:  []string{strings.Join(missing, ", ")},
				pkgs:    missing,
			})
		}

		// 2. Features, in declaration order.
		for _, feat := range box.Host.Features {
			if feat.Local() {
				declaredLocalFeatures[localFeatureName(feat.Uses)] = true
				act, err := planLocalFeature(in, facts, feat.Uses, feat.With)
				if err != nil {
					return nil, err
				}
				if act != nil {
					actions = append(actions, *act)
				}
				continue
			}
			def, ok := Builtins[feat.Uses]
			if !ok {
				return nil, fmt.Errorf("built-in feature %q is not implemented in this version", feat.Uses)
			}
			declaredFeatures[def.Name] = true
			if err := validateOptions(def, feat.With); err != nil {
				return nil, err
			}
			digest := optionsDigest(feat.With)
			marker, hasMarker := facts.Markers.Features[def.Name]
			installed := facts.Features[def.Name] != ""
			if installed && hasMarker && marker.Version == def.Version && marker.OptionsDigest == digest {
				continue
			}
			reason := "not installed"
			switch {
			case installed && !hasMarker:
				reason = "present but not yet managed by bastion"
			case installed && marker.OptionsDigest != digest:
				reason = "feature options changed"
			case installed && marker.Version != def.Version:
				reason = "feature definition updated"
			}
			actions = append(actions, Action{
				ID: "feature:" + def.Name, Kind: KindFeature, RequiresRoot: def.RequiresRoot,
				Summary: fmt.Sprintf("apply feature %s (%s)", def.Name, reason),
				feature: &featureAction{Name: def.Name, Def: def, With: feat.With, Digest: digest, Version: def.Version},
			})
		}

		// 3. Managed files.
		for _, mf := range box.Host.Files {
			act, err := planFile(in, facts, mf)
			if err != nil {
				return nil, err
			}
			if act != nil {
				actions = append(actions, *act)
			}
		}

		// 4. Shell integration: the managed shell.sh flows through the
		// ordinary file pipeline; the .bashrc source line is its own step.
		if box.Host.Shell != nil {
			if act := fileActionFromContent(facts, ShellTarget, shellContent(box.Host.Shell.Prompt), "0644"); act != nil {
				actions = append(actions, *act)
			}
			if !facts.ShellLine {
				actions = append(actions, Action{
					ID: "shell-line", Kind: KindShellLine,
					Summary: "add the bastion shell-integration line to ~/.bashrc",
				})
			}
		}
	}

	// 4. Runtime, volumes, secrets, services.
	if len(box.Services) > 0 || len(box.Volumes) > 0 {
		if !facts.Docker.Installed {
			hasDockerFeature := false
			if box.Host != nil {
				for _, feat := range box.Host.Features {
					if feat.Uses == "docker" {
						hasDockerFeature = true
					}
				}
			}
			if !hasDockerFeature {
				return nil, fmt.Errorf("services are declared but Docker is not installed; add `- uses: docker` to host.features")
			}
		}
		if !facts.Docker.NetworkExists {
			actions = append(actions, Action{
				ID: "network", Kind: KindNetwork, RequiresRoot: true,
				Summary: fmt.Sprintf("create container network %s", in.networkName()),
			})
		}
	}

	// Ingress removal runs before service actions: a private endpoint
	// migrating onto 80/443 needs the proxy's binding released first.
	pubs := box.PublicEndpoints()
	if (box.Ingress == nil || len(pubs) == 0) && facts.Ingress.Exists {
		actions = append(actions, Action{
			ID: "ingress-remove", Kind: KindIngressRemove, RequiresRoot: true, Destructive: true,
			Summary: "remove ingress proxy (no public endpoints declared; certificate state is retained)",
		})
	}
	for _, name := range sortedKeys(box.Volumes) {
		vol := box.Volumes[name]
		if vol.Persistence == "durable" && !facts.DurableVolumes[name] {
			actions = append(actions, Action{
				ID: "volume:" + name, Kind: KindVolume, RequiresRoot: true,
				Summary: fmt.Sprintf("create durable volume directory %s", in.durableVolumeDir(name)),
				volume:  &volumeAction{Name: name},
			})
		}
		if vol.Persistence == "ephemeral" && !facts.EphemeralVolumes[name] {
			actions = append(actions, Action{
				ID: "volume:" + name, Kind: KindVolume, RequiresRoot: true,
				Summary: fmt.Sprintf("create ephemeral volume %s", in.ephemeralVolume(name)),
				volume:  &volumeAction{Name: name, Ephemeral: true},
			})
		}
	}

	serviceActs, notes, err := planServices(in, facts)
	if err != nil {
		return nil, err
	}
	actions = append(actions, serviceActs...)
	plan.Notes = append(plan.Notes, notes...)

	// Ingress deploys after service actions: a private endpoint migrating
	// off 80/443 must release the host port before the proxy binds it.
	if box.Ingress != nil && len(pubs) > 0 {
		caddyfile, compose, digest, err := GenIngress(in)
		if err != nil {
			return nil, err
		}
		if !facts.Ingress.Exists || facts.Ingress.ConfigDigest != digest ||
			facts.Ingress.State != "running" || facts.Ingress.Health == "unbound" {
			reason := "create"
			switch {
			case facts.Ingress.Exists && facts.Ingress.ConfigDigest != digest:
				reason = "routes changed; container will be replaced"
			case facts.Ingress.Exists && facts.Ingress.State != "running":
				reason = "not running"
			case facts.Ingress.Exists && facts.Ingress.Health == "unbound":
				reason = "port bindings missing; container will be recreated"
			}
			detail := make([]string, 0, len(pubs))
			for _, pe := range pubs {
				detail = append(detail, fmt.Sprintf("https://%s → %s:%d", pe.Hostname, pe.Service, pe.ContainerPort))
			}
			actions = append(actions, Action{
				ID: "ingress", Kind: KindIngress, RequiresRoot: true,
				Summary: fmt.Sprintf("deploy ingress proxy (caddy) for %d public endpoint(s) (%s)", len(pubs), reason),
				Detail:  detail,
				ingress: &ingressAction{Caddyfile: caddyfile, Compose: compose, Digest: digest},
			})
		}
	}

	// Orphaned durable volume directories: reported, never deleted here.
	declaredDurable := map[string]bool{}
	for name, vol := range box.Volumes {
		if vol.Persistence == "durable" {
			declaredDurable[name] = true
		}
	}
	for _, name := range facts.OrphanDurable {
		if !declaredDurable[name] {
			plan.Notes = append(plan.Notes, fmt.Sprintf(
				"durable volume directory %q is orphaned (not declared); delete with `bastion volume delete %s %s`",
				name, in.BoxID, name))
		}
	}

	// Feature markers with no declaration: installed by bastion, no longer
	// managed. Reported only — apply never uninstalls on undeclare.
	for _, name := range facts.FeatureMarkerNames {
		if declaredFeatures[name] {
			continue
		}
		def, known := Builtins[name]
		switch {
		case known && len(def.RemovePaths) > 0:
			plan.Notes = append(plan.Notes, fmt.Sprintf(
				"feature %q was installed by bastion but is no longer declared; remove it with `bastion feature remove %s %s`, or redeclare it",
				name, in.BoxID, name))
		case known:
			plan.Notes = append(plan.Notes, fmt.Sprintf(
				"feature %q was installed by bastion but is no longer declared; it is apt-managed — remove it yourself (%s), or redeclare it",
				name, def.RemoveHint))
		default:
			plan.Notes = append(plan.Notes, fmt.Sprintf(
				"feature %q left bastion state on the box but is unknown to this version; its marker remains", name))
		}
	}
	for _, name := range facts.LocalFeatureMarkerNames {
		if !declaredLocalFeatures[name] {
			plan.Notes = append(plan.Notes, fmt.Sprintf(
				"local feature %q was applied by bastion but is no longer declared; its effects are user-defined and stay until you remove them", name))
		}
	}
	// Dangling prerequisites: recorded for a feature whose own marker is
	// gone (removed outside `feature remove`). A prerequisite whose feature
	// is declared or still marked is owned — its feature's path covers it.
	featureMarked := map[string]bool{}
	for _, name := range facts.FeatureMarkerNames {
		featureMarked[name] = true
	}
	for _, feature := range sortedKeys(facts.PrereqMarkers) {
		if declaredFeatures[feature] || featureMarked[feature] {
			continue
		}
		for _, pkg := range facts.PrereqMarkers[feature] {
			plan.Notes = append(plan.Notes, fmt.Sprintf(
				"package %q was installed by bastion as a prerequisite of feature %q, which is gone; remove it (sudo apt-get remove %s) or redeclare the feature",
				pkg, feature, pkg))
		}
	}

	// Bootstrap state directories first whenever any work is planned.
	if len(actions) > 0 {
		actions = append([]Action{{
			ID: "bootstrap", Kind: KindBootstrap, RequiresRoot: true,
			Summary: "ensure bastion state directories on the box",
		}}, actions...)
	}
	plan.Actions = actions
	return plan, nil
}

func planFile(in *Input, facts *Facts, mf config.ManagedFile) (*Action, error) {
	source, target, permissions := mf.Source, mf.Target, mf.Permissions
	content, err := os.ReadFile(filepath.Join(in.Dir, filepath.Clean(source)))
	if err != nil {
		return nil, fmt.Errorf("managed file source %q: %w", source, err)
	}
	if mf.Mode == "template" {
		if content, err = renderTemplate(in, source, content); err != nil {
			return nil, err
		}
	}
	return fileActionFromContent(facts, target, content, permissions), nil
}

// fileActionFromContent diffs desired file content against observed facts —
// shared by definition-supplied managed files and generated ones (shell.sh).
func fileActionFromContent(facts *Facts, target string, content []byte, permissions string) *Action {
	sha := sha256hex(content)
	fact := facts.Files[target]
	_, managed := facts.Markers.Files[target]

	contentChanged := !fact.Exists || fact.SHA256 != sha
	permsChanged := permissions != "" && fact.Exists && fact.Mode != strings.TrimPrefix(permissions, "0")
	if !contentChanged && !permsChanged {
		return nil
	}
	root := !strings.HasPrefix(target, "~")
	firstTouch := fact.Exists && !managed && !facts.Backups[target]

	var detail []string
	switch {
	case !fact.Exists:
		detail = append(detail, "create")
	case contentChanged && fact.SHA256 == "unreadable":
		detail = append(detail, "replace (current content unreadable)")
	case contentChanged:
		detail = append(detail, "replace content")
	}
	if permsChanged {
		detail = append(detail, fmt.Sprintf("permissions %s → %s", fact.Mode, strings.TrimPrefix(permissions, "0")))
	}
	if firstTouch {
		detail = append(detail, "existing unmanaged file: one backup kept at "+target+".bastion-backup")
	}
	return &Action{
		ID: "file:" + target, Kind: KindFile, RequiresRoot: root,
		Summary: "write " + target,
		Detail:  detail,
		file: &fileAction{
			Target: target, Content: content, SHA256: sha,
			Mode: permissions, Root: root, FirstTouch: firstTouch,
		},
	}
}

func planLocalFeature(in *Input, facts *Facts, uses string, with map[string]any) (*Action, error) {
	meta, dir, err := LoadLocalFeature(in.Dir, uses)
	if err != nil {
		return nil, err
	}
	tarball, sourceDigest, err := PackLocalFeature(dir)
	if err != nil {
		return nil, err
	}
	inputs, inputsDigest := localFeatureInputs(with)

	marker, hasMarker := facts.Markers.LocalFeatures[meta.Name]
	checkOK := facts.LocalFeatureCheck[meta.Name] == "ok"
	if hasMarker && marker.SourceDigest == sourceDigest && marker.InputsDigest == inputsDigest &&
		marker.Version == meta.Version && checkOK {
		return nil, nil
	}
	reason := "never applied"
	switch {
	case hasMarker && marker.SourceDigest != sourceDigest:
		reason = "feature source changed"
	case hasMarker && marker.InputsDigest != inputsDigest:
		reason = "feature inputs changed"
	case hasMarker && !checkOK:
		reason = "check reports drift"
	}
	return &Action{
		ID: "local-feature:" + meta.Name, Kind: KindLocalFeature,
		RequiresRoot: meta.RequiresRoot, LocalCode: true,
		Summary: fmt.Sprintf("apply local feature %s (%s) — locally supplied executable content", meta.Name, reason),
		lfeat: &localFeatureAction{
			Name: meta.Name, Meta: meta, TarGz: tarball,
			SourceDigest: sourceDigest, InputsJSON: inputs, InputsDigest: inputsDigest,
		},
	}, nil
}

func planServices(in *Input, facts *Facts) ([]Action, []string, error) {
	box := in.Box
	var actions []Action
	var notes []string

	for _, name := range serviceApplyOrder(box) {
		svc := box.Services[name]
		fact := facts.Services[name]

		if !svc.IsEnabled() {
			if fact.Exists && fact.State == "running" {
				actions = append(actions, Action{
					ID: "service-stop:" + name, Kind: KindServiceStop, RequiresRoot: true,
					Summary: fmt.Sprintf("stop disabled service %s", name),
					target:  name,
				})
			}
			continue
		}

		compose, digest, err := GenCompose(in, name)
		if err != nil {
			return nil, nil, err
		}
		needsApply := !fact.Exists || fact.ConfigDigest != digest || fact.State != "running"
		if mutableTag(svc.Image) {
			notes = append(notes, fmt.Sprintf(
				"service %q uses a mutable image reference (%s); pin a digest for reproducibility", name, svc.Image))
		}

		if refs, ok := serviceSecretRefs(box, name); ok {
			if needsApply || !facts.Secrets[name] {
				actions = append(actions, Action{
					ID: "secret:" + name, Kind: KindSecret, RequiresRoot: true,
					Summary: fmt.Sprintf("write secret env file for %s (%s)", name, strings.Join(refs, ", ")),
					secret:  &secretAction{Service: name, Refs: refs},
				})
			}
		}
		if !needsApply {
			continue
		}
		reason := "create"
		switch {
		case fact.Exists && fact.ConfigDigest != digest:
			reason = "configuration changed; container will be replaced"
		case fact.Exists && fact.State != "running":
			reason = "not running"
		}
		actions = append(actions, Action{
			ID: "service:" + name, Kind: KindService, RequiresRoot: true,
			Summary: fmt.Sprintf("deploy service %s (%s)", name, reason),
			Detail:  []string{"image " + svc.Image},
			service: &serviceAction{
				Name: name, Compose: compose, ConfigDigest: digest,
				Image: svc.Image, PullPolicy: svc.PullPolicy,
				HasHealth: svc.Healthcheck != nil,
			},
		})
		if svc.Healthcheck != nil {
			actions = append(actions, Action{
				ID: "health:" + name, Kind: KindServiceHealth, RequiresRoot: true,
				Summary: fmt.Sprintf("wait for %s to become healthy", name),
				target:  name,
			})
		}
	}

	// Orphaned bastion-owned services: destructive removal, durable data kept.
	declared := map[string]bool{}
	for name := range box.Services {
		declared[name] = true
	}
	seen := map[string]bool{}
	for _, name := range facts.Orphans {
		if declared[name] || seen[name] {
			continue
		}
		seen[name] = true
		actions = append(actions, Action{
			ID: "service-remove:" + name, Kind: KindServiceRemove,
			RequiresRoot: true, Destructive: true,
			Summary: fmt.Sprintf("remove undeclared service %s (container and generated config; durable volumes are retained)", name),
			target:  name,
		})
	}
	return actions, notes, nil
}
