#!/usr/bin/env bash
set -u
export LC_ALL=C
b64() { base64 -w0 2>/dev/null; }
eb() { printf %s "$1" | b64; }
f() { printf '@f %s\n' "$*"; }
e() { printf '@e %s %s\n' "$1" "$2"; }
run() {
  local id="$1"; shift
  e "$id" start
  ( set -eo pipefail
    "$@" 2>&1 | while IFS= read -r ln; do printf '@l %s %s\n' "$id" "$(printf %s "$ln" | b64)"; done
    exit "${PIPESTATUS[0]}" )
  local rc=$?
  if [ "$rc" -eq 0 ]; then e "$id" ok; else e "$id" fail; exit 20; fi
}
dk() { sudo -n docker "$@"; }
LOCKD="$HOME/.cache/bastion/locks"; mkdir -p "$LOCKD"; LOCK="$LOCKD/testbox"
if ! mkdir "$LOCK" 2>/dev/null; then
  age=$(( $(date +%s) - $(stat -c %Y "$LOCK" 2>/dev/null || echo 0) ))
  if [ "$age" -gt 3600 ]; then
    rmdir "$LOCK" 2>/dev/null || true
    mkdir "$LOCK" 2>/dev/null || { e lock fail; exit 21; }
    e lock takeover
  else
    printf '@e lock fail %s\n' "$age"; exit 21
  fi
fi
trap 'rmdir "$LOCK" 2>/dev/null' EXIT
trap 'rmdir "$LOCK" 2>/dev/null; trap - EXIT; exit 129' HUP INT TERM PIPE
s0() {
  sudo -n mkdir -p /var/lib/bastion/state/testbox/files /var/lib/bastion/state/testbox/features /var/lib/bastion/state/testbox/lfeatures /var/lib/bastion/state/testbox/services /var/lib/bastion/services/testbox /var/lib/bastion/secrets/testbox
  sudo -n chmod 700 /var/lib/bastion/secrets/testbox
  mkdir -p "$HOME/.cache/bastion"
}
run a0 s0
s1() {
  sudo -n DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 update -qq
  sudo -n DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y git jq
}
run a1 s1
s2() {
  sudo -n DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 update -qq
  sudo -n DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y ca-certificates curl
  sudo -n install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo -n tee /etc/apt/keyrings/docker.asc >/dev/null
  sudo -n chmod a+r /etc/apt/keyrings/docker.asc
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo -n tee /etc/apt/sources.list.d/docker.list >/dev/null
  sudo -n DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 update -qq
  sudo -n DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  if [ ! -e /etc/docker/daemon.json ]; then
    printf %s '{"log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"}}' | sudo -n tee /etc/docker/daemon.json >/dev/null
    sudo -n systemctl restart docker
  fi
  sudo -n usermod -aG docker "$USER"
  echo "note: docker group membership is effectively root; it takes effect on your next SSH session"
  printf %s eyJuYW1lIjoiZG9ja2VyIiwidmVyc2lvbiI6IjEiLCJvcHRpb25zRGlnZXN0IjoiZTNiMGM0NDI5OGZjMWMxNDlhZmJmNGM4OTk2ZmI5MjQyN2FlNDFlNDY0OWI5MzRjYTQ5NTk5MWI3ODUyYjg1NSJ9 | base64 -d | sudo -n tee /var/lib/bastion/state/testbox/features/.docker.tmp >/dev/null && sudo -n mv /var/lib/bastion/state/testbox/features/.docker.tmp /var/lib/bastion/state/testbox/features/docker.json
}
run a2 s2
s3() {
  sudo -n DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 update -qq
  sudo -n DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 install -y tmux
  printf %s eyJuYW1lIjoidG11eCIsInZlcnNpb24iOiIxIiwib3B0aW9uc0RpZ2VzdCI6ImUzYjBjNDQyOThmYzFjMTQ5YWZiZjRjODk5NmZiOTI0MjdhZTQxZTQ2NDliOTM0Y2E0OTU5OTFiNzg1MmI4NTUifQ== | base64 -d | sudo -n tee /var/lib/bastion/state/testbox/features/.tmux.tmp >/dev/null && sudo -n mv /var/lib/bastion/state/testbox/features/.tmux.tmp /var/lib/bastion/state/testbox/features/tmux.json
}
run a3 s3
s4() {
  d="$HOME"/.cache/bastion/features/testbox/mytool
  rm -rf "$d" && mkdir -p "$d"
  printf %s H4sIAAAAAAAC/+zUy66CMBDG8a55ih7OXr5WKglvU8kYiNxCi7Fvb3SjIXFniZf5babpbhb/sePYBhEXABTG3CaA5QS0ur+v/wraKCEhVjA7bycBvGLJx+U+xP9ftm/6zNUJnRsvkQj2S6qaquMb9g/un/tn0R3I+nmiTbBdG7P/XZ4/73+rF/0rowrufw297aiUXfDD0CYnmlwz9KVMdcqHgDHGvtolAAD//15p314AEAAA | base64 -d | tar -xzf - -C "$d"
  printf %s eyJjaGFubmVsIjoic3RhYmxlIn0= | base64 -d > "$d/inputs.json"
  chmod +x "$d/check" "$d/apply" 2>/dev/null || true
  ( cd "$d" && ./apply inputs.json )
  printf %s eyJuYW1lIjoibXl0b29sIiwidmVyc2lvbiI6IjIiLCJzb3VyY2VEaWdlc3QiOiIxNzY2MmU4NmI1ZmIyYjFlMTIyMGJkOTlhZTQ2YmNlOGU1MTQ4OWUxYjg2OTNlNGVmNzc3YzRlODg3Mjc5NTc4IiwiaW5wdXRzRGlnZXN0IjoiYzBiY2VkYmY0Y2EzNjk2OGNhYTVjYzlmNTg1MTAwNGJiNjBkMWIwZTA1ZjUzNjM0YzExODNmOGQ1ZmQ5M2ZjMSJ9 | base64 -d | sudo -n tee /var/lib/bastion/state/testbox/lfeatures/.mytool.tmp >/dev/null && sudo -n mv /var/lib/bastion/state/testbox/lfeatures/.mytool.tmp /var/lib/bastion/state/testbox/lfeatures/mytool.json
}
run a4 s4
s5() {
  t="$HOME"/.tmux.conf
  tmp=$(mktemp) || return 1
  printf %s c2V0IC1nIG1vdXNlIG9uCg== | base64 -d > "$tmp" || return 1
  chmod 0600 "$tmp" || return 1
  mkdir -p "$(dirname "$t")" || return 1
  mv -f "$tmp" "$t" || return 1
  printf %s eyJ0YXJnZXQiOiJ+Ly50bXV4LmNvbmYiLCJzaGEyNTYiOiJkY2ZlZDkxMjczN2VlNjM0MDZmOTg1NjVhZDZiNjM1ZjM4NWQ0YzJmOWEwYWExMTBiNzZiOWJiZjExZDA2ZTI3IiwibW9kZSI6IjA2MDAifQ== | base64 -d | sudo -n tee /var/lib/bastion/state/testbox/files/.5e5cd79a0e5bebe6.tmp >/dev/null && sudo -n mv /var/lib/bastion/state/testbox/files/.5e5cd79a0e5bebe6.tmp /var/lib/bastion/state/testbox/files/5e5cd79a0e5bebe6.json
}
run a5 s5
s6() {
  t="$HOME"/.config/bastion/shell.sh
  tmp=$(mktemp) || return 1
  printf %s IyBNYW5hZ2VkIGJ5IGJhc3Rpb24g4oCUIGVkaXQgdGhlIGJveCBkZWZpbml0aW9uLCBub3QgdGhpcyBmaWxlLgpQUzE9J1xbXGVbMDE7MzJtXF1kZXZAXGhcW1xlWzAwbVxdOlxbXGVbMDE7MzRtXF1cd1xbXGVbMDBtXF1cJCAnCg== | base64 -d > "$tmp" || return 1
  chmod 0644 "$tmp" || return 1
  mkdir -p "$(dirname "$t")" || return 1
  mv -f "$tmp" "$t" || return 1
  printf %s eyJ0YXJnZXQiOiJ+Ly5jb25maWcvYmFzdGlvbi9zaGVsbC5zaCIsInNoYTI1NiI6IjgwYzBiYmQxYzQzMmRiMGViODRiNGUwYjcyZmE1NDIyYmMxMzQyNDg1YzFjZjViMmViNTA2ZjE4YzEwMGE4NDUiLCJtb2RlIjoiMDY0NCJ9 | base64 -d | sudo -n tee /var/lib/bastion/state/testbox/files/.2b1aae370a5deedf.tmp >/dev/null && sudo -n mv /var/lib/bastion/state/testbox/files/.2b1aae370a5deedf.tmp /var/lib/bastion/state/testbox/files/2b1aae370a5deedf.json
}
run a6 s6
s7() {
  if ! grep -Fq '# bastion:shell-integration' "$HOME/.bashrc" 2>/dev/null; then
    printf '\n%s\n' '[ -f "$HOME/.config/bastion/shell.sh" ] && . "$HOME/.config/bastion/shell.sh"  # bastion:shell-integration' >> "$HOME/.bashrc"
  fi
  echo "shell integration line ensured in ~/.bashrc"
}
run a7 s7
s8() {
  printf '%s ALL=(ALL:ALL) NOPASSWD:ALL\n' dev | sudo -n tee /etc/sudoers.d/bastion-user-alias >/dev/null
  sudo -n chmod 0440 /etc/sudoers.d/bastion-user-alias
  if getent passwd dev >/dev/null; then
    echo "login alias already present"
  else
    sudo -n useradd -o -u "$(id -u)" -g "$(id -g)" -d "$HOME" -M -s /bin/bash dev
    echo "login alias created; whoami now reports dev"
  fi
}
run a8 s8
s9() {
  t="$HOME"/.hushlogin
  tmp=$(mktemp) || return 1
  printf %s '' | base64 -d > "$tmp" || return 1
  chmod 0644 "$tmp" || return 1
  mkdir -p "$(dirname "$t")" || return 1
  mv -f "$tmp" "$t" || return 1
  printf %s eyJ0YXJnZXQiOiJ+Ly5odXNobG9naW4iLCJzaGEyNTYiOiJlM2IwYzQ0Mjk4ZmMxYzE0OWFmYmY0Yzg5OTZmYjkyNDI3YWU0MWU0NjQ5YjkzNGNhNDk1OTkxYjc4NTJiODU1IiwibW9kZSI6IjA2NDQifQ== | base64 -d | sudo -n tee /var/lib/bastion/state/testbox/files/.af87863e7924ae82.tmp >/dev/null && sudo -n mv /var/lib/bastion/state/testbox/files/.af87863e7924ae82.tmp /var/lib/bastion/state/testbox/files/af87863e7924ae82.json
}
run a9 s9
s10() {
  t=/etc/apt/apt.conf.d/51bastion-unattended-reboot
  tmp=$(mktemp) || return 1
  printf %s Ly8gTWFuYWdlZCBieSBiYXN0aW9uIOKAlCBlZGl0IHRoZSBib3ggZGVmaW5pdGlvbiwgbm90IHRoaXMgZmlsZS4KVW5hdHRlbmRlZC1VcGdyYWRlOjpBdXRvbWF0aWMtUmVib290ICJ0cnVlIjsKVW5hdHRlbmRlZC1VcGdyYWRlOjpBdXRvbWF0aWMtUmVib290LVRpbWUgIjA0OjMwIjsKVW5hdHRlbmRlZC1VcGdyYWRlOjpBdXRvbWF0aWMtUmVib290LVdpdGhVc2VycyAidHJ1ZSI7Cg== | base64 -d > "$tmp" || return 1
  chmod 0644 "$tmp" || return 1
  sudo -n mkdir -p "$(dirname "$t")" || return 1
  sudo -n mv -f "$tmp" "$t" || return 1
  sudo -n chown root:root "$t" || return 1
  printf %s eyJ0YXJnZXQiOiIvZXRjL2FwdC9hcHQuY29uZi5kLzUxYmFzdGlvbi11bmF0dGVuZGVkLXJlYm9vdCIsInNoYTI1NiI6Ijc5YWFkNzNhZWU5OWVhODhkNzkwNjc0MmM2NzU3ZjkzYWVjOGQwMzA4NzNlNWYwMDZkOGJjM2QwNDgyN2Q4OWUiLCJtb2RlIjoiMDY0NCJ9 | base64 -d | sudo -n tee /var/lib/bastion/state/testbox/files/.c9dc08f25a1d0538.tmp >/dev/null && sudo -n mv /var/lib/bastion/state/testbox/files/.c9dc08f25a1d0538.tmp /var/lib/bastion/state/testbox/files/c9dc08f25a1d0538.json
}
run a10 s10
s11() {
  dk network inspect bastion-testbox >/dev/null 2>&1 || dk network create --label bastion.box-id=testbox bastion-testbox
}
run a11 s11
s12() {
  sudo -n mkdir -p /mnt/bastion/volumes/data
}
run a12 s12
s13() {
  dk volume inspect bastion-testbox-scratch >/dev/null 2>&1 || dk volume create --label bastion.box-id=testbox bastion-testbox-scratch
}
run a13 s13
s14() {
  sudo -n mkdir -p /var/lib/bastion/services/testbox/db
  printf %s IyBHZW5lcmF0ZWQgYnkgYmFzdGlvbiB0ZXN0IGZvciBib3ggInRlc3Rib3giLiBEbyBub3QgZWRpdDsgYXBwbHkgb3ZlcndyaXRlcyB0aGlzIGZpbGUuCm5hbWU6ICJiYXN0aW9uLXRlc3Rib3gtZGIiCnNlcnZpY2VzOgogIGRiOgogICAgaW1hZ2U6ICJkb2NrZXIuaW8vbGlicmFyeS9wb3N0Z3JlczoxNi40IgogICAgY29udGFpbmVyX25hbWU6ICJiYXN0aW9uLXRlc3Rib3gtZGIiCiAgICByZXN0YXJ0OiAidW5sZXNzLXN0b3BwZWQiCiAgICB2b2x1bWVzOgogICAgICAtICIvbW50L2Jhc3Rpb24vdm9sdW1lcy9kYXRhOi92YXIvbGliL3Bvc3RncmVzcWwvZGF0YSIKICAgIGhlYWx0aGNoZWNrOgogICAgICB0ZXN0OiBbIkNNRCIsInBnX2lzcmVhZHkiXQogICAgICBpbnRlcnZhbDogMTBzCiAgICBzZWN1cml0eV9vcHQ6CiAgICAgIC0gbm8tbmV3LXByaXZpbGVnZXM6dHJ1ZQogICAgbGFiZWxzOgogICAgICBiYXN0aW9uLmJveC1pZDogInRlc3Rib3giCiAgICAgIGJhc3Rpb24uc2VydmljZTogImRiIgogICAgICBiYXN0aW9uLmltYWdlOiAiZG9ja2VyLmlvL2xpYnJhcnkvcG9zdGdyZXM6MTYuNCIKICAgICAgYmFzdGlvbi52ZXJzaW9uOiAidGVzdCIKICAgICAgYmFzdGlvbi5jb25maWctZGlnZXN0OiAiOGE5ODUwOTExZDE5YjgwNDcxNTRhM2VmNjFmNzUyMzllM2M0MDRmZTRkZmRlMmUwZGZhMGMwZWEyYmY0MjhhOCIKbmV0d29ya3M6CiAgZGVmYXVsdDoKICAgIG5hbWU6ICJiYXN0aW9uLXRlc3Rib3giCiAgICBleHRlcm5hbDogdHJ1ZQo= | base64 -d | sudo -n tee /var/lib/bastion/services/testbox/db/compose.yaml >/dev/null
  dk compose -p bastion-testbox-db -f /var/lib/bastion/services/testbox/db/compose.yaml up -d --pull missing --remove-orphans
  printf %s eyJuYW1lIjoiZGIiLCJjb25maWdEaWdlc3QiOiI4YTk4NTA5MTFkMTliODA0NzE1NGEzZWY2MWY3NTIzOWUzYzQwNGZlNGRmZGUyZTBkZmEwYzBlYTJiZjQyOGE4IiwiaW1hZ2UiOiJkb2NrZXIuaW8vbGlicmFyeS9wb3N0Z3JlczoxNi40In0= | base64 -d | sudo -n tee /var/lib/bastion/state/testbox/services/.db.tmp >/dev/null && sudo -n mv /var/lib/bastion/state/testbox/services/.db.tmp /var/lib/bastion/state/testbox/services/db.json
}
run a14 s14
s15() {
  end=$(( $(date +%s) + 120 ))
  while :; do
    h=$(dk inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}healthy{{end}}' bastion-testbox-db 2>/dev/null || echo error)
    case "$h" in
      healthy) exit 0 ;;
      unhealthy) echo "service db is unhealthy"; exit 1 ;;
    esac
    if [ "$(date +%s)" -gt "$end" ]; then echo "timed out waiting for db to become healthy (last: $h)"; exit 1; fi
    sleep 3
  done
}
run a15 s15
s16() {
  printf %s QVBJX1RPS0VOPXNla3JpdC12YWx1ZQo= | base64 -d | sudo -n tee /var/lib/bastion/secrets/testbox/web.env >/dev/null
  sudo -n chmod 0600 /var/lib/bastion/secrets/testbox/web.env
}
run a16 s16
s17() {
  sudo -n mkdir -p /var/lib/bastion/services/testbox/web
  printf %s IyBHZW5lcmF0ZWQgYnkgYmFzdGlvbiB0ZXN0IGZvciBib3ggInRlc3Rib3giLiBEbyBub3QgZWRpdDsgYXBwbHkgb3ZlcndyaXRlcyB0aGlzIGZpbGUuCm5hbWU6ICJiYXN0aW9uLXRlc3Rib3gtd2ViIgpzZXJ2aWNlczoKICB3ZWI6CiAgICBpbWFnZTogImdoY3IuaW8vZXhhbXBsZS93ZWJAc2hhMjU2OjAwMTEyMjMzNDQ1NTY2Nzc4ODk5MDAxMTIyMzM0NDU1NjY3Nzg4OTkwMDExMjIzMzQ0NTU2Njc3ODg5OTAwMTEiCiAgICBjb250YWluZXJfbmFtZTogImJhc3Rpb24tdGVzdGJveC13ZWIiCiAgICByZXN0YXJ0OiAidW5sZXNzLXN0b3BwZWQiCiAgICBlbnZpcm9ubWVudDoKICAgICAgTE9HX0xFVkVMOiAiaW5mbyIKICAgIGVudl9maWxlOgogICAgICAtICIvdmFyL2xpYi9iYXN0aW9uL3NlY3JldHMvdGVzdGJveC93ZWIuZW52IgogICAgcG9ydHM6CiAgICAgIC0gIjEyNy4wLjAuMTozMDAwOjMwMDAiCiAgICB2b2x1bWVzOgogICAgICAtICJiYXN0aW9uLXRlc3Rib3gtc2NyYXRjaDovdG1wL2NhY2hlIgogICAgc2VjdXJpdHlfb3B0OgogICAgICAtIG5vLW5ldy1wcml2aWxlZ2VzOnRydWUKICAgIGxhYmVsczoKICAgICAgYmFzdGlvbi5ib3gtaWQ6ICJ0ZXN0Ym94IgogICAgICBiYXN0aW9uLnNlcnZpY2U6ICJ3ZWIiCiAgICAgIGJhc3Rpb24uaW1hZ2U6ICJnaGNyLmlvL2V4YW1wbGUvd2ViQHNoYTI1NjowMDExMjIzMzQ0NTU2Njc3ODg5OTAwMTEyMjMzNDQ1NTY2Nzc4ODk5MDAxMTIyMzM0NDU1NjY3Nzg4OTkwMDExIgogICAgICBiYXN0aW9uLnZlcnNpb246ICJ0ZXN0IgogICAgICBiYXN0aW9uLmNvbmZpZy1kaWdlc3Q6ICJjMDg5MTA3OWVkZTA1ODEyZDlmOTE3ZDc3YjJhYWYxOTgxOTFiNTk5MmNlYjc1ZGY3ODA1ZGU2ZTQyNzM1ZjM1IgpuZXR3b3JrczoKICBkZWZhdWx0OgogICAgbmFtZTogImJhc3Rpb24tdGVzdGJveCIKICAgIGV4dGVybmFsOiB0cnVlCnZvbHVtZXM6CiAgYmFzdGlvbi10ZXN0Ym94LXNjcmF0Y2g6CiAgICBleHRlcm5hbDogdHJ1ZQo= | base64 -d | sudo -n tee /var/lib/bastion/services/testbox/web/compose.yaml >/dev/null
  dk compose -p bastion-testbox-web -f /var/lib/bastion/services/testbox/web/compose.yaml up -d --pull missing --remove-orphans
  printf %s eyJuYW1lIjoid2ViIiwiY29uZmlnRGlnZXN0IjoiYzA4OTEwNzllZGUwNTgxMmQ5ZjkxN2Q3N2IyYWFmMTk4MTkxYjU5OTJjZWI3NWRmNzgwNWRlNmU0MjczNWYzNSIsImltYWdlIjoiZ2hjci5pby9leGFtcGxlL3dlYkBzaGEyNTY6MDAxMTIyMzM0NDU1NjY3Nzg4OTkwMDExMjIzMzQ0NTU2Njc3ODg5OTAwMTEyMjMzNDQ1NTY2Nzc4ODk5MDAxMSJ9 | base64 -d | sudo -n tee /var/lib/bastion/state/testbox/services/.web.tmp >/dev/null && sudo -n mv /var/lib/bastion/state/testbox/services/.web.tmp /var/lib/bastion/state/testbox/services/web.json
}
run a17 s17
printf '@e apply done\n'
exit 0
