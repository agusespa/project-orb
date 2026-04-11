package main

import (
	"context"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"

	tea "github.com/charmbracelet/bubbletea"

	"project-orb/internal/paths"
)

type shutdownManagerStub struct {
	mu          sync.Mutex
	shutdowns   int
	shutdownErr error
}

func (s *shutdownManagerStub) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdowns++
	return s.shutdownErr
}

func (s *shutdownManagerStub) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdowns
}

func TestSetupSignalHandlerWithChannelShutsDownOnSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := &shutdownManagerStub{}
	sigChan := make(chan os.Signal, 1)
	cleanup := setupSignalHandlerWithChannel(ctx, cancel, manager.Shutdown, sigChan)
	defer cleanup()

	sigChan <- os.Interrupt

	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("context was not canceled after signal")
	}

	if manager.Count() != 1 {
		t.Fatalf("expected shutdown to be called once, got %d", manager.Count())
	}
}

func TestSetupSignalHandlerWithChannelStopsCleanlyWithoutSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := &shutdownManagerStub{}
	sigChan := make(chan os.Signal, 1)
	cleanup := setupSignalHandlerWithChannel(ctx, cancel, manager.Shutdown, sigChan)
	cleanup()

	time.Sleep(50 * time.Millisecond)

	if manager.Count() != 0 {
		t.Fatalf("expected no shutdown calls, got %d", manager.Count())
	}
}

func TestBubbleTeaProgramOptionsEnableAltScreenWithCellMotion(t *testing.T) {
	p := tea.NewProgram(nil, bubbleTeaProgramOptions(context.Background())...)

	startupOptions := readProgramStartupOptions(t, p)

	if startupOptions&(1<<0) == 0 {
		t.Fatal("expected alternate screen to be enabled")
	}
	if startupOptions&(1<<1) == 0 {
		t.Fatal("expected mouse cell motion to be enabled")
	}
	if startupOptions&(1<<2) != 0 {
		t.Fatal("expected mouse all motion to remain disabled")
	}
}

func TestGetLogPathUsesXDGStateHome(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmpDir)

	got, err := getLogPath()
	if err != nil {
		t.Fatalf("getLogPath() error = %v", err)
	}

	want, err := paths.DebugLogPath()
	if err != nil {
		t.Fatalf("DebugLogPath() error = %v", err)
	}
	if got != want {
		t.Fatalf("getLogPath() = %q, want %q", got, want)
	}
}

func readProgramStartupOptions(t *testing.T, p *tea.Program) int16 {
	t.Helper()

	value := reflect.ValueOf(p).Elem()
	field := value.FieldByName("startupOptions")
	if !field.IsValid() {
		t.Fatal("bubbletea.Program.startupOptions field not found")
	}

	return *(*int16)(unsafe.Pointer(field.UnsafeAddr()))
}
