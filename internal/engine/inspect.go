package engine

import (
	"github.com/sanketsaurav/bastion/internal/config"
)

// GenInspectScript builds the read-only inspection program. It never mutates
// the guest: `bastion plan` runs exactly this and nothing else.
func GenInspectScript(in *Input) []byte {
	w := &scriptWriter{}
	w.header()
	box := in.Box
	state := in.stateDir()

	// Guest identity and privilege.
	w.raw(`. /etc/os-release 2>/dev/null || true
f os "${ID:-unknown}" "${VERSION_ID:-0}" "$(uname -m)"
if sudo -n true 2>/dev/null; then f sudo ok; else f sudo missing; fi
`)
	// Read-only docker helper: try without sudo (docker group), then with.
	w.raw(`dq() { docker "$@" 2>/dev/null || sudo -n docker "$@" 2>/dev/null; }
`)

	if box.Host != nil {
		for _, pkg := range box.Host.Packages {
			w.linef(`if [ "$(dpkg-query -W -f='${db:Status-Status}' %s 2>/dev/null)" = installed ]; then f pkg %s installed; else f pkg %s absent; fi`,
				q(pkg), q(pkg), q(pkg))
		}
		files := box.Host.Files
		if box.Host.Shell != nil {
			// The generated shell.sh flows through the same file facts.
			files = append(files[:len(files):len(files)], config.ManagedFile{Target: ShellTarget})
			w.linef(`if grep -Fq %s "$HOME/.bashrc" 2>/dev/null; then f shline present; else f shline absent; fi`,
				q(shellLineMarker))
		}
		for _, mf := range files {
			t := targetExpr(mf.Target)
			bt := q(b64([]byte(mf.Target)))
			w.linef(`t=%s
if [ -e "$t" ]; then
  if [ -r "$t" ]; then sha=$(sha256sum <"$t" | cut -d' ' -f1); else sha=unreadable; fi
  f file %s present "$sha" "$(stat -c %%a "$t" 2>/dev/null || echo '?')"
else f file %s absent; fi
if [ -e "$t.bastion-backup" ]; then f bak %s present; else f bak %s absent; fi
m=$(cat %s 2>/dev/null || true); [ -n "$m" ] && f marker file %s "$(eb "$m")"`,
				t, bt, bt, bt, bt,
				q(state+"/files/"+shortHash(mf.Target)+".json"), q(shortHash(mf.Target)))
		}
		for _, feat := range box.Host.Features {
			if feat.Local() {
				name := localFeatureName(feat.Uses)
				// dir is a pre-quoted shell expression ($HOME must expand).
				dir := localFeatureRemoteDir(in.BoxID, name)
				w.linef(`if [ -x %s/check ]; then
  if ( cd %s && ./check >/dev/null 2>&1 ); then f lcheck %s ok; else f lcheck %s needs; fi
else f lcheck %s needs; fi
m=$(cat %s 2>/dev/null || true); [ -n "$m" ] && f marker lfeature %s "$(eb "$m")"`,
					dir, dir, q(name), q(name), q(name),
					q(state+"/lfeatures/"+name+".json"), q(name))
				continue
			}
			def, ok := Builtins[feat.Uses]
			if !ok {
				continue // validation already rejected unknown names
			}
			w.raw(def.CheckBash)
			w.linef(`m=$(cat %s 2>/dev/null || true); [ -n "$m" ] && f marker feature %s "$(eb "$m")"`,
				q(state+"/features/"+def.Name+".json"), q(def.Name))
		}
	}

	// Container runtime, services, volumes.
	needsDocker := len(box.Services) > 0 || len(box.Volumes) > 0 || box.Ingress != nil
	if !needsDocker && box.Host != nil {
		for _, feat := range box.Host.Features {
			if feat.Uses == "docker" {
				needsDocker = true
			}
		}
	}
	if needsDocker {
		w.raw(`if command -v docker >/dev/null 2>&1; then
  f docker present "$(eb "$(dq --version | head -1)")" "$(eb "$(dq compose version --short || true)")"
else f docker absent; fi
`)
		w.linef(`if dq network inspect %s >/dev/null; then f network present; else f network absent; fi`, q(in.networkName()))
		// A container can run with its port bindings silently unprogrammed
		// after a failed first start, so bindings are a fact of their own —
		// "running" alone does not mean "serving".
		w.linef(`if dq container inspect %s >/dev/null; then
  st=$(dq inspect -f '{{.State.Status}}' %s); dg=$(dq inspect -f '{{index .Config.Labels "bastion.config-digest"}}' %s)
  pb=$(dq inspect -f '{{if index .NetworkSettings.Ports "443/tcp"}}bound{{else}}unbound{{end}}' %s)
  f ingx present "$st" "${dg:-none}" "${pb:-unbound}"
else f ingx absent; fi`, q(in.ingressName()), q(in.ingressName()), q(in.ingressName()), q(in.ingressName()))
	}
	for _, name := range sortedKeys(box.Services) {
		cname := in.containerName(name)
		w.linef(`if dq container inspect %s >/dev/null; then
  st=$(dq inspect -f '{{.State.Status}}' %s); hl=$(dq inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' %s)
  dg=$(dq inspect -f '{{index .Config.Labels "bastion.config-digest"}}' %s); im=$(dq inspect -f '{{.Config.Image}}' %s)
  f svc %s present "$st" "$hl" "${dg:-none}" "$(eb "$im")"
else f svc %s absent; fi
m=$(cat %s 2>/dev/null || true); [ -n "$m" ] && f marker service %s "$(eb "$m")"`,
			q(cname), q(cname), q(cname), q(cname), q(cname), q(name), q(name),
			q(state+"/services/"+name+".json"), q(name))
		if _, hasSecrets := serviceSecretRefs(box, name); hasSecrets {
			w.linef(`if sudo -n test -e %s 2>/dev/null; then f sec %s present; else f sec %s absent; fi`,
				q(in.secretEnvPath(name)), q(name), q(name))
		}
	}
	if len(box.Services) > 0 {
		// Discover bastion-owned containers no longer declared. Only
		// label-carrying containers are ever considered (SPEC.md §9.7).
		w.linef(`dq ps -a --filter label=bastion.box-id=%s --format '{{.Label "bastion.service"}}' | while IFS= read -r s; do
  case "$s" in (*[!a-z0-9-]*|"") ;; (*) f osvc "$s";; esac
done`, q(in.BoxID))
	}
	for _, name := range sortedKeys(box.Volumes) {
		vol := box.Volumes[name]
		if vol.Persistence == "durable" {
			w.linef(`if [ -d %s ]; then f dvol %s present; else f dvol %s absent; fi`,
				q(in.durableVolumeDir(name)), q(name), q(name))
		} else {
			w.linef(`if dq volume inspect %s >/dev/null; then f evol %s present; else f evol %s absent; fi`,
				q(in.ephemeralVolume(name)), q(name), q(name))
		}
	}
	// Orphaned durable volume directories (reported, never touched).
	w.linef(`if [ -d %s ]; then for d in %s/*/; do [ -d "$d" ] && f odvol "$(basename "$d")"; done; fi`,
		q(box.Workspace.DataRoot+"/volumes"), q(box.Workspace.DataRoot+"/volumes"))
	// Every feature marker on the box, declared or not — the plan reports
	// the undeclared ones as orphans (installed by bastion, no longer
	// managed). Same label-guard pattern as the service sweep.
	w.linef(`for mf in %s/features/*.json %s/lfeatures/*.json; do
  [ -e "$mf" ] || continue
  n=$(basename "$mf" .json)
  case "$n" in (*[!a-z0-9-]*|"") continue;; esac
  case "$mf" in (*/lfeatures/*) f lmark "$n";; (*) f fmark "$n";; esac
done`, q(state), q(state))
	// Apt prerequisites bastion recorded installing for a feature. Package
	// names allow apt's wider charset (g++, libfoo1.2) — still protocol-safe.
	w.linef(`for pf in %s/prereqs/*/*.json; do
  [ -e "$pf" ] || continue
  pd=$(basename "$(dirname "$pf")"); pn=$(basename "$pf" .json)
  case "$pd" in (*[!a-z0-9-]*|"") continue;; esac
  case "$pn" in (*[!a-z0-9.+-]*|"") continue;; esac
  f pmark "$pd" "$pn"
done`, q(state))

	w.raw("f end ok\n")
	w.raw("exit 0\n")
	return w.bytes()
}

// serviceSecretRefs returns the sorted secret names a service's environment
// references and whether there are any.
func serviceSecretRefs(box *config.Box, svc string) ([]string, bool) {
	s, ok := box.Services[svc]
	if !ok {
		return nil, false
	}
	seen := map[string]bool{}
	for _, v := range s.Environment {
		if !v.IsLiteral && v.SecretRef != "" {
			seen[v.SecretRef] = true
		}
	}
	if len(seen) == 0 {
		return nil, false
	}
	return sortedKeys(seen), true
}
