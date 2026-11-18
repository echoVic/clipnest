//go:build !darwin && !windows

package main

const imageTextSupported = false

func (*Desktop) runImageText() {}

func imageTextNote() string { return "" }
