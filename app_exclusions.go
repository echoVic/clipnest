package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

const maxExcludedApps = 128

type ExcludedApp struct {
	BundleID string `json:"bundleID"`
	Name     string `json:"name"`
}

func (a ExcludedApp) validate() error {
	if len(a.BundleID) == 0 || len(a.BundleID) > 255 {
		return errors.New("应用 Bundle ID 不能为空或超过 255 字节")
	}
	for _, c := range a.BundleID {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return errors.New("应用 Bundle ID 包含无效字符")
		}
	}
	if strings.TrimSpace(a.Name) == "" || len(a.Name) > 256 || !utf8.ValidString(a.Name) {
		return errors.New("应用名称为空、无效或超过 256 字节")
	}
	return nil
}

type exclusionsConfig struct {
	Version int           `json:"version"`
	Apps    []ExcludedApp `json:"apps"`
}

func (c *exclusionsConfig) UnmarshalJSON(data []byte) error {
	// encoding/json otherwise replaces invalid encoded UTF-8 with U+FFFD.
	if !utf8.Valid(data) {
		return errors.New("排除应用配置包含无效 UTF-8")
	}
	type config exclusionsConfig
	var parsed config
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*c = exclusionsConfig(parsed)
	return nil
}

// Rules are low-frequency configuration, independent of history migration.
// Readers always get a copy; writes publish only at the atomic rename boundary.
type appExclusions struct {
	mu    sync.RWMutex
	path  string
	apps  []ExcludedApp
	write func(string, any) error
}

func openAppExclusions(path string) (*appExclusions, error) {
	a := &appExclusions{path: path, apps: []ExcludedApp{}, write: writePrivateJSON}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("排除应用配置路径不是普通文件")
	}
	config := exclusionsConfig{}
	if err := decodePrivateJSON(path, &config); err != nil {
		return nil, fmt.Errorf("排除应用配置无法读取：%w", err)
	}
	if config.Version != 1 || config.Apps == nil || len(config.Apps) > maxExcludedApps {
		return nil, errors.New("排除应用配置版本或列表无效")
	}
	seen := make(map[string]bool, len(config.Apps))
	for _, app := range config.Apps {
		if err := app.validate(); err != nil {
			return nil, err
		}
		if seen[app.BundleID] {
			return nil, errors.New("排除应用配置包含重复 Bundle ID")
		}
		seen[app.BundleID] = true
	}
	a.apps = config.Apps
	sortExcludedApps(a.apps)
	return a, nil
}

func sortExcludedApps(apps []ExcludedApp) {
	slices.SortFunc(apps, func(a, b ExcludedApp) int { return strings.Compare(a.BundleID, b.BundleID) })
}

func (a *appExclusions) List() []ExcludedApp {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return slices.Clone(a.apps)
}

func (a *appExclusions) Contains(bundleID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return slices.ContainsFunc(a.apps, func(app ExcludedApp) bool { return app.BundleID == bundleID })
}

func (a *appExclusions) Add(app ExcludedApp) error {
	if err := app.validate(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	next := slices.Clone(a.apps)
	i := slices.IndexFunc(next, func(old ExcludedApp) bool { return old.BundleID == app.BundleID })
	if i >= 0 {
		if next[i] == app {
			return nil
		}
		next[i] = app
	} else {
		if len(next) >= maxExcludedApps {
			return errors.New("最多排除 128 个应用")
		}
		next = append(next, app)
	}
	sortExcludedApps(next)
	return a.save(next)
}

func (a *appExclusions) Remove(bundleID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := slices.IndexFunc(a.apps, func(app ExcludedApp) bool { return app.BundleID == bundleID })
	if i < 0 {
		return nil
	}
	return a.save(slices.Delete(slices.Clone(a.apps), i, i+1))
}

func (a *appExclusions) save(next []ExcludedApp) error {
	err := a.write(a.path, exclusionsConfig{Version: 1, Apps: next})
	if err == nil || writeReplaced(err) {
		a.apps = next
	}
	return err
}
