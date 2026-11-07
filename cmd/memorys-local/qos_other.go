//go:build !windows

package main

func requestWebHighQoS() bool { return false }
