package alerts

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Higangssh/homebutler/internal/docker"
)

// ActionResult holds the outcome of a playbook action.
type PlaybookResult struct {
	Action  string `json:"action"`
	Success bool   `json:"success"`
	Output  string `json:"output"`
}

// cooldownTracker manages per-rule cooldown timers.
type cooldownTracker struct {
	mu    sync.Mutex
	fired map[string]time.Time
}

func newCooldownTracker() *cooldownTracker {
	return &cooldownTracker{
		fired: make(map[string]time.Time),
	}
}

// InCooldown checks if a rule is still in its cooldown period.
func (ct *cooldownTracker) InCooldown(ruleName string, cooldown time.Duration) bool {
	if cooldown == 0 {
		return false
	}
	ct.mu.Lock()
	defer ct.mu.Unlock()
	last, ok := ct.fired[ruleName]
	if !ok {
		return false
	}
	return time.Since(last) < cooldown
}

// MarkFired records the current time as the last fire time for a rule.
func (ct *cooldownTracker) MarkFired(ruleName string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.fired[ruleName] = time.Now()
}

// dangerousPatterns contains shell commands that should never be executed.
var dangerousPatterns = []string{
	"rm -rf /",
	"rm -rf /*",
	"mkfs",
	"dd if=",
	":(){:|:&};:",
	"shutdown",
	"reboot",
	"init 0",
	"init 6",
	"halt",
	"poweroff",
	"> /dev/sda",
}

// IsDangerousCommand checks if a command matches known dangerous patterns.
// NOTE: This is a best-effort blocklist and is NOT a complete security boundary.
// It serves as a supplementary safety net; do not rely on it as the sole defense.
func IsDangerousCommand(cmd string) bool {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// ExecuteAction runs the appropriate playbook action for a triggered rule.
func ExecuteAction(rule Rule) PlaybookResult {
	switch rule.Action {
	case "restart":
		return executeRestart(rule)
	case "exec":
		return executeExec(rule)
	case "notify":
		return PlaybookResult{Action: "notify", Success: true, Output: "notification only"}
	default:
		return PlaybookResult{Action: rule.Action, Success: false, Output: fmt.Sprintf("unknown action: %s", rule.Action)}
	}
}

func executeRestart(rule Rule) PlaybookResult {
	if len(rule.Watch) == 0 {
		return PlaybookResult{Action: "restart", Success: false, Output: "no containers to restart"}
	}

	var failed []string
	var restarted []string
	for _, name := range rule.Watch {
		_, err := docker.Restart(name)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", name, err))
		} else {
			restarted = append(restarted, name)
		}
	}

	if len(failed) > 0 {
		return PlaybookResult{
			Action:  "restart",
			Success: false,
			Output:  fmt.Sprintf("restarted: [%s], failed: [%s]", strings.Join(restarted, ", "), strings.Join(failed, "; ")),
		}
	}

	return PlaybookResult{
		Action:  "restart",
		Success: true,
		Output:  fmt.Sprintf("restarted: [%s]", strings.Join(restarted, ", ")),
	}
}

func executeExec(rule Rule) PlaybookResult {
	if rule.Exec == "" {
		return PlaybookResult{Action: "exec", Success: false, Output: "no command specified"}
	}

	if IsDangerousCommand(rule.Exec) {
		return PlaybookResult{
			Action:  "exec",
			Success: false,
			Output:  fmt.Sprintf("blocked dangerous command: %s", rule.Exec),
		}
	}

	log.Printf("[exec] rule=%q running: %s (timeout=%s)", rule.Name, rule.Exec, rule.ExecTimeout())

	timeout := rule.ExecTimeout()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", rule.Exec)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("[exec] rule=%q timed out after %s", rule.Name, timeout)
		return PlaybookResult{
			Action:  "exec",
			Success: false,
			Output:  fmt.Sprintf("command timed out after %s", timeout),
		}
	}

	if err != nil {
		log.Printf("[exec] rule=%q failed: %v", rule.Name, err)
		return PlaybookResult{
			Action:  "exec",
			Success: false,
			Output:  fmt.Sprintf("command failed: %s (output: %s)", err, output),
		}
	}

	log.Printf("[exec] rule=%q succeeded", rule.Name)
	return PlaybookResult{
		Action:  "exec",
		Success: true,
		Output:  output,
	}
}
