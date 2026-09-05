package proxmox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Version identifies a Proxmox VE release.
type Version struct {
	Version string `json:"version"`
	Release string `json:"release"`
	RepoID  string `json:"repo_id,omitempty"`
}

// ClusterStatus is an item returned by /cluster/status.
type ClusterStatus struct {
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	NodeID  int    `json:"nodeid,omitempty"`
	IP      string `json:"ip,omitempty"`
	Level   string `json:"level,omitempty"`
	ID      string `json:"id,omitempty"`
	Local   bool   `json:"local,omitempty"`
	Online  bool   `json:"online,omitempty"`
	Nodes   int    `json:"nodes,omitempty"`
	Version int    `json:"version,omitempty"`
	Quorate *bool  `json:"quorate,omitempty"`
}

func (s *ClusterStatus) UnmarshalJSON(data []byte) error {
	type rawClusterStatus struct {
		Type    string        `json:"type"`
		Name    string        `json:"name"`
		NodeID  int           `json:"nodeid"`
		IP      string        `json:"ip"`
		Level   string        `json:"level"`
		ID      string        `json:"id"`
		Local   flexibleBool  `json:"local"`
		Online  flexibleBool  `json:"online"`
		Nodes   int           `json:"nodes"`
		Version int           `json:"version"`
		Quorate *flexibleBool `json:"quorate"`
	}
	var raw rawClusterStatus
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = ClusterStatus{Type: raw.Type, Name: raw.Name, NodeID: raw.NodeID, IP: raw.IP, Level: raw.Level, ID: raw.ID, Local: bool(raw.Local), Online: bool(raw.Online), Nodes: raw.Nodes, Version: raw.Version}
	if raw.Quorate != nil {
		value := bool(*raw.Quorate)
		s.Quorate = &value
	}
	return nil
}

// Node is a node entry from /cluster/resources. Metric pointers distinguish
// missing metrics from measured zero values.
type Node struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Local       bool     `json:"local,omitempty"`
	CPU         *float64 `json:"cpu,omitempty"`
	MaxCPU      *float64 `json:"max_cpu,omitempty"`
	Mem         *int64   `json:"mem,omitempty"`
	MaxMem      *int64   `json:"max_mem,omitempty"`
	Uptime      *int64   `json:"uptime,omitempty"`
	Level       string   `json:"level,omitempty"`
	Fingerprint string   `json:"ssl_fingerprint,omitempty"`
}

func (n *Node) UnmarshalJSON(data []byte) error {
	type rawNode struct {
		Name        string       `json:"node"`
		Status      string       `json:"status"`
		Local       flexibleBool `json:"local"`
		CPU         *float64     `json:"cpu"`
		MaxCPU      *float64     `json:"maxcpu"`
		Mem         *int64       `json:"mem"`
		MaxMem      *int64       `json:"maxmem"`
		Uptime      *int64       `json:"uptime"`
		Level       string       `json:"level"`
		Fingerprint string       `json:"ssl_fingerprint"`
	}
	var raw rawNode
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*n = Node{Name: raw.Name, Status: raw.Status, Local: bool(raw.Local), CPU: raw.CPU, MaxCPU: raw.MaxCPU, Mem: raw.Mem, MaxMem: raw.MaxMem, Uptime: raw.Uptime, Level: raw.Level, Fingerprint: raw.Fingerprint}
	return nil
}

// Guest is the unified QEMU and LXC guest representation.
type Guest struct {
	VMID     int      `json:"vmid"`
	Name     string   `json:"name,omitempty"`
	Type     string   `json:"type"`
	Node     string   `json:"node"`
	Status   string   `json:"status"`
	Template bool     `json:"template"`
	Lock     string   `json:"lock,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Pool     string   `json:"pool,omitempty"`
	CPU      *float64 `json:"cpu,omitempty"`
	MaxCPU   *float64 `json:"max_cpu,omitempty"`
	Mem      *int64   `json:"mem,omitempty"`
	MaxMem   *int64   `json:"max_mem,omitempty"`
	Disk     *int64   `json:"disk,omitempty"`
	MaxDisk  *int64   `json:"max_disk,omitempty"`
	NetIn    *int64   `json:"net_in,omitempty"`
	NetOut   *int64   `json:"net_out,omitempty"`
	Uptime   *int64   `json:"uptime,omitempty"`
}

func (g *Guest) UnmarshalJSON(data []byte) error {
	type rawGuest struct {
		VMID     int          `json:"vmid"`
		Name     string       `json:"name"`
		Type     string       `json:"type"`
		Node     string       `json:"node"`
		Status   string       `json:"status"`
		Template flexibleBool `json:"template"`
		Lock     string       `json:"lock"`
		Tags     string       `json:"tags"`
		Pool     string       `json:"pool"`
		CPU      *float64     `json:"cpu"`
		MaxCPU   *float64     `json:"maxcpu"`
		Mem      *int64       `json:"mem"`
		MaxMem   *int64       `json:"maxmem"`
		Disk     *int64       `json:"disk"`
		MaxDisk  *int64       `json:"maxdisk"`
		NetIn    *int64       `json:"netin"`
		NetOut   *int64       `json:"netout"`
		Uptime   *int64       `json:"uptime"`
	}
	var raw rawGuest
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*g = Guest{VMID: raw.VMID, Name: raw.Name, Type: raw.Type, Node: raw.Node, Status: raw.Status, Template: bool(raw.Template), Lock: raw.Lock, Tags: splitTags(raw.Tags), Pool: raw.Pool, CPU: raw.CPU, MaxCPU: raw.MaxCPU, Mem: raw.Mem, MaxMem: raw.MaxMem, Disk: raw.Disk, MaxDisk: raw.MaxDisk, NetIn: raw.NetIn, NetOut: raw.NetOut, Uptime: raw.Uptime}
	return nil
}

// Store is a storage entry from /cluster/resources.
type Store struct {
	Name    string `json:"name"`
	Node    string `json:"node,omitempty"`
	Type    string `json:"type,omitempty"`
	Status  string `json:"status,omitempty"`
	Shared  bool   `json:"shared,omitempty"`
	Content string `json:"content,omitempty"`
	Used    *int64 `json:"used,omitempty"`
	Total   *int64 `json:"total,omitempty"`
}

func (s *Store) UnmarshalJSON(data []byte) error {
	type rawStore struct {
		Name    string       `json:"storage"`
		Node    string       `json:"node"`
		Type    string       `json:"plugintype"`
		Status  string       `json:"status"`
		Shared  flexibleBool `json:"shared"`
		Content string       `json:"content"`
		Used    *int64       `json:"disk"`
		Total   *int64       `json:"maxdisk"`
	}
	var raw rawStore
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = Store{Name: raw.Name, Node: raw.Node, Type: raw.Type, Status: raw.Status, Shared: bool(raw.Shared), Content: raw.Content, Used: raw.Used, Total: raw.Total}
	return nil
}

// Resources groups the known resource types returned by /cluster/resources.
// Unknown resource types are ignored so newer Proxmox responses remain usable.
type Resources struct {
	Nodes   []Node  `json:"nodes"`
	Guests  []Guest `json:"guests"`
	Storage []Store `json:"storage"`
}

// NodeStatus is the detailed data returned by /nodes/{node}/status.
type NodeStatus struct {
	CPU        *float64 `json:"cpu,omitempty"`
	Wait       *float64 `json:"wait,omitempty"`
	Uptime     *int64   `json:"uptime,omitempty"`
	LoadAvg    []string `json:"loadavg,omitempty"`
	PVEVersion string   `json:"pveversion,omitempty"`
	CPUInfo    CPUInfo  `json:"cpuinfo"`
	Memory     Memory   `json:"memory"`
	BootInfo   BootInfo `json:"boot_info"`
}

type CPUInfo struct {
	Cores  int    `json:"cores"`
	CPUs   int    `json:"cpus"`
	MHz    string `json:"mhz"`
	HVM    string `json:"hvm"`
	UserHZ int    `json:"user_hz"`
}

type Memory struct {
	Total     *int64 `json:"total,omitempty"`
	Used      *int64 `json:"used,omitempty"`
	Free      *int64 `json:"free,omitempty"`
	Available *int64 `json:"available,omitempty"`
}

type BootInfo struct {
	Mode       string `json:"mode,omitempty"`
	SecureBoot bool   `json:"secureboot"`
}

func (n *NodeStatus) UnmarshalJSON(data []byte) error {
	type rawBootInfo struct {
		Mode       string       `json:"mode"`
		SecureBoot flexibleBool `json:"secureboot"`
	}
	type rawNodeStatus struct {
		CPU        *float64    `json:"cpu"`
		Wait       *float64    `json:"wait"`
		Uptime     *int64      `json:"uptime"`
		LoadAvg    []string    `json:"loadavg"`
		PVEVersion string      `json:"pveversion"`
		CPUInfo    CPUInfo     `json:"cpuinfo"`
		Memory     Memory      `json:"memory"`
		BootInfo   rawBootInfo `json:"boot-info"`
	}
	var raw rawNodeStatus
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*n = NodeStatus{CPU: raw.CPU, Wait: raw.Wait, Uptime: raw.Uptime, LoadAvg: raw.LoadAvg, PVEVersion: raw.PVEVersion, CPUInfo: raw.CPUInfo, Memory: raw.Memory, BootInfo: BootInfo{Mode: raw.BootInfo.Mode, SecureBoot: bool(raw.BootInfo.SecureBoot)}}
	return nil
}

// Task is an entry returned by /nodes/{node}/tasks.
type Task struct {
	UPID      string `json:"upid"`
	Node      string `json:"node"`
	PID       int64  `json:"pid,omitempty"`
	PStart    int64  `json:"pstart,omitempty"`
	StartTime int64  `json:"starttime,omitempty"`
	EndTime   *int64 `json:"endtime,omitempty"`
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	User      string `json:"user,omitempty"`
	Status    string `json:"status"`
}

// GuestAction is one approved guest power operation.
type GuestAction string

const (
	GuestActionStart    GuestAction = "start"
	GuestActionShutdown GuestAction = "shutdown"
	GuestActionReboot   GuestAction = "reboot"
)

// TaskStatus is returned by /nodes/{node}/tasks/{upid}/status.
type TaskStatus struct {
	UPID       string `json:"upid"`
	Node       string `json:"node"`
	PID        int64  `json:"pid,omitempty"`
	PStart     int64  `json:"pstart,omitempty"`
	StartTime  int64  `json:"starttime,omitempty"`
	Type       string `json:"type"`
	ID         string `json:"id,omitempty"`
	User       string `json:"user,omitempty"`
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus,omitempty"`
	Result     string `json:"result"`
}

// DefaultView contains the three responses used by the initial status view.
type DefaultView struct {
	Version   Version         `json:"version"`
	Cluster   []ClusterStatus `json:"cluster"`
	Resources Resources       `json:"resources"`
	Warnings  []string        `json:"warnings,omitempty"`
	// Failed lists collectors that did not answer, and also collectors that
	// answered but returned a response that cannot be true for a connected
	// cluster (e.g. resources with no nodes, guests, or storage at all),
	// which points to an ACL-limited token rather than a genuinely empty
	// cluster.
	Failed []string `json:"failed_collectors,omitempty"`
	// FailureClasses gives UI and API callers a safe reason per failed collector.
	FailureClasses map[string]FailureClass `json:"failure_classes,omitempty"`
	// FirstErr is the first classified collector error, for callers that
	// branch on FailureClass rather than display text. Not serialized.
	FirstErr error `json:"-"`
}

// Collector names recorded in DefaultView.Failed.
const (
	CollectorVersion   = "version"
	CollectorCluster   = "cluster"
	CollectorResources = "resources"
)

// CollectorFailed reports whether a default status collector failed.
func (v DefaultView) CollectorFailed(name string) bool {
	for _, failed := range v.Failed {
		if failed == name {
			return true
		}
	}
	return false
}

type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("true")) || bytes.Equal(data, []byte("1")) {
		*b = true
		return nil
	}
	if bytes.Equal(data, []byte("false")) || bytes.Equal(data, []byte("0")) {
		*b = false
		return nil
	}
	return fmt.Errorf("expected boolean or 0/1, got %s", data)
}

// ExpectedGuest names one guest `watch start` expects to be running, by the
// exact triple Proxmox needs to address it.
type ExpectedGuest struct {
	Node string `yaml:"node"`
	Type string `yaml:"type"` // "qemu" or "lxc"
	VMID int    `yaml:"vmid"`
}

// Validate reports a malformed expected-guest entry without contacting Proxmox.
func (g ExpectedGuest) Validate() error {
	if strings.TrimSpace(g.Node) == "" {
		return fmt.Errorf("proxmox guest entry is missing node")
	}
	if g.Type != "qemu" && g.Type != "lxc" {
		return fmt.Errorf("proxmox guest entry for node %q has invalid type %q: must be qemu or lxc", g.Node, g.Type)
	}
	if g.VMID < 1 {
		return fmt.Errorf("proxmox guest entry for node %q has invalid vmid %d", g.Node, g.VMID)
	}
	return nil
}

func splitTags(tags string) []string {
	if tags == "" {
		return nil
	}
	return strings.Split(tags, ";")
}
