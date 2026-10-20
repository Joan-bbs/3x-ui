package portlimit

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeTC struct {
	actions  map[string]bool
	filters  map[string][]string
	calls    []string
	failOnce bool
	deny     bool
}

func newFake() *fakeTC { return &fakeTC{actions: map[string]bool{}, filters: map[string][]string{}} }
func value(a []string, k string) string {
	for i, s := range a {
		if s == k && i+1 < len(a) {
			return a[i+1]
		}
	}
	return ""
}
func (f *fakeTC) run(a ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(a, " "))
	if f.deny {
		return nil, fmt.Errorf("Operation not permitted")
	}
	if a[0] == "-j" {
		switch a[1] {
		case "qdisc":
			return []byte(`[{"kind":"clsact"}]`), nil
		case "actions":
			if f.actions[value(a, "index")] {
				return []byte(`{"actions":[{"cookie":"` + cookie + `"}]}`), nil
			}
			return nil, fmt.Errorf("Failed to find specified action")
		case "filter":
			rows := []any{}
			for _, v := range f.filters {
				if v[3] == a[4] && v[4] == a[5] && v[8] == value(a, "pref") {
					rows = append(rows, map[string]any{"options": map[string]any{"handle": v[10], "actions": []any{map[string]any{"cookie": cookie}}}})
				}
			}
			b, _ := json.Marshal(rows)
			return b, nil
		}
	}
	if a[0] == "actions" {
		id := value(a, "index")
		if a[1] == "del" {
			delete(f.actions, id)
		} else {
			if a[1] == "add" && f.actions[id] {
				return nil, fmt.Errorf("exists")
			}
			f.actions[id] = true
		}
		return nil, nil
	}
	if a[0] == "filter" {
		key := a[3] + a[4] + a[6] + a[10]
		if a[1] == "del" {
			delete(f.filters, key)
			return nil, nil
		}
		if f.failOnce && strings.Contains(strings.Join(a, " "), "udp") {
			f.failOnce = false
			return nil, fmt.Errorf("simulated unsupported rule")
		}
		if a[1] == "add" && f.filters[key] != nil {
			return nil, fmt.Errorf("exists")
		}
		f.filters[key] = append([]string{}, a...)
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected command %v", a)
}
func mockManager(path string, run Runner) *Manager {
	m := New(path, run)
	m.checkInterface = func(string) error { return nil }
	return m
}
func TestLimitLifecycle(t *testing.T) {
	f := newFake()
	path := filepath.Join(t.TempDir(), "limits.json")
	m := mockManager(path, f.run)
	r := Rule{ID: 1, UserID: 2, Port: 65535, Interface: "pl0", Upload: 10, Download: 20}
	if err := m.Set(r); err != nil {
		t.Fatal(err)
	}
	if len(f.filters) != 8 || len(f.actions) != 2 {
		t.Fatalf("must share two actions among eight filters: %d %d", len(f.filters), len(f.actions))
	}
	for _, v := range f.filters {
		dir := 0
		if v[4] == "egress" {
			dir = 1
		}
		if value(v, "index") != actionID(r.Port, dir) {
			t.Fatal("separate bucket per protocol")
		}
	}
	if b, err := os.ReadFile(path); err != nil || !strings.Contains(string(b), `"userId": 2`) {
		t.Fatal("not persisted", err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0600 {
		t.Fatal("unsafe config permissions")
	}
	if rows, _ := m.Snapshot(99); len(rows) != 0 {
		t.Fatal("cross-user disclosure")
	}
	m = mockManager(path, f.run)
	if err := m.Restore(); err != nil {
		t.Fatal("restore", err)
	}
	r.Download = 30
	if err := m.Set(r); err != nil {
		t.Fatal("update", err)
	}
	r.UserID = 99
	if err := m.Set(r); err == nil {
		t.Fatal("ownership bypass")
	}
	r.UserID = 2
	r.Upload = 0
	r.Download = 0
	if err := m.Set(r); err != nil {
		t.Fatal("remove", err)
	}
	if len(f.filters) != 0 || len(f.actions) != 0 {
		t.Fatal("residual limits")
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "actions add ") || strings.HasPrefix(call, "actions replace ") {
			if !strings.Contains(call, "skip_hw") {
				t.Fatal("shared action must use the filter's software-only flags", call)
			}
		}
		if strings.Contains(call, "qdisc del") || strings.Contains(call, " root") || strings.Contains(call, "flush") {
			t.Fatal("destructive tc command", call)
		}
	}
}
func TestRollbackAndDeniedTC(t *testing.T) {
	f := newFake()
	m := mockManager(filepath.Join(t.TempDir(), "limits.json"), f.run)
	r := Rule{ID: 1, UserID: 1, Port: 443, Interface: "pl0", Upload: 10, Download: 20}
	if err := m.Set(r); err != nil {
		t.Fatal(err)
	}
	f.failOnce = true
	r.Upload = 30
	if err := m.Set(r); err == nil {
		t.Fatal("failure accepted")
	}
	rows, _ := m.Snapshot(1)
	if len(rows) != 1 || rows[0].Upload != 10 || len(f.filters) != 8 || len(f.actions) != 2 {
		t.Fatal("failed to restore old limit", rows, len(f.filters))
	}
	f.deny = true
	r.Upload = 0
	r.Download = 0
	if err := m.Set(r); err == nil {
		t.Fatal("permission error accepted as deleted")
	}
	rows, _ = m.Snapshot(1)
	if len(rows) != 1 {
		t.Fatal("lost configuration after denied removal")
	}
}
func TestLimitValidation(t *testing.T) {
	good := Rule{ID: 1, UserID: 1, Port: 443, Interface: "eth0", Upload: 10}
	for _, port := range []int{1, 443, 65535} {
		good.Port = port
		if err := Validate(good); err != nil {
			t.Fatal(err)
		}
	}
	good.Interface = "eth0; reboot"
	if err := Validate(good); err == nil {
		t.Fatal("bad interface")
	}
	good.Interface = "eth0"
	good.Upload = -1
	if err := Validate(good); err == nil {
		t.Fatal("bad rate")
	}
}

// Runs only inside CI's isolated network namespace; never touches the runner NIC.
func TestKernelLimit(t *testing.T) {
	if os.Getenv("PORT_LIMIT_KERNEL") != "1" {
		t.Skip("requires isolated Linux netns")
	}
	m := New(filepath.Join(t.TempDir(), "limits.json"), nil)
	r := Rule{ID: 1, UserID: 1, Port: 65535, Interface: "pl0", Upload: 10, Download: 20}
	if err := m.Set(r); err != nil {
		t.Fatal(err)
	}
	r.Upload = 15
	if err := m.Set(r); err != nil {
		t.Fatal("update", err)
	}
	if err := m.Restore(); err != nil {
		t.Fatal("restore", err)
	}
	// Exercise real upload/download policing across a veth pair.
	cmd := func(args ...string) {
		t.Helper()
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	ns := "pl-client"
	cmd("ip", "netns", "add", ns)
	defer exec.Command("ip", "netns", "del", ns).Run()
	cmd("ip", "link", "set", "pl1", "netns", ns)
	cmd("ip", "addr", "add", "172.31.252.1/24", "dev", "pl0")
	cmd("ip", "netns", "exec", ns, "ip", "addr", "add", "172.31.252.2/24", "dev", "pl1")
	cmd("ip", "netns", "exec", ns, "ip", "link", "set", "pl1", "up")
	cmd("ip", "netns", "exec", ns, "ip", "link", "set", "lo", "up")
	server := exec.Command("iperf3", "-s", "-B", "172.31.252.1", "-p", "65535")
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { server.Process.Kill(); server.Wait() }()
	time.Sleep(500 * time.Millisecond)
	r.Upload = 5
	r.Download = 7
	if err := m.Set(r); err != nil {
		t.Fatal(err)
	}
	for _, reverse := range []bool{false, true} {
		args := []string{"netns", "exec", ns, "iperf3", "-c", "172.31.252.1", "-p", "65535", "-u", "-b", "30M", "-t", "4", "-J"}
		cap := r.Upload
		if reverse {
			args = append(args, "-R")
			cap = r.Download
		}
		out, err := exec.Command("ip", args...).CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		var result struct {
			Error string `json:"error"`
			End   struct {
				Received struct {
					Rate float64 `json:"bits_per_second"`
				} `json:"sum_received"`
			} `json:"end"`
		}
		if err = json.Unmarshal(out, &result); err != nil || result.Error != "" {
			t.Fatal(err, string(out))
		}
		rate := result.End.Received.Rate
		if rate < 100000 || rate > float64(cap)*1000000*1.5 {
			t.Fatalf("reverse=%v cap=%dMbps actual=%.2fMbps output=%s", reverse, cap, rate/1e6, out)
		}
		t.Logf("reverse=%v configured=%dMbps actual=%.2fMbps", reverse, cap, rate/1e6)
	}
	r.Upload = 0
	r.Download = 0
	if err := m.Set(r); err != nil {
		t.Fatal("delete", err)
	}
	for _, direction := range []string{"ingress", "egress"} {
		out, err := exec.Command("tc", "-j", "filter", "show", "dev", "pl0", direction).CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		if strings.Contains(string(out), cookie) {
			t.Fatal("residual kernel filter", string(out))
		}
	}
}
