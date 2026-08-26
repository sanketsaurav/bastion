package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ApplyOptions tunes apply-script generation.
type ApplyOptions struct {
	HealthTimeout time.Duration
	// SecretValues are resolved secret values (ResolveSecrets). Consumed only
	// here, embedded base64 into the script that transits SSH stdin — never
	// argv, never logs, never local state (SPEC.md §9.4).
	SecretValues map[string]string
}

// GenApplyScript renders the apply program for a plan. Steps execute in plan
// order; the first failure stops the script after emitting its event, leaving
// earlier successes and their markers in place (resumable, SPEC.md §10).
func GenApplyScript(in *Input, plan *Plan, opts ApplyOptions) ([]byte, error) {
	if opts.HealthTimeout <= 0 {
		opts.HealthTimeout = 2 * time.Minute
	}
	w := &scriptWriter{}
	w.header()
	w.runFn()
	w.remoteLock(in.BoxID)

	for i := range plan.Actions {
		act := &plan.Actions[i]
		name := fmt.Sprintf("s%d", i)
		body, err := stepBody(in, act, opts)
		if err != nil {
			return nil, fmt.Errorf("action %s: %w", act.ID, err)
		}
		w.linef("%s() {", name)
		w.raw(indent(body))
		w.linef("}")
		// Steps are identified positionally in the event protocol; action IDs
		// may contain characters unsafe for a space-delimited line format.
		w.linef("run a%d %s", i, name)
	}
	w.raw("printf '@e apply done\\n'\nexit 0\n")
	return w.bytes(), nil
}

func indent(body string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

func stepBody(in *Input, act *Action, opts ApplyOptions) (string, error) {
	switch act.Kind {
	case KindBootstrap:
		return bootstrapBody(in), nil
	case KindPackages:
		return aptGet + " update -qq\n" +
			aptGet + " install -y " + shellJoinQuoted(act.pkgs) + "\n", nil
	case KindFeature:
		return featureBody(in, act.feature)
	case KindLocalFeature:
		return localFeatureBody(in, act.lfeat)
	case KindFile:
		return fileBody(in, act.file), nil
	case KindShellLine:
		return shellLineBody(), nil
	case KindNetwork:
		return fmt.Sprintf("dk network inspect %s >/dev/null 2>&1 || dk network create --label bastion.box-id=%s %s\n",
			q(in.networkName()), q(in.BoxID), q(in.networkName())), nil
	case KindIngress:
		return ingressBody(in, act.ingress), nil
	case KindIngressRemove:
		return ingressRemoveBody(in), nil
	case KindVolume:
		if act.volume.Ephemeral {
			ev := in.ephemeralVolume(act.volume.Name)
			return fmt.Sprintf("dk volume inspect %s >/dev/null 2>&1 || dk volume create --label bastion.box-id=%s %s\n",
				q(ev), q(in.BoxID), q(ev)), nil
		}
		return fmt.Sprintf("sudo -n mkdir -p %s\n", q(in.durableVolumeDir(act.volume.Name))), nil
	case KindSecret:
		return secretBody(in, act.secret, opts)
	case KindService:
		return serviceBody(in, act.service)
	case KindServiceHealth:
		return healthBody(in, act.target, opts.HealthTimeout), nil
	case KindServiceStop:
		return fmt.Sprintf("dk stop %s\n", q(in.containerName(act.target))), nil
	case KindServiceRemove:
		return serviceRemoveBody(in, act.target), nil
	default:
		return "", fmt.Errorf("unknown action kind %q", act.Kind)
	}
}

func bootstrapBody(in *Input) string {
	state := in.stateDir()
	return "sudo -n mkdir -p " + shellJoinQuoted([]string{
		state + "/files", state + "/features", state + "/lfeatures", state + "/services",
		in.servicesDir(), in.secretsDir(),
	}) + "\n" +
		"sudo -n chmod 700 " + q(in.secretsDir()) + "\n" +
		"mkdir -p \"$HOME/.cache/bastion\"\n"
}

func featureBody(in *Input, fa *featureAction) (string, error) {
	body, err := fa.Def.ApplyBash(fa.With)
	if err != nil {
		return "", err
	}
	marker, _ := json.Marshal(FeatureMarker{Name: fa.Name, Version: fa.Version, OptionsDigest: fa.Digest})
	return aptPrereqBash(in, fa.Def) + body + markerWrite(in, "features", fa.Name, marker), nil
}

// aptPrereqBash installs a feature's missing apt prerequisites, recording a
// marker for each one it installs — and only those: a prerequisite already
// on the box is never claimed, so removing the feature never touches it.
func aptPrereqBash(in *Input, def *Builtin) string {
	var b strings.Builder
	for _, pkg := range def.AptPrereqs {
		dir := in.stateDir() + "/prereqs/" + def.Name
		marker, _ := json.Marshal(PrereqMarker{Package: pkg, Feature: def.Name})
		fmt.Fprintf(&b, `if [ "$(dpkg-query -W -f='${db:Status-Status}' %s 2>/dev/null)" = installed ]; then :; else
%s update -qq
%s install -y %s
sudo -n mkdir -p %s
%sfi
`, q(pkg), aptGet, aptGet, q(pkg), q(dir), markerWrite(in, "prereqs/"+def.Name, pkg, marker))
	}
	return b.String()
}

func localFeatureBody(in *Input, lf *localFeatureAction) (string, error) {
	dir := localFeatureRemoteDir(in.BoxID, lf.Name)
	sudo := ""
	if lf.Meta.RequiresRoot {
		sudo = "sudo -n "
	}
	marker, _ := json.Marshal(LocalFeatureMarker{
		Name: lf.Name, Version: lf.Meta.Version,
		SourceDigest: lf.SourceDigest, InputsDigest: lf.InputsDigest,
	})
	return fmt.Sprintf(`d=%s
rm -rf "$d" && mkdir -p "$d"
printf %%s %s | base64 -d | tar -xzf - -C "$d"
printf %%s %s | base64 -d > "$d/inputs.json"
chmod +x "$d/check" "$d/apply" 2>/dev/null || true
( cd "$d" && %s./apply inputs.json )
`, dir, q(b64(lf.TarGz)), q(b64(lf.InputsJSON)), sudo) + markerWrite(in, "lfeatures", lf.Name, marker), nil
}

func fileBody(in *Input, fa *fileAction) string {
	sudo := ""
	if fa.Root {
		sudo = "sudo -n "
	}
	mode := fa.Mode
	if mode == "" {
		mode = "0644"
	}
	marker, _ := json.Marshal(FileMarker{Target: fa.Target, SHA256: fa.SHA256, Mode: fa.Mode, Backup: fa.FirstTouch})
	var b strings.Builder
	fmt.Fprintf(&b, "t=%s\n", targetExpr(fa.Target))
	b.WriteString("tmp=$(mktemp) || return 1\n")
	fmt.Fprintf(&b, "printf %%s %s | base64 -d > \"$tmp\" || return 1\n", q(b64(fa.Content)))
	fmt.Fprintf(&b, "chmod %s \"$tmp\" || return 1\n", q(mode))
	if fa.FirstTouch {
		fmt.Fprintf(&b, "if [ -e \"$t\" ] && ! [ -e \"$t.bastion-backup\" ]; then %scp -p \"$t\" \"$t.bastion-backup\" || return 1; fi\n", sudo)
	}
	fmt.Fprintf(&b, "%smkdir -p \"$(dirname \"$t\")\" || return 1\n", sudo)
	fmt.Fprintf(&b, "%smv -f \"$tmp\" \"$t\" || return 1\n", sudo)
	if fa.Root {
		b.WriteString("sudo -n chown root:root \"$t\" || return 1\n")
	}
	b.WriteString(markerWrite(in, "files", shortHash(fa.Target), marker))
	return b.String()
}

func secretBody(in *Input, sa *secretAction, opts ApplyOptions) (string, error) {
	content, err := BuildSecretEnvFile(in.Box, sa.Service, opts.SecretValues)
	if err != nil {
		return "", err
	}
	path := in.secretEnvPath(sa.Service)
	return fmt.Sprintf(`printf %%s %s | base64 -d | sudo -n tee %s >/dev/null
sudo -n chmod 0600 %s
`, q(b64(content)), q(path), q(path)), nil
}

func serviceBody(in *Input, sa *serviceAction) (string, error) {
	dir := in.servicesDir() + "/" + sa.Name
	pull := map[string]string{"if-not-present": "missing", "always": "always", "never": "never"}[sa.PullPolicy]
	if pull == "" {
		pull = "missing"
	}
	marker, _ := json.Marshal(ServiceMarker{Name: sa.Name, ConfigDigest: sa.ConfigDigest, Image: sa.Image})
	return fmt.Sprintf(`sudo -n mkdir -p %s
printf %%s %s | base64 -d | sudo -n tee %s >/dev/null
dk compose -p %s -f %s up -d --pull %s --remove-orphans
`, q(dir), q(b64(sa.Compose)), q(in.composePath(sa.Name)),
		q(in.projectName(sa.Name)), q(in.composePath(sa.Name)), pull) + markerWrite(in, "services", sa.Name, marker), nil
}

func healthBody(in *Input, svc string, timeout time.Duration) string {
	return fmt.Sprintf(`end=$(( $(date +%%s) + %d ))
while :; do
  h=$(dk inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}healthy{{end}}' %s 2>/dev/null || echo error)
  case "$h" in
    healthy) exit 0 ;;
    unhealthy) echo "service %s is unhealthy"; exit 1 ;;
  esac
  if [ "$(date +%%s)" -gt "$end" ]; then echo "timed out waiting for %s to become healthy (last: $h)"; exit 1; fi
  sleep 3
done
`, int(timeout.Seconds()), q(in.containerName(svc)), svc, svc)
}

func serviceRemoveBody(in *Input, svc string) string {
	dir := in.servicesDir() + "/" + svc
	return fmt.Sprintf(`dk rm -f %s 2>/dev/null || true
sudo -n rm -rf %s
sudo -n rm -f %s %s
echo "removed service %s; durable volumes were retained"
`, q(in.containerName(svc)), q(dir),
		q(in.secretEnvPath(svc)), q(in.stateDir()+"/services/"+svc+".json"), svc)
}

// ingressBody writes the generated Caddyfile and Compose project, then
// converges the proxy container. --force-recreate because this step only
// runs on drift: restarting a container whose previous start failed to
// program its port bindings can come up "running" with none (observed
// live) — a fresh create either binds or fails loudly.
func ingressBody(in *Input, ia *ingressAction) string {
	dir := in.ingressDir()
	return fmt.Sprintf(`sudo -n mkdir -p %s %s %s
printf %%s %s | base64 -d | sudo -n tee %s >/dev/null
printf %%s %s | base64 -d | sudo -n tee %s >/dev/null
dk compose -p %s -f %s up -d --pull missing --remove-orphans --force-recreate
`, q(dir), q(in.ingressDataDir()+"/data"), q(in.ingressDataDir()+"/config"),
		q(b64(ia.Caddyfile)), q(dir+"/Caddyfile"),
		q(b64(ia.Compose)), q(dir+"/compose.yaml"),
		q(in.ingressName()), q(dir+"/compose.yaml"))
}

// ingressRemoveBody stops the proxy and removes its generated config.
// Certificate state on the data root is deliberately retained: re-enabling
// ingress must not re-issue certificates.
func ingressRemoveBody(in *Input) string {
	return fmt.Sprintf(`dk compose -p %s -f %s down --remove-orphans 2>/dev/null || dk rm -f %s 2>/dev/null || true
sudo -n rm -rf %s
echo "ingress proxy removed; certificate state under %s is retained"
`, q(in.ingressName()), q(in.ingressDir()+"/compose.yaml"), q(in.ingressName()),
		q(in.ingressDir()), in.ingressDataDir())
}

// shellLineBody appends the delimited shell-integration line to ~/.bashrc
// once — the only edit bastion ever makes to a shell startup file (SPEC.md
// §8.5). Appended at the end so it wins over the distribution's own PS1.
func shellLineBody() string {
	return `if ! grep -Fq ` + q(shellLineMarker) + ` "$HOME/.bashrc" 2>/dev/null; then
  printf '\n%s\n' ` + q(shellLine) + ` >> "$HOME/.bashrc"
fi
echo "shell integration line ensured in ~/.bashrc"
`
}

// markerWrite records completion state atomically as the step's last act.
func markerWrite(in *Input, kind, key string, markerJSON []byte) string {
	dir := in.stateDir() + "/" + kind
	tmp := dir + "/." + key + ".tmp"
	final := dir + "/" + key + ".json"
	return fmt.Sprintf("printf %%s %s | base64 -d | sudo -n tee %s >/dev/null && sudo -n mv %s %s\n",
		q(b64(markerJSON)), q(tmp), q(tmp), q(final))
}

func shellJoinQuoted(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = q(s)
	}
	return strings.Join(quoted, " ")
}
