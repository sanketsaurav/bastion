// Package provider defines the provider-neutral view of a box's VM.
package provider

// Instance is the observed state of a box's virtual machine.
type Instance struct {
	Name        string            `json:"name"`
	ID          string            `json:"id,omitempty"`
	Zone        string            `json:"zone"`
	MachineType string            `json:"machineType"`
	Status      string            `json:"status"`
	InternalIP  string            `json:"internalIP,omitempty"`
	ExternalIP  string            `json:"externalIP,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Metadata    map[string]string `json:"-"`
	LastStart   string            `json:"lastStart,omitempty"`
	LastStop    string            `json:"lastStop,omitempty"`

	// Security-relevant posture, consumed by `bastion audit`.
	ServiceAccounts []ServiceAccount `json:"serviceAccounts,omitempty"`
	Network         string           `json:"network,omitempty"`    // first NIC's network name
	SecureBoot      *bool            `json:"secureBoot,omitempty"` // nil = platform didn't report
}

// ServiceAccount is a cloud identity attached to the instance — anything
// running on the box can mint tokens for it via the metadata server.
type ServiceAccount struct {
	Email  string   `json:"email"`
	Scopes []string `json:"scopes,omitempty"`
}

func (i *Instance) Running() bool   { return i.Status == "RUNNING" }
func (i *Instance) Stopped() bool   { return i.Status == "TERMINATED" }
func (i *Instance) Suspended() bool { return i.Status == "SUSPENDED" }

// Transitional reports whether the instance is between stable states.
func (i *Instance) Transitional() bool {
	switch i.Status {
	case "PROVISIONING", "STAGING", "STOPPING", "SUSPENDING", "REPAIRING":
		return true
	}
	return false
}
