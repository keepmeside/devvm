package cli

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/devvmhq/devvm/internal/msg"
	"github.com/devvmhq/devvm/internal/settings"
	"github.com/devvmhq/devvm/internal/ui"
)

// stubbedRun is devvm's command tree with nothing run: each command's
// RunE only records that it ran (returning fail), and the root's
// PersistentPreRunE is gone.
type stubbedRun struct {
	root   *cobra.Command
	u      *ui.UI
	errOut *bytes.Buffer
	ran    []string
}

func stubbed(fail error) *stubbedRun {
	u, _, errOut := testUI()
	s := &stubbedRun{root: NewRoot(u), u: u, errOut: errOut}
	s.root.PersistentPreRunE = nil
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.RunE != nil || c.Run != nil {
			c.Run, c.RunE = nil, func(cmd *cobra.Command, _ []string) error {
				s.ran = append(s.ran, cmd.CommandPath())
				if fail != nil {
					return ranError{fail} // as checkCommands's wrapper of RunE does
				}
				return nil
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(s.root)
	return s
}

// run runs the stubbed tree with args, returning the exit code.
func (s *stubbedRun) run(t *testing.T, args ...string) int {
	t.Helper()
	return execute(t.Context(), s.u, s.root, args)
}

// commandPaths lists every command under root that parses its own flags,
// by its path below the root (nil for the root).
func commandPaths(root *cobra.Command) [][]string {
	var out [][]string
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		if c.DisableFlagParsing {
			return
		}
		out = append(out, path)
		for _, sub := range c.Commands() {
			walk(sub, append(slices.Clone(path), sub.Name()))
		}
	}
	walk(root, nil)
	return out
}

// tries are the command lines that must be usage errors for the command
// c at path: an unknown flag; an argument when its help shows none; none
// when it shows one it needs; three when it shows one at most; any flag
// that takes a value given a blank one, as a special value is a word.
func tries(path []string, c *cobra.Command) [][]string {
	use := argsOf(c)
	with := func(extra ...string) []string { return append(slices.Clone(path), extra...) }
	out := [][]string{with("--no-such-flag")}
	if use == "" {
		out = append(out, with("extra"))
	}
	if use != "" && !strings.HasPrefix(use, "[") {
		out = append(out, with())
	}
	if use != "" && !strings.Contains(use, "...") && !strings.Contains(use, " ") {
		out = append(out, with("a", "b", "c"))
	}
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Value.Type() == "string" {
			out = append(out, with("--"+f.Name+"="))
		}
	})
	return out
}

// Every command, run with an unknown flag, without an argument it needs,
// with too many, or with a flag's value blank, fails with a devvm
// message, exit 2, then the line naming the command's own --help; it
// never runs, and the parser's own English never shows.
func TestUsageErrorsEveryCommand(t *testing.T) {
	paths := commandPaths(stubbed(nil).root)
	if len(paths) < 40 {
		t.Fatalf("only %d commands found", len(paths))
	}
	for _, path := range paths {
		c, _, err := stubbed(nil).root.Find(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, args := range tries(path, c) {
			s := stubbed(nil)
			code := s.run(t, args...)
			got := s.errOut.String()
			if code != usageCode || len(s.ran) > 0 {
				t.Errorf("devvm %q: exit %d, ran %q, want %d and nothing run; stderr %q", args, code, s.ran, usageCode, got)
				continue
			}
			lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
			want := "usage_help command=" + c.CommandPath()
			if len(lines) < 2 || lines[len(lines)-1] != want || strings.Contains(got, "usage_error") {
				t.Errorf("devvm %q printed %q, want a devvm message, then %q", args, got, want)
			}
		}
	}
}

// sampleArgs are command lines the arguments in use stand for: none when
// all are optional, and each given, twice for one that repeats.
func sampleArgs(t *testing.T, use string) [][]string {
	t.Helper()
	// a value each argument a Use names takes
	argSamples := map[string]string{
		"IMAGE": "node", "KEY": "0123456789ab", "TOOL": "node", "LAYER": "docker", "NAME": "github", "RULE": "example.com",
		"LIST": "5173", "TARGET": "db", "PORT": "5432", "PORTS": "5432,8000-8010", "KEY_FILE": "/k", "FILE": "/f.tar.gz", "KEY=VALUE": settings.RetainRunLogs + "=10", "SHELL": "bash",
		"VM": "devvm-app-0123456789abcd", "AGENT": "codex", "ARGV": "true", "ID": "s1",
	}
	var all []string
	optional := true
	for word := range strings.FieldsSeq(use) {
		optional = optional && strings.HasPrefix(word, "[")
		many := strings.HasSuffix(strings.TrimSuffix(word, "]"), "...")
		name := strings.Trim(word, "[].")
		name, _, _ = strings.Cut(name, "|")
		v := name
		if strings.ToUpper(name) == name {
			if v = argSamples[name]; v == "" {
				t.Fatalf("argSamples has no %s", name)
			}
		}
		all = append(all, v)
		if many {
			all = append(all, v)
		}
	}
	out := [][]string{all}
	if optional && len(all) > 0 {
		out = append(out, nil)
	}
	return out
}

// Every command takes the arguments its Use shows: each, a repeated one
// twice, and none when they're optional.
func TestUseMatchesArgs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, path := range commandPaths(stubbed(nil).root) {
		c, _, err := stubbed(nil).root.Find(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, args := range sampleArgs(t, argsOf(c)) {
			s := stubbed(nil)
			line := slices.Concat(path, args)
			if code := s.run(t, line...); code != 0 || !slices.Equal(s.ran, []string{c.CommandPath()}) {
				t.Errorf("devvm %q: exit %d, ran %q, stderr %q; want it run, as its Use is %q", line, code, s.ran, s.errOut, c.Use)
			}
		}
	}
}

// What a command returns once it ran is a failure, exit 1, printed
// without the help line: a message as it is, anything else in
// output_failed.
func TestRanErrorIsAFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{msg.NoVM("/x"), "devvm: no_vm dir=/x\n"},
		{errors.New("broken pipe"), "devvm: output_failed reason=broken pipe\n"},
	} {
		s := stubbed(tc.err)
		if code := s.run(t, "stop"); code != 1 || s.errOut.String() != tc.want {
			t.Errorf("a stop failing with %v: exit %d, stderr %q; want 1 and %q", tc.err, code, s.errOut, tc.want)
		}
	}
}

func TestArgsOf(t *testing.T) {
	t.Parallel()
	for use, want := range map[string]string{
		"stop":                             "",
		"add KEY_FILE":                     "KEY_FILE",
		"ports [all|none|LIST]":            "[all|none|LIST]",
		"rm IMAGE|KEY...":                  "IMAGE|KEY...",
		"gc [--tools [--force] [TOOL...]]": "",
		"x [--force] NAME [KEY=VALUE...]":  "NAME [KEY=VALUE...]",
		"y --as NAME TARGET":               "NAME TARGET",
	} {
		if got := argsOf(&cobra.Command{Use: use}); got != want {
			t.Errorf("argsOf(%q) = %q, want %q", use, got, want)
		}
	}
}

// Every flag error the parser can give becomes a devvm message, by the
// flag as given.
func TestFlagErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"stop", "--nope"}, "flag_unknown command=devvm stop flag=--nope"},
		{[]string{"stop", "-q"}, "flag_unknown command=devvm stop flag=-q"},
		{[]string{"network", "log", "-n"}, "flag_needs_value flag=-n form=-n N"},
		{[]string{"network", "log", "--lines"}, "flag_needs_value flag=--lines form=--lines N"},
		{[]string{"network", "log", "--lines", "x"}, "flag_bad_value flag=--lines form=N value=x"},
		{[]string{"--clone=maybe"}, "flag_bad_switch flag=--clone value=maybe"},
		{[]string{"network", "log", "--blocked", "--follow"}, "flags_exclusive flag=--blocked other=--follow"},
		{[]string{"build", "--backend", "lima", "base"}, "flag_unknown command=devvm build flag=--backend"},
		{[]string{"--backend", "lima"}, "flag_unknown command=devvm flag=--backend"},
		{[]string{"ls", "--cpus", "4"}, "flag_unknown command=devvm ls flag=--cpus"},
		{[]string{"--ports", ""}, "flag_blank flag=ports"},
		{[]string{"gc", "--history", "--older-than", "30"}, "bad_duration value=30"},
		{[]string{"--cpus", "x"}, `cpus_not_number flag=--cpus value="x"`},
		{[]string{"--ports", "x"}, `port_invalid value="x"`},
	} {
		s := stubbed(nil)
		if code := s.run(t, tc.args...); code != usageCode || !strings.HasPrefix(s.errOut.String(), "devvm: "+tc.want+"\n") {
			t.Errorf("devvm %q: exit %d, stderr %q; want %d and %q", tc.args, code, s.errOut, usageCode, tc.want)
		}
	}
}
