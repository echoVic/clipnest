package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The update signing key stays out of the repository: release builds get it
// from MYGO_UPDATER_PRIVATE_KEY.
func TestUpdateConfiguration(t *testing.T) {
	data, err := os.ReadFile("mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Slug  string `json:"slug"`
		MacOS struct {
			MinimumSystemVersion string         `json:"minimumSystemVersion"`
			InfoPlist            map[string]any `json:"infoPlist"`
		} `json:"macos"`
		Updates struct {
			GitHub     string  `json:"github"`
			PublicKey  string  `json:"publicKey"`
			PrivateKey *string `json:"privateKey"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Updates.GitHub != "echoVic/clipnest" {
		t.Errorf("updates.github = %q, want echoVic/clipnest", config.Updates.GitHub)
	}
	if strings.TrimSpace(config.Updates.PublicKey) == "" {
		t.Error("updates.publicKey must not be empty")
	}
	if config.Updates.PrivateKey != nil {
		t.Error("updates.privateKey must not be configured: release builds get the key from MYGO_UPDATER_PRIVATE_KEY")
	}
	if config.Slug != "clipnest" {
		t.Fatal("distribution filenames must keep the clipnest slug")
	}
	if config.MacOS.InfoPlist["LSUIElement"] != true {
		t.Fatal("packaged menu-bar app must stay out of Dock from startup")
	}
	if config.MacOS.MinimumSystemVersion != "13.0" {
		t.Fatal("bundle minimum must match Go 1.27's macOS 13 binary target")
	}
}
