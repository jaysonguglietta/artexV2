package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const workerUID uint32 = 10001
const workerLeaseDuration = 90 * time.Second
const workerExecutable = "/usr/local/bin/tool-worker"

type processSecurityState struct {
	uids       [4]uint32
	parentPID  int
	capability [5]uint64 // Inheritable, permitted, effective, bounding, ambient.
	noNewPrivs bool
}

// Parse kernel-generated status fields strictly. A missing or malformed field
// must never turn a failed privilege drop into a zero-valued success.
func parseProcessSecurityStatus(status string) (processSecurityState, error) {
	var state processSecurityState
	seen := map[string]bool{}
	capIndex := map[string]int{"CapInh": 0, "CapPrm": 1, "CapEff": 2, "CapBnd": 3, "CapAmb": 4}
	for _, line := range strings.Split(status, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		index, isCap := capIndex[key]
		if key != "Uid" && key != "PPid" && key != "NoNewPrivs" && !isCap {
			continue
		}
		if seen[key] {
			return state, fmt.Errorf("duplicate process status field %s", key)
		}
		seen[key] = true
		fields := strings.Fields(value)
		if key == "Uid" {
			if len(fields) != len(state.uids) {
				return state, errors.New("invalid process Uid field")
			}
			for i, field := range fields {
				uid, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return state, errors.New("invalid process Uid value")
				}
				state.uids[i] = uint32(uid)
			}
			continue
		}
		if len(fields) != 1 {
			return state, fmt.Errorf("invalid process status field %s", key)
		}
		switch {
		case isCap:
			capability, err := strconv.ParseUint(fields[0], 16, 64)
			if err != nil {
				return state, fmt.Errorf("invalid process capability field %s", key)
			}
			state.capability[index] = capability
		case key == "PPid":
			pid, err := strconv.ParseUint(fields[0], 10, 31)
			if err != nil {
				return state, errors.New("invalid process PPid field")
			}
			state.parentPID = int(pid)
		case key == "NoNewPrivs":
			if fields[0] != "0" && fields[0] != "1" {
				return state, errors.New("invalid process NoNewPrivs field")
			}
			state.noNewPrivs = fields[0] == "1"
		}
	}
	for _, key := range []string{"Uid", "PPid", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb", "NoNewPrivs"} {
		if !seen[key] {
			return state, fmt.Errorf("missing process status field %s", key)
		}
	}
	return state, nil
}

func (s processSecurityState) requireUnprivilegedUID(uid uint32) error {
	for _, actual := range s.uids {
		if actual != uid {
			return fmt.Errorf("all real, effective, saved and filesystem UIDs must be %d", uid)
		}
	}
	for _, capability := range s.capability {
		if capability != 0 {
			return errors.New("all capability sets must be empty")
		}
	}
	if !s.noNewPrivs {
		return errors.New("no_new_privs must be enabled")
	}
	return nil
}

func validateWatchCommandLine(commandLine []byte) error {
	args := strings.Split(string(commandLine), "\x00")
	if len(args) != 4 || args[0] != workerExecutable || args[1] != "watch" || args[3] != "" {
		return errors.New("PID 1 is not the independent worker watchdog")
	}
	pid, err := strconv.ParseInt(args[2], 10, 31)
	if err != nil || pid <= 1 || strconv.FormatInt(pid, 10) != args[2] {
		return errors.New("invalid watchdog child process identity")
	}
	return nil
}

type workerLease struct{ deadline time.Time }

func newWorkerLease(now time.Time) workerLease {
	return workerLease{deadline: now.Add(workerLeaseDuration)}
}

func (l workerLease) expired(now time.Time) bool { return !now.Before(l.deadline) }

// A delayed heartbeat cannot revive a lease that already expired.
func (l *workerLease) renew(now time.Time) bool {
	if l.expired(now) {
		return false
	}
	l.deadline = now.Add(workerLeaseDuration)
	return true
}
