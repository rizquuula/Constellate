package cli_test

import (
	"flag"
	"io"
	"testing"

	"github.com/rizquuula/Constellate/internal/platform/cli"
)

func TestConfigFlag(t *testing.T) {
	inputs := [][]string{
		{"--config", "x"},
		{"-config", "x"},
		{"-c", "x"},
	}
	for _, args := range inputs {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		p := cli.ConfigFlag(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatalf("ConfigFlag: parse %v: %v", args, err)
		}
		if *p != "x" {
			t.Errorf("ConfigFlag: args %v: got %q, want %q", args, *p, "x")
		}
	}
}

func TestStringAlias(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--log-level", "debug"}, "debug"},
		{[]string{"-l", "debug"}, "debug"},
		// Long and short share one target: last on the command line wins.
		{[]string{"-l", "info", "--log-level", "debug"}, "debug"},
		{[]string{"--log-level", "debug", "-l", "info"}, "info"},
	}
	for _, tc := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		p := cli.String(fs, "log-level", "l", "", "")
		if err := fs.Parse(tc.args); err != nil {
			t.Fatalf("String: parse %v: %v", tc.args, err)
		}
		if *p != tc.want {
			t.Errorf("String: args %v: got %q, want %q", tc.args, *p, tc.want)
		}
	}
}

func TestBoolAlias(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"--rootless"}, true},
		{[]string{"-r"}, true},
		{[]string{}, false},
	}
	for _, tc := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		p := cli.Bool(fs, "rootless", "r", false, "")
		if err := fs.Parse(tc.args); err != nil {
			t.Fatalf("Bool: parse %v: %v", tc.args, err)
		}
		if *p != tc.want {
			t.Errorf("Bool: args %v: got %v, want %v", tc.args, *p, tc.want)
		}
	}
}
