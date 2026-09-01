package proxmox

import "testing"

func TestExpectedGuestValidate(t *testing.T) {
	tests := []struct {
		name    string
		guest   ExpectedGuest
		wantErr bool
	}{
		{"valid qemu", ExpectedGuest{Node: "pve1", Type: "qemu", VMID: 100}, false},
		{"valid lxc", ExpectedGuest{Node: "pve1", Type: "lxc", VMID: 101}, false},
		{"missing node", ExpectedGuest{Type: "qemu", VMID: 100}, true},
		{"bad type", ExpectedGuest{Node: "pve1", Type: "docker", VMID: 100}, true},
		{"zero vmid", ExpectedGuest{Node: "pve1", Type: "qemu", VMID: 0}, true},
		{"negative vmid", ExpectedGuest{Node: "pve1", Type: "qemu", VMID: -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.guest.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
