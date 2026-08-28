package engine

// Hardening convergence (SPEC.md §8.5): declared guest security posture,
// applied through the ordinary managed-file pipeline.

// AutoRebootTarget carries the unattended-upgrades reboot policy. A root
// path, so the file action runs privileged like any absolute target.
const AutoRebootTarget = "/etc/apt/apt.conf.d/51bastion-unattended-reboot"

// autoRebootContent lets unattended-upgrades reboot in the given window
// when a security update requires it — the fix for updates that sit
// downloaded but inactive. WithUsers stays true: a personal box's idle SSH
// session must not block a kernel fix.
func autoRebootContent(window string) []byte {
	return []byte(`// Managed by bastion — edit the box definition, not this file.
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "` + window + `";
Unattended-Upgrade::Automatic-Reboot-WithUsers "true";
`)
}
