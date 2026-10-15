//go:build windows

package tray

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/kurotch-homelab/entra-token-agent/internal/agent"
)

func TestMutexExcludesAnotherThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name := fmt.Sprintf("Local\\EntraTrayTest-%d", os.Getpid())
	release, err := lock(name)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan bool)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		r, e := lock(name)
		if r != nil {
			r()
		}
		done <- e == nil && r == nil
	}()
	if !<-done {
		t.Fatal("second owner acquired mutex")
	}
}

func TestNotificationLifecycle(t *testing.T) {
	u := &UI{}
	e := agent.Event{State: agent.SignIn, Message: "test code", Expires: time.Now().Add(time.Minute)}
	u.publish(e)
	u.drain()
	if u.shown != e.Message {
		t.Fatal("missing notification")
	}
	u.publish(e)
	u.drain()
	if u.shown != e.Message {
		t.Fatal("duplicate changed notification")
	}
	u.publish(agent.Event{State: agent.Ready, Expires: time.Now().Add(time.Hour)})
	u.drain()
	if u.shown != "" || u.status.Message != "" {
		t.Fatal("stale sign-in instructions")
	}
	e.Expires = time.Now().Add(-time.Second)
	u.publish(e)
	u.drain()
	if u.status.State != agent.Failed || u.shown != "" {
		t.Fatal("expired code still active")
	}
}

func TestNativeWindowLifecycle(t *testing.T) {
	dir := t.TempDir()
	checked := make(chan error, 1)
	err := run(dir, func(ctx context.Context, publish func(agent.Event)) error {
		var hwnd uintptr
		callback := syscall.NewCallback(func(h, _ uintptr) uintptr {
			var pid uint32
			call("GetWindowThreadProcessId", h, unsafe.Pointer(&pid))
			if pid == uint32(os.Getpid()) {
				hwnd = h
				return 0
			}
			return 1
		})
		call("EnumWindows", callback, 0)
		if hwnd == 0 {
			checked <- fmt.Errorf("window missing")
			return fmt.Errorf("window missing")
		}
		var checkErr error
		if call("IsWindowVisible", hwnd) != 0 {
			checkErr = fmt.Errorf("initial window visible")
		}
		event := agent.Event{State: agent.SignIn, Message: "TEST ONLY: no sign-in required", Expires: time.Now().Add(time.Minute)}
		publish(event)
		time.Sleep(400 * time.Millisecond)
		if call("IsWindowVisible", hwnd) == 0 {
			checkErr = fmt.Errorf("sign-in notification not visible")
		}
		// Close hides the notification but leaves the agent running.
		call("SendMessageW", hwnd, 0x10, 0, 0)
		publish(event)
		time.Sleep(400 * time.Millisecond)
		if call("IsWindowVisible", hwnd) != 0 {
			checkErr = fmt.Errorf("duplicate notification reopened")
		}
		publish(agent.Event{State: agent.Ready, Expires: time.Now().Add(time.Hour)})
		time.Sleep(400 * time.Millisecond)
		if call("IsWindowVisible", hwnd) != 0 {
			checkErr = fmt.Errorf("completed notification visible")
		}
		event.Message = "TEST ONLY: expiring code"
		event.Expires = time.Now().Add(300 * time.Millisecond)
		publish(event)
		time.Sleep(800 * time.Millisecond)
		if call("IsWindowVisible", hwnd) != 0 {
			checkErr = fmt.Errorf("expired notification visible")
		}
		checked <- checkErr
		call("PostMessageW", hwnd, 0x111, 3, 0)
		<-ctx.Done()
		return nil
	}, func() (func(), error) { return lock(fmt.Sprintf("Local\\EntraTrayUITest-%d", os.Getpid())) })
	if err != nil {
		t.Fatal(err)
	}
	if err := <-checked; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.pid")); !os.IsNotExist(err) {
		t.Fatal("PID not removed")
	}
}
