package alerts

import (
	"testing"
	"time"
)

func TestIsDangerousCommandBlocked(t *testing.T) {
	dangerous := []string{
		"rm -rf /",
		"rm -rf /*",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		"shutdown -h now",
		"reboot",
		"> /dev/sda",
	}
	for _, cmd := range dangerous {
		if !IsDangerousCommand(cmd) {
			t.Errorf("expected %q to be blocked as dangerous", cmd)
		}
	}
}

func TestIsDangerousCommandAllowed(t *testing.T) {
	safe := []string{
		"docker system prune -f",
		"docker compose up -d",
		"echo hello",
		"ls -la /tmp",
		"df -h",
		"systemctl restart nginx",
	}
	for _, cmd := range safe {
		if IsDangerousCommand(cmd) {
			t.Errorf("expected %q to be allowed", cmd)
		}
	}
}

func TestExecuteActionNotify(t *testing.T) {
	rule := Rule{Action: "notify"}
	result := ExecuteAction(rule)
	if !result.Success {
		t.Error("notify action should succeed")
	}
	if result.Action != "notify" {
		t.Errorf("expected action 'notify', got %q", result.Action)
	}
}

func TestExecuteActionExecSafe(t *testing.T) {
	rule := Rule{Action: "exec", Exec: "echo hello"}
	result := ExecuteAction(rule)
	if !result.Success {
		t.Errorf("exec 'echo hello' should succeed: %s", result.Output)
	}
	if result.Output != "hello" {
		t.Errorf("expected output 'hello', got %q", result.Output)
	}
}

func TestExecuteActionExecDangerous(t *testing.T) {
	rule := Rule{Action: "exec", Exec: "rm -rf /"}
	result := ExecuteAction(rule)
	if result.Success {
		t.Error("dangerous command should be blocked")
	}
	if result.Action != "exec" {
		t.Errorf("expected action 'exec', got %q", result.Action)
	}
}

func TestExecuteActionExecEmpty(t *testing.T) {
	rule := Rule{Action: "exec", Exec: ""}
	result := ExecuteAction(rule)
	if result.Success {
		t.Error("empty exec should fail")
	}
}

func TestExecuteActionUnknown(t *testing.T) {
	rule := Rule{Action: "unknown"}
	result := ExecuteAction(rule)
	if result.Success {
		t.Error("unknown action should fail")
	}
}

func TestCooldownTracker(t *testing.T) {
	ct := newCooldownTracker()

	// Not in cooldown initially
	if ct.InCooldown("rule1", 5*time.Minute) {
		t.Error("should not be in cooldown before firing")
	}

	ct.MarkFired("rule1")

	// Should be in cooldown
	if !ct.InCooldown("rule1", 5*time.Minute) {
		t.Error("should be in cooldown after firing")
	}

	// Different rule should not be in cooldown
	if ct.InCooldown("rule2", 5*time.Minute) {
		t.Error("different rule should not be in cooldown")
	}

	// Zero cooldown should never block
	if ct.InCooldown("rule1", 0) {
		t.Error("zero cooldown should never block")
	}
}

func TestCooldownTrackerExpired(t *testing.T) {
	ct := newCooldownTracker()
	ct.mu.Lock()
	ct.fired["rule1"] = time.Now().Add(-10 * time.Minute)
	ct.mu.Unlock()

	if ct.InCooldown("rule1", 5*time.Minute) {
		t.Error("should not be in cooldown after expiry")
	}
}

func TestExecuteRestartNoContainers(t *testing.T) {
	rule := Rule{Action: "restart", Watch: nil}
	result := ExecuteAction(rule)
	if result.Success {
		t.Error("restart with no containers should fail")
	}
}
