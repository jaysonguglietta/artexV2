package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func supervisorStatusFixture(uid uint32, parentPID int) string {
	return fmt.Sprintf("Name:\ttool-worker\nPPid:\t%d\nUid:\t%d\t%d\t%d\t%d\nCapInh:\t0000000000000000\nCapPrm:\t0000000000000000\nCapEff:\t0000000000000000\nCapBnd:\t0000000000000000\nCapAmb:\t0000000000000000\nNoNewPrivs:\t1\n", parentPID, uid, uid, uid, uid)
}

func TestSupervisorSecurityStatusRequiresCompleteKernelFields(t *testing.T) {
	valid := supervisorStatusFixture(0, 0)
	for _, uid := range []uint32{0, workerUID} {
		state, err := parseProcessSecurityStatus(supervisorStatusFixture(uid, 1))
		if err != nil {
			t.Fatal(err)
		}
		if state.parentPID != 1 || state.requireUnprivilegedUID(uid) != nil {
			t.Fatal("valid dropped-capability identity rejected")
		}
	}
	for _, field := range []string{"Uid", "PPid", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb", "NoNewPrivs"} {
		t.Run("missing "+field, func(t *testing.T) {
			var kept []string
			for _, line := range strings.Split(valid, "\n") {
				if !strings.HasPrefix(line, field+":") {
					kept = append(kept, line)
				}
			}
			if _, err := parseProcessSecurityStatus(strings.Join(kept, "\n")); err == nil {
				t.Fatal("missing security field accepted as zero")
			}
		})
	}
	for _, malformed := range []string{
		valid + "Uid:\t0\t0\t0\t0\n",
		valid + "CapEff:\t0000000000000000\n",
		strings.Replace(valid, "Uid:\t0\t0\t0\t0", "Uid:\t0\t0\t0", 1),
		strings.Replace(valid, "Uid:\t0\t0\t0\t0", "Uid:\t0\t0\t0\t4294967296", 1),
		strings.Replace(valid, "CapEff:\t0000000000000000", "CapEff:\tnot-hex", 1),
		strings.Replace(valid, "CapBnd:\t0000000000000000", "CapBnd:\t10000000000000000", 1),
		strings.Replace(valid, "NoNewPrivs:\t1", "NoNewPrivs:\t2", 1),
		strings.Replace(valid, "PPid:\t0", "PPid:\t-1", 1),
	} {
		if _, err := parseProcessSecurityStatus(malformed); err == nil {
			t.Fatal("malformed or duplicated security field accepted")
		}
	}
}

func TestSupervisorSecurityStatusRejectsEveryRemainingCapability(t *testing.T) {
	valid := supervisorStatusFixture(0, 0)
	for _, field := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
		t.Run(field, func(t *testing.T) {
			input := strings.Replace(valid, field+":\t0000000000000000", field+":\t0000000000001000", 1)
			state, err := parseProcessSecurityStatus(input)
			if err != nil {
				t.Fatal(err)
			}
			if state.requireUnprivilegedUID(0) == nil {
				t.Fatal("supervisor retained a capability")
			}
		})
	}
	for _, input := range []string{
		strings.Replace(valid, "Uid:\t0\t0\t0\t0", "Uid:\t0\t0\t10001\t0", 1),
		strings.Replace(valid, "NoNewPrivs:\t1", "NoNewPrivs:\t0", 1),
		supervisorStatusFixture(workerUID, 1),
	} {
		state, err := parseProcessSecurityStatus(input)
		if err != nil {
			t.Fatal(err)
		}
		if state.requireUnprivilegedUID(0) == nil {
			t.Fatal("supervisor identity or no_new_privs invariant bypassed")
		}
	}
	worker, err := parseProcessSecurityStatus(strings.Replace(supervisorStatusFixture(workerUID, 1), "Uid:\t10001\t10001\t10001\t10001", "Uid:\t10001\t10001\t0\t10001", 1))
	if err != nil {
		t.Fatal(err)
	}
	if worker.requireUnprivilegedUID(workerUID) == nil {
		t.Fatal("worker retained a saved root identity")
	}
}

func TestWorkerLeaseExpiresAndCannotBeRevived(t *testing.T) {
	start := time.Unix(1000, 0)
	lease := newWorkerLease(start)
	if lease.expired(start.Add(89*time.Second)) || !lease.expired(start.Add(90*time.Second)) {
		t.Fatal("initial lease did not enforce the 90-second boundary")
	}
	if !lease.renew(start.Add(20*time.Second)) || !lease.deadline.Equal(start.Add(110*time.Second)) {
		t.Fatal("broker heartbeat did not extend a live lease")
	}
	deadline := lease.deadline
	if lease.renew(deadline) || lease.renew(deadline.Add(time.Second)) || !lease.deadline.Equal(deadline) {
		t.Fatal("expired lease was revived by a delayed heartbeat")
	}
}

func TestWorkerLeaseRenewalRequiresExactWatchCommand(t *testing.T) {
	if err := validateWatchCommandLine([]byte(workerExecutable + "\x00watch\x0024\x00")); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{
		"/bin/sh\x00/usr/local/bin/worker-entrypoint\x00",
		workerExecutable + "\x00supervise\x00",
		workerExecutable + "\x00serve\x00",
		"/tmp/tool-worker\x00watch\x0024\x00",
		workerExecutable + "\x00watch\x000\x00",
		workerExecutable + "\x00watch\x00024\x00",
		workerExecutable + "\x00watch\x0024",
		workerExecutable + "\x00watch\x0024\x00extra\x00",
	} {
		if validateWatchCommandLine([]byte(command)) == nil {
			t.Fatalf("renewal accepted a bootstrap or ambiguous process: %q", command)
		}
	}
}

func TestSupervisorDispatchRejectsMalformedCommandsWithoutSpawning(t *testing.T) {
	for _, args := range [][]string{nil, {"serve"}, {"call"}, {"unknown"}} {
		if handled, err := dispatchSupervisor(args); handled || err != nil {
			t.Fatalf("ordinary worker mode incorrectly intercepted: %v, %v", args, err)
		}
	}
	for _, args := range [][]string{{"supervise", "extra"}, {"watch"}, {"watch", "0"}, {"watch", "1"}, {"watch", "-2"}, {"watch", "999999999999"}, {"watch", "not-a-pid"}, {"renew", "extra"}} {
		if handled, err := dispatchSupervisor(args); !handled || err == nil {
			t.Fatalf("malformed supervisor command accepted: %v", args)
		}
	}
}
