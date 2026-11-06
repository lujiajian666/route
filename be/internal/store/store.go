// Package store 负责托管路由的持久化。
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

	"be/internal/model"
	"be/internal/validate"
)

var ErrNotFound = errors.New("route not managed")

type Store struct {
	mu     sync.RWMutex
	path   string
	routes map[string]model.Route
}

func New(path string) (*Store, error) {
	s := &Store{path: path, routes: make(map[string]model.Route)}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	var routes []model.Route
	if err := json.Unmarshal(b, &routes); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	for _, r := range routes {
		host, err1 := validate.IPv4(r.Host)
		gateway, err2 := validate.IPv4(r.Gateway)
		if err1 != nil || err2 != nil || host == gateway {
			continue
		}
		r.Host, r.Gateway = host, gateway
		s.routes[r.Host] = r
	}
	return s, nil
}

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

func (s *Store) Get(host string) (model.Route, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.routes[host]
	return r, ok
}

func (s *Store) Upsert(host, gateway string) (model.Route, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.routes[host]
	route := model.Route{Host: host, Gateway: gateway}
	s.routes[host] = route
	if err := s.persistLocked(); err != nil {
		if exists {
			s.routes[host] = existing
		} else {
			delete(s.routes, host)
		}
		return model.Route{}, false, err
	}
	return route, !exists, nil
}

func (s *Store) Delete(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	route, ok := s.routes[host]
	if !ok {
		return ErrNotFound
	}
	delete(s.routes, host)
	if err := s.persistLocked(); err != nil {
		s.routes[host] = route
		return err
	}
	return nil
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	routes := make([]model.Route, 0, len(s.routes))
	for _, r := range s.routes {
		routes = append(routes, r)
	}
	sort.Slice(routes, func(i, j int) bool { return lessIP(routes[i].Host, routes[j].Host) })

	body, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".routes-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

func lessIP(a, b string) bool {
	ia, ib := net.ParseIP(a), net.ParseIP(b)
	if ia == nil || ib == nil {
		return a < b
	}
	aa, bb := ia.To4(), ib.To4()
	for i := 0; i < len(aa) && i < len(bb); i++ {
		if aa[i] != bb[i] {
			return aa[i] < bb[i]
		}
	}
	return false
}
