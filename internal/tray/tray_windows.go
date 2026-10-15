//go:build windows

// Package tray provides the Windows notification-area frontend without a console.
package tray

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/kurotch-homelab/entra-token-agent/internal/agent"
	"golang.org/x/sys/windows"
)

var user = windows.NewLazySystemDLL("user32.dll")
var shell = windows.NewLazySystemDLL("shell32.dll")

// Keep Go pointers live until the synchronous Win32 call has returned.
func invoke(proc *windows.LazyProc, args ...any) uintptr {
	values := make([]uintptr, len(args))
	for i, arg := range args {
		switch v := arg.(type) {
		case uintptr:
			values[i] = v
		case int:
			values[i] = uintptr(v)
		case uint32:
			values[i] = uintptr(v)
		case *uint16:
			values[i] = uintptr(unsafe.Pointer(v))
		case unsafe.Pointer:
			values[i] = uintptr(v)
		default:
			panic("unsupported Win32 argument")
		}
	}
	r, _, _ := proc.Call(values...)
	runtime.KeepAlive(args)
	return r
}
func call(name string, args ...any) uintptr { return invoke(user.NewProc(name), args...) }
func str(s string) *uint16                  { p, _ := windows.UTF16PtrFromString(s); return p }
func ptr(s string) *uint16                  { return str(s) }

type point struct{ X, Y int32 }
type message struct {
	Hwnd           uintptr
	ID             uint32
	Wparam, Lparam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type notifyIcon struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	Title               [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

// Lock uses the legacy launcher's mutex name, so old and new launchers exclude each other.
// The caller must keep its OS thread locked until release (mutex ownership is thread-affine).
func Lock() (func(), error) {
	token := windows.GetCurrentProcessToken()
	u, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return lock("Local\\HomeLabEntraMetrics-" + u.User.Sid.String())
}

func lock(name string) (func(), error) {
	h, err := windows.CreateMutex(nil, false, str(name))
	if err != nil && err != windows.ERROR_ALREADY_EXISTS {
		return nil, err
	}
	result, err := windows.WaitForSingleObject(h, 0)
	if err != nil || (result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED) {
		windows.CloseHandle(h)
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	return func() { windows.ReleaseMutex(h); windows.CloseHandle(h) }, nil
}

type UI struct {
	hwnd, text, icon uintptr
	taskbar          uint32
	status           agent.Event
	shown            string
	logDir           string
	cancel           context.CancelFunc
	exiting          bool
	iconAdded        bool
	mu               sync.Mutex
	events           []agent.Event
}

func (u *UI) publish(e agent.Event) { u.mu.Lock(); u.events = append(u.events, e); u.mu.Unlock() }
func (u *UI) drain() {
	u.mu.Lock()
	events := u.events
	u.events = nil
	u.mu.Unlock()
	for _, e := range events {
		previous := u.status
		u.status = e
		if e.State == agent.SignIn {
			call("SetWindowTextW", u.text, ptr(e.Message))
			if e.Message != u.shown {
				u.shown = e.Message
				u.show()
			}
		} else {
			if previous.State == agent.SignIn {
				call("ShowWindow", u.hwnd, 0)
			}
			u.shown = ""
		}
		u.notify(1)
	}
	if u.status.State == agent.Ready && time.Now().After(u.status.Expires) {
		u.status.State = agent.Failed
		u.notify(1)
	}
	if u.status.State == agent.SignIn && !u.status.Expires.IsZero() && time.Now().After(u.status.Expires) {
		u.status = agent.Event{State: agent.Failed}
		u.shown = ""
		call("ShowWindow", u.hwnd, 0)
		u.notify(1)
	}
}
func (u *UI) show() {
	if u.status.State != agent.SignIn {
		return
	}
	call("ShowWindow", u.hwnd, 5)
	call("SetForegroundWindow", u.hwnd)
}
func (u *UI) notify(action uintptr) {
	if action == 0 && u.iconAdded {
		return
	}
	n := notifyIcon{Window: u.hwnd, ID: 1, Flags: 7, Callback: 0x8001, Icon: u.icon}
	n.Size = uint32(unsafe.Sizeof(n))
	tip, _ := windows.UTF16FromString("Entra token agent: " + string(u.status.State))
	copy(n.Tip[:], tip)
	result, _, _ := shell.NewProc("Shell_NotifyIconW").Call(action, uintptr(unsafe.Pointer(&n)))
	if action == 0 {
		u.iconAdded = result != 0
	}
	if action == 1 && result == 0 {
		u.iconAdded = false
	}
	if action == 2 {
		u.iconAdded = false
	}
}
func (u *UI) menu() {
	menu := call("CreatePopupMenu")
	defer call("DestroyMenu", menu)
	call("AppendMenuW", menu, 2, 0, ptr("Status: "+string(u.status.State)))
	flags := uintptr(0)
	if u.status.State != agent.SignIn {
		flags = 2
	}
	call("AppendMenuW", menu, flags, 1, ptr("Sign-in instructions"))
	call("AppendMenuW", menu, 0, 2, ptr("Open log folder"))
	call("AppendMenuW", menu, 0, 3, ptr("Exit token agent"))
	var p point
	call("GetCursorPos", unsafe.Pointer(&p))
	call("SetForegroundWindow", u.hwnd)
	selected := call("TrackPopupMenu", menu, 0x100|2, uintptr(p.X), uintptr(p.Y), 0, u.hwnd, 0)
	switch selected {
	case 1:
		u.show()
	case 2:
		invoke(shell.NewProc("ShellExecuteW"), u.hwnd, ptr("open"), ptr(u.logDir), 0, 0, 1)
	case 3:
		u.exiting = true
		u.cancel()
	}
	call("PostMessageW", u.hwnd, 0, 0, 0)
}

// Run owns the UI thread; work runs in a goroutine and publishes structured events.
func Run(logDir string, work func(context.Context, func(agent.Event)) error) error {
	return run(logDir, work, Lock)
}

func run(logDir string, work func(context.Context, func(agent.Event)) error, acquireLock func() (func(), error)) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	release, err := acquireLock()
	if err != nil {
		return err
	}
	if release == nil {
		return nil
	}
	defer release()
	pidFile := filepath.Join(logDir, "agent.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
		return err
	}
	defer os.Remove(pidFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u := &UI{logDir: logDir, cancel: cancel, status: agent.Event{State: agent.Pending}}
	done := make(chan struct{})
	u.taskbar = uint32(call("RegisterWindowMessageW", ptr("TaskbarCreated")))
	u.icon = call("LoadIconW", 0, 32512)
	proc := syscall.NewCallback(func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
		switch msg {
		case 0x10:
			call("ShowWindow", hwnd, 0)
			return 0
		case 0x11:
			u.exiting = true
			cancel()
			return 1
		case 0x16:
			if wp != 0 {
				u.exiting = true
				cancel()
			}
			return 0
		case 0x111:
			if wp&0xffff == 3 {
				u.exiting = true
				cancel()
			} else {
				call("ShowWindow", hwnd, 0)
			}
			return 0
		case 0x113:
			u.drain()
			if u.exiting {
				select {
				case <-done:
					call("DestroyWindow", hwnd)
				default:
				}
				return 0
			}
			// Retry icon registration if Explorer was not ready at startup.
			u.notify(0)
			return 0
		case 0x8001:
			if lp == 0x205 {
				u.menu()
			}
			if lp == 0x203 {
				u.show()
			}
			return 0
		case 2:
			u.notify(2)
			call("PostQuitMessage", 0)
			return 0
		}
		if msg == u.taskbar {
			u.iconAdded = false
			u.notify(0)
			return 0
		}
		return call("DefWindowProcW", hwnd, uintptr(msg), wp, lp)
	})
	name := str("EntraTokenAgentTray")
	wc := windowClass{Proc: proc, Name: name, Icon: u.icon, Background: 6, Cursor: call("LoadCursorW", 0, 32512)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if call("RegisterClassExW", unsafe.Pointer(&wc)) == 0 {
		return fmt.Errorf("register tray window class")
	}
	defer call("UnregisterClassW", unsafe.Pointer(name), 0)
	u.hwnd = call("CreateWindowExW", 0, unsafe.Pointer(name), ptr("HomeLab monitoring: sign-in required"), 0x00C80000, 200, 200, 640, 230, 0, 0, 0, 0)
	if u.hwnd == 0 {
		return fmt.Errorf("create tray window")
	}
	u.text = call("CreateWindowExW", 0, ptr("STATIC"), ptr(""), 0x50000000, 20, 20, 590, 110, u.hwnd, 0, 0, 0)
	call("CreateWindowExW", 0, ptr("BUTTON"), ptr("Close"), 0x50010000, 500, 140, 90, 30, u.hwnd, 10, 0, 0)
	u.notify(0)
	if call("SetTimer", u.hwnd, 1, 250, 0) == 0 {
		call("DestroyWindow", u.hwnd)
		return fmt.Errorf("create UI timer")
	}
	go func() {
		defer close(done)
		if err := work(ctx, u.publish); err != nil {
			slog.Error("agent stopped", "error", err)
		}
		u.publish(agent.Event{State: agent.Stopped})
	}()
	var m message
	for {
		r := call("GetMessageW", unsafe.Pointer(&m), 0, 0, 0)
		if int32(r) == -1 {
			cancel()
			<-done
			return fmt.Errorf("get window message")
		}
		if r == 0 {
			break
		}
		call("TranslateMessage", unsafe.Pointer(&m))
		call("DispatchMessageW", unsafe.Pointer(&m))
	}
	cancel()
	<-done
	return nil
}

// Alert reports startup failures even when a log file could not be opened.
func Alert(err error) { call("MessageBoxW", 0, ptr(err.Error()), ptr("Entra token agent"), 0x10) }
