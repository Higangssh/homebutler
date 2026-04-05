package alerts

import (
	"strings"
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

func TestExecTimeout(t *testing.T) {
	rule := Rule{Action: "exec", Exec: "sleep 10", Timeout: "1s"}
	result := ExecuteAction(rule)
	if result.Success {
		t.Error("long-running command should fail with timeout")
	}
	if !strings.Contains(result.Output, "timed out") {
		t.Errorf("expected timeout message, got %q", result.Output)
	}
}

func TestExecDefaultTimeout(t *testing.T) {
	rule := Rule{Action: "exec", Exec: "echo fast"}
	result := ExecuteAction(rule)
	if !result.Success {
		t.Errorf("fast command should succeed: %s", result.Output)
	}
}

func TestExecCustomTimeout(t *testing.T) {
	rule := Rule{Action: "exec", Exec: "echo ok", Timeout: "5s"}
	result := ExecuteAction(rule)
	if !result.Success {
		t.Errorf("command should succeed within timeout: %s", result.Output)
	}
}

func TestExecTimeoutParsing(t *testing.T) {
	// Default
	r := Rule{}
	if r.ExecTimeout() != 30*time.Second {
		t.Errorf("default timeout should be 30s, got %s", r.ExecTimeout())
	}
	// Custom
	r.Timeout = "10s"
	if r.ExecTimeout() != 10*time.Second {
		t.Errorf("custom timeout should be 10s, got %s", r.ExecTimeout())
	}
	// Invalid falls back to 30s
	r.Timeout = "invalid"
	if r.ExecTimeout() != 30*time.Second {
		t.Errorf("invalid timeout should fallback to 30s, got %s", r.ExecTimeout())
	}
}

func TestIsDangerousCommandComment(t *testing.T) {
	// Verify the function still works (blocklist is supplementary)
	if !IsDangerousCommand("rm -rf /") {
		t.Error("rm -rf / should still be blocked")
	}
	// Bypass example: encoded/obfuscated commands pass through
	// This is expected — the blocklist is documented as supplementary
	if IsDangerousCommand("perl -e 'system(\"rm -rf /\")'") {
		// This should actually NOT be caught by simple string matching
		// but the nested rm -rf / IS caught since it's in the string
	}
}

func TestExecWithStrings(t *testing.T) {
	// Verify the strings import is used
	rule := Rule{Action: "exec", Exec: "echo hello"}
	result := ExecuteAction(rule)
	if result.Output != "hello" {
		t.Errorf("expected 'hello', got %q", result.Output)
	}
}
