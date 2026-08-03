package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mssantosdev/hydra/internal/output"
)

// resetCommandState clears flag values and the resolved project between tests.
// The cobra tree and the project globals are package level, so without this one
// test's --branches or --project silently leaks into the next.
func resetCommandState(t *testing.T) {
	t.Helper()

	cfg, projectRoot, projectConfigPath = nil, "", ""
	cfgFile, projectFlag, outputFlag = "", "", ""
	verboseFlag, noHooksFlag = false, false
	outMode = output.ModeText
	rootCmd.SetArgs(nil)

	var reset func(c *cobra.Command)
	reset = func(c *cobra.Command) {
		clear := func(f *pflag.Flag) {
			f.Changed = false
			if slice, ok := f.Value.(pflag.SliceValue); ok {
				_ = slice.Replace(nil)
				return
			}
			_ = f.Value.Set(f.DefValue)
		}
		c.Flags().VisitAll(clear)
		c.PersistentFlags().VisitAll(clear)
		for _, child := range c.Commands() {
			reset(child)
		}
	}
	reset(rootCmd)
}

func withJSONOutput(t *testing.T) func() {
	t.Helper()
	old := outputFlag
	outputFlag = "json"
	return func() {
		outputFlag = old
	}
}

func decodeJSONData(t *testing.T, stdout *bytes.Buffer, dest any) {
	t.Helper()

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, stdout.String())
	}
	if err := json.Unmarshal(envelope.Data, dest); err != nil {
		t.Fatalf("decode data: %v", err)
	}
}
