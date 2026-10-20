// Package portlimit manages only its own tc filters and shared police actions.
package portlimit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cookie = "33787569706f72746c696d6974"

// iproute2 versions encode flower handles as either JSON numbers or hex strings.
type filterHandle string

func (h *filterHandle) UnmarshalJSON(data []byte) error {
	var value uint64
	var text string
	if json.Unmarshal(data, &text) == nil {
		n, err := strconv.ParseUint(text, 0, 32)
		if err != nil {
			return err
		}
		value = n
	} else {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	}
	*h = filterHandle(fmt.Sprintf("0x%x", value))
	return nil
}

type Rule struct {
	ID        int    `json:"id"`
	UserID    int    `json:"userId"`
	Port      int    `json:"port"`
	Interface string `json:"interface"`
	Upload    int    `json:"upload"` // Decimal Mbit/s, client -> server.
	Download  int    `json:"download"`
}
type Runner func(...string) ([]byte, error)
type Manager struct {
	mu             sync.Mutex
	path           string
	run            Runner
	rules          map[int]Rule
	loaded         bool
	lastError      string
	checkInterface func(string) error
}

func New(path string, run Runner) *Manager {
	if run == nil {
		run = func(args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "tc", args...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return out, fmt.Errorf("tc %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			}
			return out, nil
		}
	}
	return &Manager{path: path, run: run, rules: map[int]Rule{}, checkInterface: func(name string) error { _, err := net.InterfaceByName(name); return err }}
}
func Validate(r Rule) error {
	if r.ID < 1 || r.UserID < 1 || r.Port < 1 || r.Port > 65535 {
		return errors.New("无效的入站或端口")
	}
	if len(r.Interface) == 0 || len(r.Interface) > 15 || strings.ContainsAny(r.Interface, " /\t\n\x00") || r.Interface == "lo" {
		return errors.New("无效的出口网卡")
	}
	if r.Upload < 0 || r.Download < 0 || r.Upload > 100000 || r.Download > 100000 {
		return errors.New("速率必须为 0–100000 的整数，单位 Mbps；0 为不限速")
	}
	return nil
}
func (m *Manager) load() error {
	if m.loaded {
		return nil
	}
	b, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		m.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	var rows []Rule
	if err = json.Unmarshal(b, &rows); err != nil {
		return fmt.Errorf("限速配置损坏：%w", err)
	}
	parsed := map[int]Rule{}
	for _, r := range rows {
		if err = Validate(r); err != nil {
			return err
		}
		if _, exists := parsed[r.Port]; exists {
			return errors.New("限速配置存在重复端口")
		}
		parsed[r.Port] = r
	}
	m.rules = parsed
	m.loaded = true
	return nil
}
func (m *Manager) save() error {
	rows := []Rule{}
	for _, r := range m.rules {
		rows = append(rows, r)
	}
	b, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".port-limits-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, m.path)
}
func actionID(port, dir int) string { return strconv.Itoa(0x58490000 + port*2 + dir) }
func filterArgs(r Rule, dir, family, transport int) []string {
	direction, key := "ingress", "dst_port"
	if dir == 1 {
		direction, key = "egress", "src_port"
	}
	proto := "ip"
	if family == 1 {
		proto = "ipv6"
	}
	l4 := "tcp"
	if transport == 1 {
		l4 = "udp"
	}
	return []string{"filter", "add", "dev", r.Interface, direction, "protocol", proto, "pref", strconv.Itoa(49152 + family), "handle", fmt.Sprintf("0x%x", r.Port*2+transport), "flower", "skip_hw", "ip_proto", l4, key, strconv.Itoa(r.Port), "action", "police", "index", actionID(r.Port, dir)}
}
func (m *Manager) cmd(args ...string) error { _, err := m.run(args...); return err }

// Action cookies prevent updating an action owned by another application.
func (m *Manager) ownAction(id string) (bool, error) {
	out, err := m.run("-j", "actions", "get", "action", "police", "index", id)
	if err != nil {
		text := strings.ToLower(string(out) + err.Error())
		if strings.Contains(text, "no such file") || strings.Contains(text, "failed to find") || strings.Contains(text, "cannot find") || strings.Contains(text, "not found") {
			return false, nil
		}
		return false, err
	}
	if !strings.Contains(strings.ToLower(string(out)), cookie) {
		return false, errors.New("tc 动作编号被其他程序使用，拒绝覆盖")
	}
	return true, nil
}
func (m *Manager) install(r Rule) error {
	out, err := m.run("-j", "qdisc", "show", "dev", r.Interface)
	if err != nil {
		return err
	}
	var qs []struct {
		Kind string `json:"kind"`
	}
	if err = json.Unmarshal(out, &qs); err != nil {
		return err
	}
	has := false
	for _, q := range qs {
		if q.Kind == "clsact" {
			has = true
		}
		if q.Kind == "ingress" {
			return errors.New("网卡已有 ingress 队列，请先处理现有规则；不会覆盖")
		}
	}
	if !has {
		if err = m.cmd("qdisc", "add", "dev", r.Interface, "clsact"); err != nil {
			return err
		}
	}
	for dir, rate := range []int{r.Upload, r.Download} {
		if rate == 0 {
			continue
		}
		id := actionID(r.Port, dir)
		owned, err := m.ownAction(id)
		if err != nil {
			return err
		}
		op := "add"
		if owned {
			op = "replace"
		}
		// All TCP/UDP + IPv4/IPv6 filters share this bucket, so the cap is aggregated.
		burst := rate * 1000000 / 8 / 20
		if burst < 131072 {
			burst = 131072
		}
		if err = m.cmd("actions", op, "action", "police", "rate", fmt.Sprintf("%dmbit", rate), "burst", strconv.Itoa(burst), "mtu", "65535", "conform-exceed", "drop/ok", "index", id, "cookie", cookie, "skip_hw"); err != nil {
			return err
		}
		for family := 0; family < 2; family++ {
			for transport := 0; transport < 2; transport++ {
				args := filterArgs(r, dir, family, transport)
				list, err := m.run("-j", "filter", "show", "dev", r.Interface, args[4], "pref", args[8])
				if err != nil {
					return err
				}
				var filters []struct {
					Options struct {
						Handle  filterHandle      `json:"handle"`
						Actions []json.RawMessage `json:"actions"`
					} `json:"options"`
				}
				if err = json.Unmarshal(list, &filters); err != nil {
					return err
				}
				for _, f := range filters {
					if string(f.Options.Handle) == args[10] {
						data, _ := json.Marshal(f.Options.Actions)
						if !strings.Contains(strings.ToLower(string(data)), cookie) {
							return errors.New("过滤器编号被其他程序占用，拒绝覆盖")
						}
						args[1] = "replace"
					}
				}
				if err = m.cmd(args...); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (m *Manager) remove(r Rule) error {
	var errs []error
	for dir, rate := range []int{r.Upload, r.Download} {
		if rate == 0 {
			continue
		}
		id := actionID(r.Port, dir)
		owned, err := m.ownAction(id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !owned {
			continue
		}
		direction := "ingress"
		if dir == 1 {
			direction = "egress"
		}
		for family := 0; family < 2; family++ {
			out, err := m.run("-j", "filter", "show", "dev", r.Interface, direction, "pref", strconv.Itoa(49152+family))
			if err != nil {
				errs = append(errs, err)
				continue
			}
			var filters []struct {
				Options struct {
					Handle  filterHandle      `json:"handle"`
					Actions []json.RawMessage `json:"actions"`
				} `json:"options"`
			}
			if err = json.Unmarshal(out, &filters); err != nil {
				errs = append(errs, err)
				continue
			}
			for transport := 0; transport < 2; transport++ {
				handle := fmt.Sprintf("0x%x", r.Port*2+transport)
				for _, f := range filters {
					if string(f.Options.Handle) != handle {
						continue
					}
					data, _ := json.Marshal(f.Options.Actions)
					if !strings.Contains(strings.ToLower(string(data)), cookie) {
						errs = append(errs, errors.New("tc 过滤规则已被其他程序更改，拒绝删除"))
						continue
					}
					proto := "ip"
					if family == 1 {
						proto = "ipv6"
					}
					if err = m.cmd("filter", "del", "dev", r.Interface, direction, "protocol", proto, "pref", strconv.Itoa(49152+family), "handle", handle, "flower"); err != nil {
						errs = append(errs, err)
					}
				}
			}
		}
		if len(errs) == 0 {
			if err = m.cmd("actions", "del", "action", "police", "index", id); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
func (m *Manager) Set(r Rule) error {
	if err := Validate(r); err != nil {
		return err
	}
	if r.Upload != 0 || r.Download != 0 {
		if err := m.checkInterface(r.Interface); err != nil {
			return errors.New("网卡不存在")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return err
	}
	old, exists := m.rules[r.Port]
	if exists && (old.UserID != r.UserID || old.ID != r.ID) {
		return errors.New("端口已由其他入站配置限速")
	}
	if !exists && len(m.rules) >= 128 {
		return errors.New("最多支持 128 个限速端口")
	}
	if exists {
		if err := m.remove(old); err != nil {
			restore := m.install(old)
			m.lastError = fmt.Sprint(errors.Join(err, restore))
			return errors.New(m.lastError)
		}
	}
	rollback := func(cause error) error {
		cleanup := m.remove(r)
		var restore error
		if exists {
			restore = m.install(old)
		}
		m.lastError = fmt.Sprint(errors.Join(cause, cleanup, restore))
		return errors.New(m.lastError)
	}
	if r.Upload != 0 || r.Download != 0 {
		if err := m.install(r); err != nil {
			return rollback(err)
		}
		m.rules[r.Port] = r
	} else {
		delete(m.rules, r.Port)
	}
	if err := m.save(); err != nil {
		if exists {
			m.rules[r.Port] = old
		} else {
			delete(m.rules, r.Port)
		}
		return rollback(err)
	}
	m.lastError = ""
	return nil
}
func (m *Manager) Snapshot(userID int) ([]Rule, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return []Rule{}, err.Error()
	}
	rows := []Rule{}
	for _, r := range m.rules {
		if r.UserID == userID {
			r.UserID = 0
			rows = append(rows, r)
		}
	}
	return rows, m.lastError
}

// Restore reattaches persisted limits after reboot without replacing root qdiscs.
func (m *Manager) Restore() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		m.lastError = err.Error()
		return err
	}
	var errs []error
	for _, r := range m.rules {
		if err := m.remove(r); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := m.install(r); err != nil {
			errs = append(errs, err)
		}
	}
	err := errors.Join(errs...)
	if err != nil {
		m.lastError = err.Error()
	}
	return err
}
