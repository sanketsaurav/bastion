package engine

import (
	"fmt"
	"regexp"
)

// Builtin is a host feature Bastion knows how to check and apply. CheckBash
// is read-only and emits `@f feat <name> …`; ApplyBash returns an idempotent
// body safe to rerun after partial failure. Bump Version to force reapply on
// upgraded feature definitions.
type Builtin struct {
	Name         string
	Version      string
	RequiresRoot bool
	Options      []string // allowed keys in `with`
	CheckBash    string
	ApplyBash    func(with map[string]any) (string, error)
}

var versionOptRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// versionOpt extracts and validates an optional `version` option.
func versionOpt(with map[string]any) (string, error) {
	raw, ok := with["version"]
	if !ok {
		return "", nil
	}
	s, ok := raw.(string)
	if !ok || !versionOptRe.MatchString(s) {
		return "", fmt.Errorf("version option must be a plain version string, got %v", raw)
	}
	return s, nil
}

func validateOptions(def *Builtin, with map[string]any) error {
	allowed := map[string]bool{}
	for _, k := range def.Options {
		allowed[k] = true
	}
	for k := range with {
		if !allowed[k] {
			return fmt.Errorf("feature %q does not accept option %q (allowed: %v)", def.Name, k, def.Options)
		}
	}
	return nil
}

// aptGet is the canonical apt invocation: non-interactive, and waiting out
// dpkg locks held by cloud-init or unattended-upgrades instead of failing a
// first apply on a freshly booted box.
const aptGet = "sudo -n DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120"

func aptCheck(name, binary string) string {
	return fmt.Sprintf(`if command -v %s >/dev/null 2>&1; then f feat %s "$(eb "$(%s -V 2>/dev/null || %s --version 2>/dev/null | head -1 || echo present)")"; else f feat %s absent; fi
`, q(binary), q(name), q(binary), q(binary), q(name))
}

func aptApply(pkgs string) func(map[string]any) (string, error) {
	return func(map[string]any) (string, error) {
		return aptGet + " update -qq\n" + aptGet + " install -y " + pkgs + "\n", nil
	}
}

// userBinCheck probes the conventional user-level install locations for
// binary. The fact is reported under feat — the feature name, which plan
// looks up — never the binary name; the two differ (claude-code/claude).
func userBinCheck(feat, binary string, paths ...string) string {
	cond := ""
	for _, p := range paths {
		if cond != "" {
			cond += " || "
		}
		cond += fmt.Sprintf(`[ -x "$HOME"/%s ]`, q(p))
	}
	return fmt.Sprintf(`if %s || command -v %s >/dev/null 2>&1; then f feat %s present; else f feat %s absent; fi
`, cond, q(binary), q(feat), q(feat))
}

// installerApply runs an official HTTPS installer script as the user.
// Installers are fetched over TLS from the upstream vendor; a `version`
// option pins where upstream supports it (SPEC.md §8.3).
func installerApply(render func(version string) string) func(map[string]any) (string, error) {
	return func(with map[string]any) (string, error) {
		v, err := versionOpt(with)
		if err != nil {
			return "", err
		}
		return render(v), nil
	}
}

// Builtins is the registry of implemented built-in features.
var Builtins = map[string]*Builtin{
	"tmux": {
		Name: "tmux", Version: "1", RequiresRoot: true,
		CheckBash: aptCheck("tmux", "tmux"),
		ApplyBash: aptApply("tmux"),
	},
	"build-essential": {
		Name: "build-essential", Version: "1", RequiresRoot: true,
		CheckBash: `if dpkg-query -W -f='${db:Status-Status}' build-essential 2>/dev/null | grep -q installed; then f feat build-essential present; else f feat build-essential absent; fi
`,
		ApplyBash: aptApply("build-essential pkg-config"),
	},
	"docker": {
		Name: "docker", Version: "1", RequiresRoot: true,
		CheckBash: `if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 || sudo -n docker compose version >/dev/null 2>&1; then f feat docker "$(eb "$(docker --version 2>/dev/null | head -1)")"; else f feat docker absent; fi
`,
		ApplyBash: func(map[string]any) (string, error) {
			return aptGet + ` update -qq
` + aptGet + ` install -y ca-certificates curl
sudo -n install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo -n tee /etc/apt/keyrings/docker.asc >/dev/null
sudo -n chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo -n tee /etc/apt/sources.list.d/docker.list >/dev/null
` + aptGet + ` update -qq
` + aptGet + ` install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
if [ ! -e /etc/docker/daemon.json ]; then
  printf %s '{"log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"}}' | sudo -n tee /etc/docker/daemon.json >/dev/null
  sudo -n systemctl restart docker
fi
sudo -n usermod -aG docker "$USER"
echo "note: docker group membership is effectively root; it takes effect on your next SSH session"
`, nil
		},
	},
	"github-cli": {
		Name: "github-cli", Version: "1", RequiresRoot: true,
		CheckBash: aptCheck("github-cli", "gh"),
		ApplyBash: func(map[string]any) (string, error) {
			return aptGet + ` update -qq
` + aptGet + ` install -y ca-certificates curl
sudo -n mkdir -p -m 755 /etc/apt/keyrings
curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo -n tee /etc/apt/keyrings/githubcli-archive-keyring.gpg >/dev/null
sudo -n chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo -n tee /etc/apt/sources.list.d/github-cli.list >/dev/null
` + aptGet + ` update -qq
` + aptGet + ` install -y gh
`, nil
		},
	},
	"mise": {
		Name: "mise", Version: "1", Options: []string{"version"},
		CheckBash: userBinCheck("mise", "mise", ".local/bin/mise"),
		ApplyBash: installerApply(func(v string) string {
			env := ""
			if v != "" {
				env = "MISE_VERSION=" + q(v) + " "
			}
			return "curl -fsSL https://mise.run | " + env + "sh\n"
		}),
	},
	"uv": {
		Name: "uv", Version: "1", Options: []string{"version"},
		CheckBash: userBinCheck("uv", "uv", ".local/bin/uv"),
		ApplyBash: installerApply(func(v string) string {
			url := "https://astral.sh/uv/install.sh"
			if v != "" {
				url = "https://astral.sh/uv/" + v + "/install.sh"
			}
			return "curl -fsSL " + q(url) + " | sh\n"
		}),
	},
	"bun": {
		// Root because the installer requires unzip, which Ubuntu cloud
		// images do not ship.
		Name: "bun", Version: "2", RequiresRoot: true, Options: []string{"version"},
		CheckBash: userBinCheck("bun", "bun", ".bun/bin/bun"),
		ApplyBash: installerApply(func(v string) string {
			pre := "if ! command -v unzip >/dev/null 2>&1; then\n" +
				aptGet + " update -qq\n" + aptGet + " install -y unzip\nfi\n"
			if v != "" {
				return pre + "curl -fsSL https://bun.sh/install | bash -s " + q("bun-v"+v) + "\n"
			}
			return pre + "curl -fsSL https://bun.sh/install | bash\n"
		}),
	},
	"claude-code": {
		Name: "claude-code", Version: "1", Options: []string{"version"},
		CheckBash: userBinCheck("claude-code", "claude", ".local/bin/claude"),
		ApplyBash: installerApply(func(v string) string {
			if v != "" {
				return "curl -fsSL https://claude.ai/install.sh | bash -s " + q(v) + "\n"
			}
			return "curl -fsSL https://claude.ai/install.sh | bash\n"
		}),
	},
	"codex": {
		Name: "codex", Version: "1", Options: []string{"version"},
		CheckBash: userBinCheck("codex", "codex", ".local/bin/codex"),
		ApplyBash: func(with map[string]any) (string, error) {
			v, err := versionOpt(with)
			if err != nil {
				return "", err
			}
			release := "latest/download"
			if v != "" {
				release = "download/" + v
			}
			return `arch=$(uname -m)
case "$arch" in
  x86_64) target=x86_64-unknown-linux-musl ;;
  aarch64) target=aarch64-unknown-linux-musl ;;
  *) echo "unsupported architecture $arch"; exit 1 ;;
esac
mkdir -p "$HOME/.local/bin"
tmp=$(mktemp -d)
curl -fsSL "https://github.com/openai/codex/releases/` + release + `/codex-$target.tar.gz" | tar -xzf - -C "$tmp"
found=$(find "$tmp" -maxdepth 2 -type f -name 'codex*' | head -1)
[ -n "$found" ] || { echo "codex binary not found in release archive"; exit 1; }
install -m 0755 "$found" "$HOME/.local/bin/codex"
rm -rf "$tmp"
`, nil
		},
	},
}
