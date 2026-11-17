package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// What capture does with a text copy that holds a recognized secret.
const (
	secretsMask = "mask" // record it with the secret masked
	secretsSkip = "skip" // do not record it
	secretsOff  = "off"  // record it as is
)

const (
	maxIgnoreRules     = 64
	maxIgnoreRuleBytes = 512
)

type PrivacySettings struct {
	Secrets   string   `json:"secrets"`
	ClearSync bool     `json:"clearSync"` // forget the last capture when the clipboard is cleared
	Ignore    []string `json:"ignore"`    // RE2 patterns; a matching text copy is not recorded
}

func defaultPrivacy() PrivacySettings {
	return PrivacySettings{Secrets: secretsMask, ClearSync: true, Ignore: []string{}}
}

func (s PrivacySettings) clone() PrivacySettings {
	s.Ignore = slices.Clone(s.Ignore)
	if s.Ignore == nil {
		s.Ignore = []string{}
	}
	return s
}

// compile validates the settings and returns the compiled ignore rules.
func (s PrivacySettings) compile() ([]*regexp.Regexp, error) {
	if s.Secrets != secretsMask && s.Secrets != secretsSkip && s.Secrets != secretsOff {
		return nil, errors.New("密钥识别设置无效")
	}
	if len(s.Ignore) > maxIgnoreRules {
		return nil, fmt.Errorf("最多 %d 条忽略规则", maxIgnoreRules)
	}
	rules := make([]*regexp.Regexp, 0, len(s.Ignore))
	for i, rule := range s.Ignore {
		switch {
		case strings.TrimSpace(rule) == "":
			return nil, errors.New("忽略规则不能为空")
		case len(rule) > maxIgnoreRuleBytes:
			return nil, fmt.Errorf("忽略规则不能超过 %d 字节", maxIgnoreRuleBytes)
		case !utf8.ValidString(rule):
			return nil, errors.New("忽略规则包含无效字符")
		case slices.Contains(s.Ignore[:i], rule):
			return nil, fmt.Errorf("忽略规则「%s」重复", rule)
		}
		re, err := regexp.Compile(rule)
		if err != nil {
			return nil, fmt.Errorf("忽略规则「%s」不是有效的正则表达式", rule)
		}
		if re.MatchString("") {
			return nil, fmt.Errorf("忽略规则「%s」会匹配所有内容", rule)
		}
		rules = append(rules, re)
	}
	return rules, nil
}

type privacyConfig struct {
	Version   int      `json:"version"`
	Secrets   string   `json:"secrets"`
	ClearSync bool     `json:"clearSync"`
	Ignore    []string `json:"ignore"`
}

func (c *privacyConfig) UnmarshalJSON(data []byte) error {
	// encoding/json otherwise replaces invalid encoded UTF-8 with U+FFFD.
	if !utf8.Valid(data) {
		return errors.New("隐私设置包含无效 UTF-8")
	}
	type config privacyConfig
	var parsed config
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*c = privacyConfig(parsed)
	return nil
}

// Settings are low-frequency configuration kept beside excluded-apps.json.
// A missing file means the defaults; a broken one stops startup rather than
// silently dropping the user's ignore rules.
type privacyStore struct {
	mu       sync.RWMutex
	path     string
	settings PrivacySettings
	rules    []*regexp.Regexp
	write    func(string, any) error
}

func openPrivacy(path string) (*privacyStore, error) {
	p := &privacyStore{path: path, settings: defaultPrivacy(), write: writePrivateJSON}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("隐私设置路径不是普通文件")
	}
	var config privacyConfig
	if err := decodePrivateJSON(path, &config); err != nil {
		return nil, fmt.Errorf("隐私设置无法读取：%w", err)
	}
	if config.Version != 1 || config.Ignore == nil {
		return nil, errors.New("隐私设置版本或忽略列表无效")
	}
	settings := PrivacySettings{Secrets: config.Secrets, ClearSync: config.ClearSync, Ignore: config.Ignore}
	rules, err := settings.compile()
	if err != nil {
		return nil, fmt.Errorf("隐私设置无效：%w", err)
	}
	p.settings, p.rules = settings, rules
	return p, nil
}

func (p *privacyStore) Settings() PrivacySettings {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.settings.clone()
}

func (p *privacyStore) Save(next PrivacySettings) error {
	next = next.clone()
	rules, err := next.compile()
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	err = p.write(p.path, privacyConfig{Version: 1, Secrets: next.Secrets, ClearSync: next.ClearSync, Ignore: next.Ignore})
	if err == nil || writeReplaced(err) {
		p.settings, p.rules = next, rules
	}
	return err
}

// Ignored reports whether a text copy matches an ignore rule.
func (p *privacyStore) Ignored(text string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return slices.ContainsFunc(p.rules, func(re *regexp.Regexp) bool { return re.MatchString(text) })
}
