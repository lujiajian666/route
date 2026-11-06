// Package store persists the desired routes and daemon settings as JSON.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"be/internal/model"
	"be/internal/validate"
)

// ErrNotFound is returned when a host is not managed.
var ErrNotFound = errors.New("route not managed")

type fileData struct {
	Settings *model.Settings `json:"settings,omitempty"`
	Routes   []model.Route   `json:"routes"`
}

// Store is a concurrency-safe, file backed route repository.
type Store struct {
	mu       sync.RWMutex
	path     string
	settings model.Settings
	routes   map[string]model.Route
}

// New loads the store from path. A missing file is not an error; defaults are
// used until the first mutation writes the file.
func New(path string, defaults model.Settings) (*Store, error) {
	s := &Store{
		path:     path,
		settings: defaults.Normalized(),
		routes:   make(map[string]model.Route),
	}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}
	var data fileData
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
	}
	if data.Settings != nil {
		s.settings = data.Settings.Normalized()
	}
	for _, r := range data.Routes {
		host, gerr := validate.IPv4(r.Host)
		if gerr != nil {
			continue
		}
		gateway, gerr := validate.IPv4(r.Gateway)
		if gerr != nil || host == gateway {
			continue
		}
		r.Host, r.Gateway = host, gateway
		if r.CreatedAt.IsZero() {
			r.CreatedAt = time.Now()
		}
		if r.UpdatedAt.IsZero() {
			r.UpdatedAt = r.CreatedAt
		}
		s.routes[r.Host] = r
	}
	return s, nil
}

// Path returns the backing file path.
func (s *Store) Path() string { return s.path }

// Settings returns the current settings.
func (s *Store) Settings() model.Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// SetSettings validates, persists and stores new settings.
func (s *Store) SetSettings(next model.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = next.Normalized()
	return s.persistLocked()
}

// Routes returns a snapshot sorted by IPv4 address.
func (s *Store) Routes() []model.Route {
	s.mu.RLock()
	out := make([]model.Route, 0, len(s.routes))
	for _, r := range s.routes {
		out = append(out, r)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return lessIP(out[i].Host, out[j].Host) })
	return out
}

// Get returns one managed route.
func (s *Store) Get(host string) (model.Route, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.routes[host]
	return r, ok
}

// Upsert creates or updates a desired route and persists the result.
func (s *Store) Upsert(host, gateway string) (model.Route, bool, error) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.routes[host]
	route := model.Route{
		Host:      host,
		Gateway:   gateway,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if exists {
		route.CreatedAt = existing.CreatedAt
	}
	s.routes[host] = route
	if err := s.persistLocked(); err != nil {
		// Roll back the in-memory change so state stays consistent with disk.
		if exists {
			s.routes[host] = existing
		} else {
			delete(s.routes, host)
		}
		return model.Route{}, false, err
	}
	return route, !exists, nil
}

// Delete removes a desired route and persists the result.
func (s *Store) Delete(host string) (model.Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	route, ok := s.routes[host]
	if !ok {
		return model.Route{}, ErrNotFound
	}
	delete(s.routes, host)
	if err := s.persistLocked(); err != nil {
		s.routes[host] = route
		return model.Route{}, err
	}
	return route, nil
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	data := fileData{
		Settings: &s.settings,
		Routes:   make([]model.Route, 0, len(s.routes)),
	}
	for _, r := range s.routes {
		data.Routes = append(data.Routes, r)
	}
	sort.Slice(data.Routes, func(i, j int) bool { return lessIP(data.Routes[i].Host, data.Routes[j].Host) })

	body, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	body = append(body, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建配置目录 %s 失败: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".routes-*.json")
	if err != nil {
		return fmt.Errorf("创建临时配置文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("写入配置失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("同步配置失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时配置失败: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("设置配置权限失败: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	return nil
}

func lessIP(a, b string) bool {
	ia, ib := net.ParseIP(a), net.ParseIP(b)
	if ia == nil || ib == nil {
		return a < b
	}
	return bytesCompare(ia.To4(), ib.To4()) < 0
}

func bytesCompare(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return int(a[i]) - int(b[i])
		}
	}
	return len(a) - len(b)
}
