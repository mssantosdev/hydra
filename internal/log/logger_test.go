package log

import (
	"bytes"
	"io"
	"os"
	"testing"

	charmlog "github.com/charmbracelet/log"
)

func TestSetVerbose_EnablesDebugLevel(t *testing.T) {
	t.Cleanup(func() { SetVerbose(false) })

	SetVerbose(true)

	if !Verbose {
		t.Fatal("Verbose = false, want true")
	}
	if Logger.GetLevel() != charmlog.DebugLevel {
		t.Errorf("Logger level = %v, want %v", Logger.GetLevel(), charmlog.DebugLevel)
	}
}

func TestSetVerbose_DisablesDebugLevel(t *testing.T) {
	SetVerbose(true)
	t.Cleanup(func() { SetVerbose(false) })

	SetVerbose(false)

	if Verbose {
		t.Fatal("Verbose = true, want false")
	}
	if Logger.GetLevel() != charmlog.InfoLevel {
		t.Errorf("Logger level = %v, want %v", Logger.GetLevel(), charmlog.InfoLevel)
	}
}

// The level wrappers exist so hydra never writes diagnostics to stdout, which would corrupt a
// JSON envelope. That is the contract worth pinning: stdout stays clean.
func TestLevelWrappersNeverWriteToStdout(t *testing.T) {
	calls := map[string]func(){
		"Info":     func() { Info("info message") },
		"Warn":     func() { Warn("warn message") },
		"Error":    func() { Error("error message") },
		"Success":  func() { Success("success message") },
		"Print":    func() { Print("plain message") },
		"Header":   func() { Header("a header") },
		"Subtitle": func() { Subtitle("a subtitle") },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			saved := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("pipe: %v", err)
			}
			os.Stdout = w

			done := make(chan string, 1)
			go func() {
				var buf bytes.Buffer
				_, _ = io.Copy(&buf, r)
				done <- buf.String()
			}()

			call()

			_ = w.Close()
			os.Stdout = saved
			got := <-done
			_ = r.Close()

			if got != "" {
				t.Errorf("%s wrote %q to stdout; diagnostics belong on stderr or a JSON envelope would be corrupted", name, got)
			}
		})
	}
}
