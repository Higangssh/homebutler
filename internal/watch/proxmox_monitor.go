package watch

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Higangssh/homebutler/internal/proxmox"
)

// Proxmox incident states. Kept distinct from restart-monitor Container
// values by the "proxmox-" prefix built in guestKey/endpointKey, so an
// endpoint and a docker container can never collide in incidents/ or in
// flapping/notification fingerprints.
const (
	ProxmoxStateUnavailable = "unavailable"
	ProxmoxStateACLFiltered = "acl_filtered"
	ProxmoxStateGuestDown   = "guest_down"
)

// ProxmoxClassEmptyResult marks a response that authenticated but returned no
// nodes, guests, or storage at all — the same heuristic client.DefaultView
// uses, and the fifth failure class #104 asks to keep distinguishable from a
// genuine 403.
const ProxmoxClassEmptyResult = "empty_result"

// ProxmoxTarget is one configured Proxmox endpoint to poll, together with the
// guests on it that are expected to stay running.
type ProxmoxTarget struct {
	Endpoint string
	Client   *proxmox.Client
	Guests   []proxmox.ExpectedGuest
}

// ProxmoxMonitor polls Proxmox endpoints for reachability and for the
// running state of explicitly configured guests. It performs GETs only.
type ProxmoxMonitor struct {
	Targets  []ProxmoxTarget
	Dir      string
	Interval time.Duration
	Keep     int
}

type proxmoxEndpointState struct {
	state string // "" (healthy), ProxmoxStateUnavailable, or ProxmoxStateACLFiltered
	class string
}

func endpointKey(name string) string {
	return "proxmox-" + name
}

func guestKey(endpoint string, g proxmox.ExpectedGuest) string {
	return fmt.Sprintf("proxmox-%s-%s-%s-%d", endpoint, g.Node, g.Type, g.VMID)
}

// Watch polls each endpoint's resources and reports endpoint and configured
// guest state transitions. A collector failure on one endpoint never blocks
// the others, and a guest state read failure on one endpoint never discards
// resources it already fetched for the rest of that poll.
func (pm *ProxmoxMonitor) Watch(ctx context.Context, incidents chan<- Incident) error {
	if len(pm.Targets) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	interval := pm.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}

	endpointPrev := make(map[string]proxmoxEndpointState, len(pm.Targets))
	guestPrev := make(map[string]bool)

	// Seed without emitting, so a guest that is already stopped or an
	// endpoint that is already unreachable when watch starts does not fire an
	// incident for a state that was never observed to change.
	if err := pm.poll(ctx, endpointPrev, guestPrev, nil); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := pm.poll(ctx, endpointPrev, guestPrev, incidents); err != nil {
				return err
			}
		}
	}
}

func (pm *ProxmoxMonitor) poll(ctx context.Context, endpointPrev map[string]proxmoxEndpointState, guestPrev map[string]bool, incidents chan<- Incident) error {
	for _, target := range pm.Targets {
		resources, err := target.Client.Resources(ctx)

		var curr proxmoxEndpointState
		switch {
		case err != nil:
			switch proxmox.Classify(err) {
			case proxmox.FailureAuthorization:
				curr = proxmoxEndpointState{state: ProxmoxStateACLFiltered, class: string(proxmox.FailureAuthorization)}
			case proxmox.FailureTLS:
				curr = proxmoxEndpointState{state: ProxmoxStateUnavailable, class: string(proxmox.FailureTLS)}
			case proxmox.FailureAuthentication:
				curr = proxmoxEndpointState{state: ProxmoxStateUnavailable, class: string(proxmox.FailureAuthentication)}
			default:
				curr = proxmoxEndpointState{state: ProxmoxStateUnavailable, class: string(proxmox.FailureTransport)}
			}
		case len(resources.Nodes) == 0 && len(resources.Guests) == 0 && len(resources.Storage) == 0:
			curr = proxmoxEndpointState{state: ProxmoxStateACLFiltered, class: ProxmoxClassEmptyResult}
		default:
			curr = proxmoxEndpointState{} // healthy
		}

		key := endpointKey(target.Endpoint)
		if prev, ok := endpointPrev[key]; ok && incidents != nil && prev.state != curr.state {
			if err := pm.emitEndpointIncident(ctx, target.Endpoint, prev, curr, incidents); err != nil {
				return err
			}
		}
		endpointPrev[key] = curr

		// A failed collector carries no resources to check guests against.
		// Guest state before the failure is left exactly as it was rather
		// than guessed at, matching #104's "preserve partial results".
		if err != nil {
			continue
		}

		running := make(map[string]bool, len(resources.Guests))
		for _, guest := range resources.Guests {
			running[fmt.Sprintf("%s-%s-%d", guest.Node, guest.Type, guest.VMID)] = guest.Status == "running"
		}

		for _, expected := range target.Guests {
			key := guestKey(target.Endpoint, expected)
			isRunning := running[fmt.Sprintf("%s-%s-%d", expected.Node, expected.Type, expected.VMID)]

			if prev, ok := guestPrev[key]; ok && incidents != nil && prev != isRunning {
				if err := pm.emitGuestIncident(ctx, target.Endpoint, expected, isRunning, incidents); err != nil {
					return err
				}
			}
			guestPrev[key] = isRunning
		}
	}
	return nil
}

func (pm *ProxmoxMonitor) emitEndpointIncident(ctx context.Context, endpoint string, prev, curr proxmoxEndpointState, incidents chan<- Incident) error {
	recovered := curr.state == ""
	state, class := curr.state, curr.class
	if recovered {
		state, class = prev.state, prev.class
	}

	now := time.Now()
	inc := Incident{
		ID:           GenerateIncidentID(endpointKey(endpoint), now),
		Container:    endpointKey(endpoint),
		DetectedAt:   now,
		Source:       "proxmox",
		ProxmoxState: state,
		ProxmoxClass: class,
		Recovered:    recovered,
		PostLogs:     proxmoxStateMessage(endpoint, "", state, class, recovered),
	}
	pm.save(&inc)
	return send(ctx, incidents, inc)
}

func (pm *ProxmoxMonitor) emitGuestIncident(ctx context.Context, endpoint string, guest proxmox.ExpectedGuest, isRunning bool, incidents chan<- Incident) error {
	label := fmt.Sprintf("%s/%s/%d", guest.Node, guest.Type, guest.VMID)
	now := time.Now()
	inc := Incident{
		ID:           GenerateIncidentID(guestKey(endpoint, guest), now),
		Container:    guestKey(endpoint, guest),
		DetectedAt:   now,
		Source:       "proxmox",
		ProxmoxState: ProxmoxStateGuestDown,
		Recovered:    isRunning,
		PostLogs:     proxmoxStateMessage(endpoint, label, ProxmoxStateGuestDown, "", isRunning),
	}
	pm.save(&inc)
	return send(ctx, incidents, inc)
}

func proxmoxStateMessage(endpoint, guest, state, class string, recovered bool) string {
	target := endpoint
	if guest != "" {
		target = endpoint + " " + guest
	}
	if recovered {
		return fmt.Sprintf("proxmox %s recovered from %s", target, state)
	}
	if class != "" {
		return fmt.Sprintf("proxmox %s is %s (%s)", target, state, class)
	}
	return fmt.Sprintf("proxmox %s is %s", target, state)
}

func (pm *ProxmoxMonitor) save(inc *Incident) {
	if pm.Dir == "" {
		return
	}
	if err := SaveIncident(pm.Dir, inc, pm.Keep); err != nil {
		fmt.Fprintf(os.Stderr, "[proxmox-monitor] warning: save incident: %v\n", err)
	}
}

func send(ctx context.Context, incidents chan<- Incident, inc Incident) error {
	if incidents == nil {
		return nil
	}
	select {
	case incidents <- inc:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
