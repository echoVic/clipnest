//go:build !darwin

package main

const imageTextSupported = false

func (*Desktop) runImageText() {}
