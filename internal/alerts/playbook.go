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
	"github.com/Higangssh/homebutler/internal/watch"
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

// FlapChecker reports whether a target is in a restart loop right now.
//
// Restarting something that is already restarting feeds the loop, and most
// systemd units carry Restart=always, so homebutler restarting them is at best
// redundant and at worst fighting systemd's own backoff. Implemented by
// internal/watch from the recorded incident history; a nil checker disables
// the suppression.
type FlapChecker interface {
	IsFlapping(target string) bool
}

// ExecuteAction runs the appropriate playbook action for a triggered rule.
//
// flap may be nil, which disables flapping suppression.
func ExecuteAction(rule Rule, flap FlapChecker) PlaybookResult {
	switch rule.Action {
	case "restart":
		return executeRestart(rule, flap)
	case "exec":
		return executeExec(rule)
	case "notify":
		return PlaybookResult{Action: "notify", Success: true, Output: "notification only"}
	default:
		return PlaybookResult{Action: rule.Action, Success: false, Output: fmt.Sprintf("unknown action: %s", rule.Action)}
	}
}

func executeRestart(rule Rule, flap FlapChecker) PlaybookResult {
	if len(rule.Watch) == 0 {
		return PlaybookResult{Action: "restart", Success: false, Output: "no targets to restart"}
	}

	kind := rule.EffectiveKind()
	if !validKind(kind) {
		return PlaybookResult{
			Action:  "restart",
			Success: false,
			Output:  fmt.Sprintf("unknown kind %q (supported: %s)", kind, strings.Join(watch.Kinds(), ", ")),
		}
	}

	var failed, restarted, skipped []string
	for _, name := range rule.Watch {
		if flap != nil && flap.IsFlapping(name) {
			// Not a failure. The target is already restarting on its own and
			// adding to that is the pathology, not the fix.
			skipped = append(skipped, name)
			continue
		}

		var err error
		if kind == watch.KindDocker {
			_, err = docker.Restart(name)
		} else {
			_, err = watch.Restart(kind, name, nil)
		}
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
			Output:  restartSummary(restarted, skipped, failed),
		}
	}
	if len(restarted) == 0 && len(skipped) > 0 {
		// Everything was suppressed. Reporting success would claim a
		// remediation that did not happen.
		return PlaybookResult{
			Action:  "restart",
			Success: false,
			Output:  restartSummary(restarted, skipped, failed),
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

// EffectiveKind returns the rule's target kind, defaulting to docker so that
// configs written before kind existed keep meaning what they meant.
func (r Rule) EffectiveKind() string {
	if r.Kind == "" {
		return watch.KindDocker
	}
	return r.Kind
}

func validKind(kind string) bool {
	for _, k := range watch.Kinds() {
		if k == kind {
			return true
		}
	}
	return false
}

// restartSummary reports every outcome rather than only the good one. A
// suppressed target is neither a success nor a failure and saying so is the
// difference between "nothing needed doing" and "we chose not to".
func restartSummary(restarted, skipped, failed []string) string {
	var parts []string
	if len(restarted) > 0 {
		parts = append(parts, fmt.Sprintf("restarted: [%s]", strings.Join(restarted, ", ")))
	}
	if len(skipped) > 0 {
		parts = append(parts, fmt.Sprintf("skipped while flapping: [%s]", strings.Join(skipped, ", ")))
	}
	if len(failed) > 0 {
		parts = append(parts, fmt.Sprintf("failed: [%s]", strings.Join(failed, "; ")))
	}
	if len(parts) == 0 {
		return "nothing to restart"
	}
	return strings.Join(parts, ", ")
}
