package main

import (
	_ "embed"
	"encoding/json"

	"github.com/egoist/mygo"
)

// The packaged version takes precedence; go run uses the same release config.
//
//go:embed mygo.json
var releaseConfig []byte

func applicationVersion() string {
	if version := mygo.App.Version(); version != "" {
		return version
	}
	var config struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(releaseConfig, &config) == nil && config.Version != "" {
		return config.Version
	}
	return "开发版"
}
