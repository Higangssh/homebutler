package system

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// systemctlShow is what systemctl 255 on the Raspberry Pi (Ubuntu 24.04)
// printed for these units, trimmed to the properties asked for. fwupd and
// packagekit are started over D-Bus and exit when idle; udisks2 has a bus name
// too but is enabled; polkit is static and D-Bus activated, but ModemManager
// requires it; dbus is static and socket activated, but wanted by
// multi-user.target; ssh is socket activated and disabled.
const systemctlShow = `BusName=org.freedesktop.fwupd
Id=fwupd.service
RequiredBy=
WantedBy=
TriggeredBy=
LoadState=loaded
UnitFileState=static

BusName=org.freedesktop.PackageKit
Id=packagekit.service
RequiredBy=
WantedBy=
TriggeredBy=
LoadState=loaded
UnitFileState=static

BusName=org.freedesktop.UDisks2
Id=udisks2.service
RequiredBy=
WantedBy=graphical.target
TriggeredBy=
LoadState=loaded
UnitFileState=enabled

BusName=org.freedesktop.PolicyKit1
Id=polkit.service
RequiredBy=ModemManager.service
WantedBy=
TriggeredBy=
LoadState=loaded
UnitFileState=static

BusName=
Id=dbus.service
RequiredBy=
WantedBy=multi-user.target
TriggeredBy=dbus.socket
LoadState=loaded
UnitFileState=static

BusName=
Id=ssh.service
RequiredBy=
WantedBy=
TriggeredBy=ssh.socket
LoadState=loaded
UnitFileState=disabled

BusName=
Id=apt-daily.service
RequiredBy=
WantedBy=
TriggeredBy=apt-daily.timer
LoadState=loaded
UnitFileState=static
`

type fakeSystemctl struct {
	out   string
	err   error
	calls [][]string
	// block makes the call wait for its context, as a hung systemctl would.
	block bool
}

func (f *fakeSystemctl) run(ctx context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if f.block {
		<-ctx.Done()
		return f.out, ctx.Err()
	}
	return f.out, f.err
}

func cgroups(byPID map[int]string) func(int) (string, error) {
	return func(pid int) (string, error) {
		c, ok := byPID[pid]
		if !ok {
			return "", errors.New("no such process")
		}
		return c, nil
	}
}

func onDemandPIDs(procs []ProcessInfo) []int {
	var out []int
	for _, p := range procs {
		if p.OnDemand {
			out = append(out, p.PID)
		}
	}
	return out
}

func piProcesses() []ProcessInfo {
	return []ProcessInfo{
		{PID: 1, Name: "systemd"},
		{PID: 10, Name: "fwupd"},
		{PID: 11, Name: "packagekitd"},
		{PID: 12, Name: "udisksd"},
		{PID: 13, Name: "polkitd"},
		{PID: 14, Name: "dbus-daemon"},
		{PID: 15, Name: "sshd"},
		{PID: 16, Name: "apt-get"},
		{PID: 17, Name: "bash"},
		{PID: 18, Name: "fwupd-helper"},
		{PID: 19, Name: "sleep"},
		{PID: 20, Name: "gone-already"},
	}
}

var piCgroups = map[int]string{
	1:  "0::/init.scope\n",
	10: "0::/system.slice/fwupd.service\n",
	11: "0::/system.slice/packagekit.service\n",
	12: "0::/system.slice/udisks2.service\n",
	13: "0::/system.slice/polkit.service\n",
	14: "0::/system.slice/dbus.service\n",
	15: "0::/system.slice/ssh.service\n",
	16: "0::/system.slice/apt-daily.service\n",
	17: "0::/user.slice/user-1000.slice/session-5568.scope\n",
	// A child in a sub-cgroup of the service is still the service's.
	18: "0::/system.slice/fwupd.service/helper\n",
	// A user unit has the same name as an on-demand system one. It is not
	// the system manager's, and is not asked about.
	19: "0::/user.slice/user-1000.slice/user@1000.service/app.slice/fwupd.service\n",
}

// #363: fwupd and packagekit are started over D-Bus and exit once idle, so
// whether one is running at a report is chance. udisks2 has a bus name too,
// and is enabled.
func TestOnDemandServicesAreTheStaticOnesNothingWantsAndSomethingActivates(t *testing.T) {
	sc := &fakeSystemctl{out: systemctlShow}
	procs := piProcesses()
	UnitLookup{Cgroup: cgroups(piCgroups), Systemctl: sc.run, Timeout: time.Second}.Mark(procs)

	if got, want := onDemandPIDs(procs), []int{10, 11, 16, 18}; !reflect.DeepEqual(got, want) {
		t.Errorf("on demand: got pids %v, want %v (fwupd, packagekitd, apt-get, fwupd's helper)", got, want)
	}
	if len(sc.calls) != 1 {
		t.Fatalf("want one systemctl call for every unit, got %d", len(sc.calls))
	}
	want := []string{"show", "--no-pager", "--property=Id,LoadState,UnitFileState,WantedBy,RequiredBy,BusName,TriggeredBy", "--",
		"apt-daily.service", "dbus.service", "fwupd.service", "packagekit.service", "polkit.service", "ssh.service", "udisks2.service"}
	if !reflect.DeepEqual(sc.calls[0], want) {
		t.Errorf("systemctl args:\n got %q\nwant %q", sc.calls[0], want)
	}
}

// cgroup v1 with the unified hierarchy alongside, as Ubuntu 20.04 and
// Raspberry Pi OS before bookworm boot: the unit is on the name=systemd line.
func TestTheUnitIsReadFromAHybridCgroupHierarchy(t *testing.T) {
	sc := &fakeSystemctl{out: systemctlShow}
	procs := []ProcessInfo{{PID: 10, Name: "fwupd"}}
	v1 := map[int]string{10: "12:cpuset:/\n11:memory:/system.slice/fwupd.service\n1:name=systemd:/system.slice/fwupd.service\n0::/system.slice/fwupd.service\n"}
	UnitLookup{Cgroup: cgroups(v1), Systemctl: sc.run, Timeout: time.Second}.Mark(procs)
	if !procs[0].OnDemand {
		t.Error("fwupd in a hybrid hierarchy was not found on demand")
	}
}

// Whenever the unit cannot be determined, nothing is marked, so the report
// keeps every line it would have printed before.
func TestNothingIsOnDemandWhenSystemdCannotSay(t *testing.T) {
	cases := map[string]struct {
		cgroup map[int]string
		sc     *fakeSystemctl
	}{
		"systemctl failed":      {piCgroups, &fakeSystemctl{out: systemctlShow, err: errors.New("exit status 1")}},
		"systemctl hung":        {piCgroups, &fakeSystemctl{block: true, out: systemctlShow}},
		"systemctl said other":  {piCgroups, &fakeSystemctl{out: "Failed to connect to bus: No such file or directory"}},
		"no cgroup readable":    {map[int]string{}, &fakeSystemctl{out: systemctlShow}},
		"not under systemd":     {map[int]string{10: "0::/\n", 11: "0::/docker/abc\n"}, &fakeSystemctl{out: systemctlShow}},
		"unit not in the reply": {map[int]string{10: "0::/system.slice/fwupd.service\n"}, &fakeSystemctl{out: "Id=other.service\nUnitFileState=static\nBusName=x\n"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			procs := piProcesses()
			start := time.Now()
			UnitLookup{Cgroup: cgroups(tc.cgroup), Systemctl: tc.sc.run, Timeout: 50 * time.Millisecond}.Mark(procs)
			if got := onDemandPIDs(procs); len(got) != 0 {
				t.Errorf("marked %v on demand", got)
			}
			if took := time.Since(start); took > 2*time.Second {
				t.Errorf("took %v; the systemctl call is not bounded", took)
			}
		})
	}
}

// A machine with no system services to ask about does not run systemctl.
func TestSystemctlIsNotRunWithNoUnitToAskAbout(t *testing.T) {
	sc := &fakeSystemctl{out: systemctlShow}
	procs := []ProcessInfo{{PID: 17, Name: "bash"}}
	UnitLookup{Cgroup: cgroups(piCgroups), Systemctl: sc.run, Timeout: time.Second}.Mark(procs)
	if len(sc.calls) != 0 {
		t.Errorf("systemctl was run for no unit: %v", sc.calls)
	}
}
