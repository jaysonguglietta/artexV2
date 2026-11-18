//go:build !linux

package main

import "errors"

func dispatchSupervisor(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "supervise", "watch", "renew":
		return true, errors.New("isolated worker supervision requires a Linux container")
	default:
		return false, nil
	}
}
