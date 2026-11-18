//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

const setprivExecutable = "/usr/bin/setpriv"

func dropCapabilityArgs() []string {
	return []string{"--bounding-set=-all", "--inh-caps=-all", "--ambient-caps=-all", "--no-new-privs"}
}

func processSecurityStatus(pid int) (processSecurityState, error) {
	status, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return processSecurityState{}, err
	}
	return parseProcessSecurityStatus(string(status))
}

// Supervisor modes are handled before the ordinary UID 10001 RPC invariant.
// They accept no executable, pathname, command or lease duration from a request.
func dispatchSupervisor(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "supervise":
		if len(args) != 1 {
			return true, errors.New("supervise accepts no arguments")
		}
		return true, superviseWorker()
	case "watch":
		if len(args) != 2 {
			return true, errors.New("watch requires a child process ID")
		}
		pid, err := strconv.ParseInt(args[1], 10, 31)
		if err != nil || pid <= 1 {
			return true, errors.New("invalid supervised child process ID")
		}
		return true, watchWorker(int(pid))
	case "renew":
		if len(args) != 1 {
			return true, errors.New("renew accepts no arguments")
		}
		return true, renewWorkerLease()
	default:
		return false, nil
	}
}

func requireRootPID1() error {
	if os.Getpid() != 1 || os.Getuid() != 0 || os.Geteuid() != 0 {
		return errors.New("worker supervisor must be root PID 1 inside its container")
	}
	return nil
}

func superviseWorker() error {
	if err := requireRootPID1(); err != nil {
		return err
	}
	args := []string{"--reuid=10001", "--regid=10001", "--clear-groups"}
	args = append(args, dropCapabilityArgs()...)
	args = append(args, workerExecutable, "serve")
	child := exec.Command(setprivExecutable, args...)
	child.Dir = "/workspace"
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		return fmt.Errorf("start unprivileged worker: %w", err)
	}
	// Start returns before setpriv finishes changing identity. Verify the child
	// before replacing PID 1; otherwise a normal startup race could fail closed.
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := processSecurityStatus(child.Process.Pid)
		if err == nil && state.parentPID == 1 && state.requireUnprivilegedUID(workerUID) == nil {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("worker child did not enter its unprivileged identity")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Root UID is retained solely so UID 10001 cannot signal or patch the lease.
	// Every capability, including NET_ADMIN, is permanently dropped before watch.
	// Ignore early heartbeats across exec until watch installs Notify. Do this
	// after spawning so the daemon and its tools retain their usual dispositions.
	signal.Ignore(syscall.SIGUSR1)
	if err := os.Chdir("/"); err != nil {
		return err
	}
	watchArgs := append([]string{setprivExecutable}, dropCapabilityArgs()...)
	watchArgs = append(watchArgs, workerExecutable, "watch", strconv.Itoa(child.Process.Pid))
	return syscall.Exec(setprivExecutable, watchArgs, os.Environ())
}

func watchWorker(childPID int) error {
	if err := requireRootPID1(); err != nil {
		return err
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	self, err := processSecurityStatus(1)
	if err != nil {
		return err
	}
	if err = self.requireUnprivilegedUID(0); err != nil {
		return fmt.Errorf("supervisor privilege drop failed: %w", err)
	}
	child, err := processSecurityStatus(childPID)
	if err != nil {
		return fmt.Errorf("inspect supervised child: %w", err)
	}
	if child.parentPID != 1 {
		return errors.New("supervised worker must be a direct child of PID 1")
	}
	if err = child.requireUnprivilegedUID(workerUID); err != nil {
		return fmt.Errorf("worker privilege drop failed: %w", err)
	}
	childExit := make(chan error, 1)
	go reapWorkerChildren(childPID, childExit)
	lease := newWorkerLease(time.Now())
	timer := time.NewTimer(time.Until(lease.deadline))
	defer timer.Stop()
	for {
		select {
		case err := <-childExit:
			return err
		case received := <-signals:
			if received != syscall.SIGUSR1 {
				return fmt.Errorf("worker supervisor received %s", received)
			}
			if !lease.renew(time.Now()) {
				return errors.New("worker lease expired before renewal")
			}
			timer.Reset(time.Until(lease.deadline))
		case <-timer.C:
			return errors.New("worker lease expired")
		}
	}
}

// Reap adopted double-fork descendants as well as the main daemon. On daemon
// exit or lease expiry, returning from PID 1 ends the namespace; Linux kills all
// remaining processes, including descendants that escaped process groups.
func reapWorkerChildren(childPID int, exited chan<- error) {
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, 0, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			exited <- fmt.Errorf("supervisor child wait failed: %w", err)
			return
		}
		if pid == childPID {
			if status.Exited() && status.ExitStatus() == 0 {
				exited <- nil
			} else {
				exited <- fmt.Errorf("supervised worker exited with wait status %d", status)
			}
			return
		}
	}
}

func renewWorkerLease() error {
	if os.Getpid() == 1 || os.Getuid() != 0 || os.Geteuid() != 0 {
		return errors.New("lease renewal requires a separate root process")
	}
	self, err := processSecurityStatus(os.Getpid())
	if err != nil {
		return err
	}
	if err = self.requireUnprivilegedUID(0); err != nil {
		return fmt.Errorf("lease renewal privilege drop failed: %w", err)
	}
	// Docker start returns before firewall bootstrap and the privilege-dropping
	// exec complete. Never signal that bootstrap process; wait for the exact watch
	// identity within the broker's bounded renewal deadline instead.
	deadline := time.Now().Add(4 * time.Second)
	for {
		supervisor, statusErr := processSecurityStatus(1)
		commandLine, commandErr := os.ReadFile("/proc/1/cmdline")
		if statusErr == nil && commandErr == nil && supervisor.requireUnprivilegedUID(0) == nil && validateWatchCommandLine(commandLine) == nil {
			return syscall.Kill(1, syscall.SIGUSR1)
		}
		if !time.Now().Before(deadline) {
			return errors.New("independent worker watchdog did not become ready before renewal deadline")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
