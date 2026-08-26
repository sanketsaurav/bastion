#!/usr/bin/env bash
set -u
export LC_ALL=C
b64() { base64 -w0 2>/dev/null; }
eb() { printf %s "$1" | b64; }
f() { printf '@f %s\n' "$*"; }
. /etc/os-release 2>/dev/null || true
f os "${ID:-unknown}" "${VERSION_ID:-0}" "$(uname -m)"
if sudo -n true 2>/dev/null; then f sudo ok; else f sudo missing; fi
dq() { docker "$@" 2>/dev/null || sudo -n docker "$@" 2>/dev/null; }
if [ "$(dpkg-query -W -f='${db:Status-Status}' git 2>/dev/null)" = installed ]; then f pkg git installed; else f pkg git absent; fi
if [ "$(dpkg-query -W -f='${db:Status-Status}' jq 2>/dev/null)" = installed ]; then f pkg jq installed; else f pkg jq absent; fi
if grep -Fq '# bastion:shell-integration' "$HOME/.bashrc" 2>/dev/null; then f shline present; else f shline absent; fi
t="$HOME"/.tmux.conf
if [ -e "$t" ]; then
  if [ -r "$t" ]; then sha=$(sha256sum <"$t" | cut -d' ' -f1); else sha=unreadable; fi
  f file fi8udG11eC5jb25m present "$sha" "$(stat -c %a "$t" 2>/dev/null || echo '?')"
else f file fi8udG11eC5jb25m absent; fi
if [ -e "$t.bastion-backup" ]; then f bak fi8udG11eC5jb25m present; else f bak fi8udG11eC5jb25m absent; fi
m=$(cat /var/lib/bastion/state/testbox/files/5e5cd79a0e5bebe6.json 2>/dev/null || true); [ -n "$m" ] && f marker file 5e5cd79a0e5bebe6 "$(eb "$m")"
t="$HOME"/.config/bastion/shell.sh
if [ -e "$t" ]; then
  if [ -r "$t" ]; then sha=$(sha256sum <"$t" | cut -d' ' -f1); else sha=unreadable; fi
  f file fi8uY29uZmlnL2Jhc3Rpb24vc2hlbGwuc2g= present "$sha" "$(stat -c %a "$t" 2>/dev/null || echo '?')"
else f file fi8uY29uZmlnL2Jhc3Rpb24vc2hlbGwuc2g= absent; fi
if [ -e "$t.bastion-backup" ]; then f bak fi8uY29uZmlnL2Jhc3Rpb24vc2hlbGwuc2g= present; else f bak fi8uY29uZmlnL2Jhc3Rpb24vc2hlbGwuc2g= absent; fi
m=$(cat /var/lib/bastion/state/testbox/files/2b1aae370a5deedf.json 2>/dev/null || true); [ -n "$m" ] && f marker file 2b1aae370a5deedf "$(eb "$m")"
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 || sudo -n docker compose version >/dev/null 2>&1; then f feat docker "$(eb "$(docker --version 2>/dev/null | head -1)")"; else f feat docker absent; fi
m=$(cat /var/lib/bastion/state/testbox/features/docker.json 2>/dev/null || true); [ -n "$m" ] && f marker feature docker "$(eb "$m")"
if command -v tmux >/dev/null 2>&1; then f feat tmux "$(eb "$(tmux -V 2>/dev/null || tmux --version 2>/dev/null | head -1 || echo present)")"; else f feat tmux absent; fi
m=$(cat /var/lib/bastion/state/testbox/features/tmux.json 2>/dev/null || true); [ -n "$m" ] && f marker feature tmux "$(eb "$m")"
if [ -x "$HOME"/.cache/bastion/features/testbox/mytool/check ]; then
  if ( cd "$HOME"/.cache/bastion/features/testbox/mytool && ./check >/dev/null 2>&1 ); then f lcheck mytool ok; else f lcheck mytool needs; fi
else f lcheck mytool needs; fi
m=$(cat /var/lib/bastion/state/testbox/lfeatures/mytool.json 2>/dev/null || true); [ -n "$m" ] && f marker lfeature mytool "$(eb "$m")"
if command -v docker >/dev/null 2>&1; then
  f docker present "$(eb "$(dq --version | head -1)")" "$(eb "$(dq compose version --short || true)")"
else f docker absent; fi
if dq network inspect bastion-testbox >/dev/null; then f network present; else f network absent; fi
if dq container inspect bastion-testbox-_ingress >/dev/null; then
  st=$(dq inspect -f '{{.State.Status}}' bastion-testbox-_ingress); dg=$(dq inspect -f '{{index .Config.Labels "bastion.config-digest"}}' bastion-testbox-_ingress)
  pb=$(dq inspect -f '{{if index .NetworkSettings.Ports "443/tcp"}}bound{{else}}unbound{{end}}' bastion-testbox-_ingress)
  f ingx present "$st" "${dg:-none}" "${pb:-unbound}"
else f ingx absent; fi
if dq container inspect bastion-testbox-db >/dev/null; then
  st=$(dq inspect -f '{{.State.Status}}' bastion-testbox-db); hl=$(dq inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' bastion-testbox-db)
  dg=$(dq inspect -f '{{index .Config.Labels "bastion.config-digest"}}' bastion-testbox-db); im=$(dq inspect -f '{{.Config.Image}}' bastion-testbox-db)
  f svc db present "$st" "$hl" "${dg:-none}" "$(eb "$im")"
else f svc db absent; fi
m=$(cat /var/lib/bastion/state/testbox/services/db.json 2>/dev/null || true); [ -n "$m" ] && f marker service db "$(eb "$m")"
if dq container inspect bastion-testbox-web >/dev/null; then
  st=$(dq inspect -f '{{.State.Status}}' bastion-testbox-web); hl=$(dq inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' bastion-testbox-web)
  dg=$(dq inspect -f '{{index .Config.Labels "bastion.config-digest"}}' bastion-testbox-web); im=$(dq inspect -f '{{.Config.Image}}' bastion-testbox-web)
  f svc web present "$st" "$hl" "${dg:-none}" "$(eb "$im")"
else f svc web absent; fi
m=$(cat /var/lib/bastion/state/testbox/services/web.json 2>/dev/null || true); [ -n "$m" ] && f marker service web "$(eb "$m")"
if sudo -n test -e /var/lib/bastion/secrets/testbox/web.env 2>/dev/null; then f sec web present; else f sec web absent; fi
dq ps -a --filter label=bastion.box-id=testbox --format '{{.Label "bastion.service"}}' | while IFS= read -r s; do
  case "$s" in (*[!a-z0-9-]*|"") ;; (*) f osvc "$s";; esac
done
if [ -d /mnt/bastion/volumes/data ]; then f dvol data present; else f dvol data absent; fi
if dq volume inspect bastion-testbox-scratch >/dev/null; then f evol scratch present; else f evol scratch absent; fi
if [ -d /mnt/bastion/volumes ]; then for d in /mnt/bastion/volumes/*/; do [ -d "$d" ] && f odvol "$(basename "$d")"; done; fi
for mf in /var/lib/bastion/state/testbox/features/*.json /var/lib/bastion/state/testbox/lfeatures/*.json; do
  [ -e "$mf" ] || continue
  n=$(basename "$mf" .json)
  case "$n" in (*[!a-z0-9-]*|"") continue;; esac
  case "$mf" in (*/lfeatures/*) f lmark "$n";; (*) f fmark "$n";; esac
done
for pf in /var/lib/bastion/state/testbox/prereqs/*/*.json; do
  [ -e "$pf" ] || continue
  pd=$(basename "$(dirname "$pf")"); pn=$(basename "$pf" .json)
  case "$pd" in (*[!a-z0-9-]*|"") continue;; esac
  case "$pn" in (*[!a-z0-9.+-]*|"") continue;; esac
  f pmark "$pd" "$pn"
done
f end ok
exit 0
