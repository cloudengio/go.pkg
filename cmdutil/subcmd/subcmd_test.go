// Copyright 2020 cloudeng llc. All rights reserved.
// Use of this source code is governed by the Apache-2.0
// license that can be found in the LICENSE file.

package subcmd_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"cloudeng.io/cmdutil/subcmd"
	"cloudeng.io/errors"
)

func ExampleCommandSet() {
	ctx := context.Background()
	type rangeFlags struct {
		From int `subcmd:"from,1,start value for a range"`
		To   int `subcmd:"to,2,end value for a range "`
	}
	printRange := func(ctx context.Context, values interface{}, args []string) error {
		r := values.(*rangeFlags)
		fmt.Printf("%v..%v\n", r.From, r.To)
		return nil
	}

	fs := subcmd.NewFlagSet()
	fs.MustRegisterFlagStruct(&rangeFlags{}, nil, nil)
	// Subcommands are added using the subcmd.WithSubcommands option.
	cmd := subcmd.NewCommand("ranger", fs, printRange, subcmd.WithoutArguments())
	cmd.Document("print an integer range")
	cmdSet := subcmd.NewCommandSet(cmd)

	// Use cmdSet.Dispatch to access os.Args.
	fmt.Println(cmdSet.Usage("example-command"))
	cmdSet.DispatchWithArgs(ctx, "example-command", "ranger")
	cmdSet.DispatchWithArgs(ctx, "example-command", "ranger", "--from=10", "--to=100")

	// Output:
	// Usage of example-command
	//
	// Usage of command ranger: print an integer range
	// ranger [--from=1 --to=2]
	//   -from int
	//     	start value for a range (default 1)
	//   -to int
	//     	end value for a range  (default 2)
	//
	// 1..2
	// 10..100
}

type flagsA struct {
	A int `subcmd:"flag-a,,a: an int flag"`
	B int `subcmd:"flag-b,,b: an int flag"`
}

type flagsB struct {
	X string `subcmd:"flag-x,,x: a string flag"`
	Y string `subcmd:"flag-y,,y: a string flag"`
}

func TestCommandSet(t *testing.T) {
	ctx := context.Background()
	var err error
	assertNoError := func() {
		_, _, line, _ := runtime.Caller(1)
		if err != nil {
			t.Fatalf("line %v: %v", line, err)
		}
	}

	out := &strings.Builder{}
	runnerA := func(ctx context.Context, values interface{}, args []string) error {
		fl, ok := values.(*flagsA)
		if !ok {
			t.Fatalf("wrong type: %T", values)
		}
		fmt.Fprintf(out, "%v .. %v\n", fl.A, fl.B)
		return nil
	}
	runnerB := func(ctx context.Context, values interface{}, args []string) error {
		fl, ok := values.(*flagsB)
		if !ok {
			t.Fatalf("wrong type: %T", values)
		}
		fmt.Fprintf(out, "%v .. %v\n", fl.X, fl.Y)
		return nil
	}

	cmdAFlags := subcmd.NewFlagSet()
	err = cmdAFlags.RegisterFlagStruct(&flagsA{}, nil, nil)
	assertNoError()
	cmdA := subcmd.NewCommand("cmd-a", cmdAFlags, runnerA)
	cmdA.Document("subcmd a", "<args>...")

	cmdBFlags := subcmd.NewFlagSet()
	err = cmdBFlags.RegisterFlagStruct(&flagsB{}, nil, nil)
	assertNoError()
	cmdB := subcmd.NewCommand("cmd-b", cmdBFlags, runnerB)
	cmdB.Document("subcmd b", "")
	commands := subcmd.NewCommandSet(cmdA, cmdB)

	err = commands.DispatchWithArgs(ctx, "test", "cmd-a", "--flag-a=1", "--flag-b=3")
	assertNoError()
	if got, want := out.String(), "1 .. 3\n"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	if got, want := commands.Usage("binary"), `Usage of binary

Usage of command cmd-a: subcmd a
cmd-a [--flag-a=0 --flag-b=0] <args>...
  -flag-a int
    	a: an int flag
  -flag-b int
    	b: an int flag

Usage of command cmd-b: subcmd b
cmd-b [--flag-x= --flag-y=]
  -flag-x string
    	x: a string flag
  -flag-y string
    	y: a string flag
`; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	out.Reset()
	err = commands.DispatchWithArgs(ctx, "test", "cmd-b", "--flag-x=s1", "--flag-y=s3")
	assertNoError()
	if got, want := out.String(), "s1 .. s3\n"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

}

func TestCommandOptions(t *testing.T) {
	ctx := context.Background()

	numArgs := -1
	runnerA := func(ctx context.Context, values interface{}, args []string) error {
		if _, ok := values.(*flagsA); !ok {
			t.Fatalf("wrong type: %T", values)
		}
		numArgs = len(args)
		return nil
	}
	cmdAFlags := subcmd.NewFlagSet()
	err := cmdAFlags.RegisterFlagStruct(&flagsA{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	cmdset := subcmd.NewCommandSet(
		subcmd.NewCommand("exactly-two", cmdAFlags, runnerA, subcmd.ExactlyNumArguments(2)),
		subcmd.NewCommand("none", cmdAFlags, runnerA, subcmd.WithoutArguments()),
		subcmd.NewCommand("at-most-one", cmdAFlags, runnerA, subcmd.OptionalSingleArgument()),
	)

	expectedError := func(errmsg string) {
		_, _, line, _ := runtime.Caller(1)
		if err == nil || !strings.Contains(err.Error(), errmsg) {
			t.Errorf("line %v: missing or incorrect error: %v does not contain %v", line, err, errmsg)
		}
	}
	expectedNArgs := func(n int) {
		_, _, line, _ := runtime.Caller(1)
		if err != nil {
			t.Fatalf("line %v: unexpected error: %v", line, err)
		}
		if got, want := numArgs, n; got != want {
			t.Errorf("line %v: got %v, want %v", line, got, want)
		}
		numArgs = -1
	}
	err = cmdset.DispatchWithArgs(ctx, "test", "exactly-two")
	expectedError("exactly-two: accepts exactly 2 arguments")
	err = cmdset.DispatchWithArgs(ctx, "test", "exactly-two", "a", "b")
	expectedNArgs(2)
	err = cmdset.DispatchWithArgs(ctx, "test", "none", "aaa")
	expectedError("none: does not accept any arguments")
	err = cmdset.DispatchWithArgs(ctx, "test", "none")
	expectedNArgs(0)
	err = cmdset.DispatchWithArgs(ctx, "test", "at-most-one", "a", "b")
	expectedError("at-most-one: accepts at most one argument")
	err = cmdset.DispatchWithArgs(ctx, "test", "at-most-one")
	expectedNArgs(0)
	err = cmdset.DispatchWithArgs(ctx, "test", "at-most-one", "a")
	expectedNArgs(1)
}

func TestMultiLevel(t *testing.T) {
	ctx := context.Background()

	cmd1, cmd2, cmd11, cmd12 := false, false, false, false

	c1 := func(ctx context.Context, values interface{}, args []string) error {
		cmd1 = true
		return nil
	}

	c2 := func(ctx context.Context, values interface{}, args []string) error {
		cmd2 = true
		return nil
	}

	c11 := func(ctx context.Context, values interface{}, args []string) error {
		cmd11 = true
		return nil
	}

	c12 := func(ctx context.Context, values interface{}, args []string) error {
		cmd12 = true
		return nil
	}

	c1Flags := subcmd.NewFlagSet()
	c2Flags := subcmd.NewFlagSet()
	c11Flags := subcmd.NewFlagSet()
	c12Flags := subcmd.NewFlagSet()
	errs := errors.M{}
	errs.Append(c1Flags.RegisterFlagStruct(&flagsA{}, nil, nil))
	errs.Append(c2Flags.RegisterFlagStruct(&flagsA{}, nil, nil))
	errs.Append(c11Flags.RegisterFlagStruct(&flagsA{}, nil, nil))
	errs.Append(c12Flags.RegisterFlagStruct(&flagsA{}, nil, nil))
	if err := errs.Err(); err != nil {
		t.Fatal(err)
	}

	c11Cmd := subcmd.NewCommand("c11", c11Flags, c11)
	c11Cmd.Document("c11")
	c12Cmd := subcmd.NewCommand("c12", c12Flags, c12)
	c12Cmd.Document("c12")
	l2 := subcmd.NewCommandSet(c11Cmd, c12Cmd)
	c1Cmd := subcmd.NewCommand("c1", c1Flags, c1, subcmd.WithSubCommands(l2))
	c1Cmd.Document("c1")
	c2Cmd := subcmd.NewCommand("c2", c2Flags, c2)
	c2Cmd.Document("c2")

	l1 := subcmd.NewCommandSet(c1Cmd, c2Cmd)

	if got, want := l1.Usage("test"), `Usage of test

Usage of command c1: c1
c1 [--flag-a=0 --flag-b=0] c11|c12 ...
  -flag-a int
    	a: an int flag
  -flag-b int
    	b: an int flag

Usage of command c2: c2
c2 [--flag-a=0 --flag-b=0]
  -flag-a int
    	a: an int flag
  -flag-b int
    	b: an int flag
`; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	var err error

	assert := func(b bool) {
		_, _, line, _ := runtime.Caller(1)
		if err != nil {
			t.Fatalf("line %v: unexpected error: %v", line, err)
		}
		if !b {
			t.Errorf("line %v: expected value to be true", line)
		}
	}

	err = l1.DispatchWithArgs(ctx, "test", "c1", "c11")
	assert(cmd11)
	assert(!cmd12)
	assert(!cmd1)
	assert(!cmd2)

	err = l1.DispatchWithArgs(ctx, "test", "c1", "c12")
	assert(cmd11)
	assert(cmd12)
	assert(!cmd1)
	assert(!cmd2)

	err = l1.DispatchWithArgs(ctx, "test", "c2")
	assert(cmd11)
	assert(cmd12)
	assert(!cmd1)
	assert(cmd2)

	err = l1.DispatchWithArgs(ctx, "test", "c1")
	if err == nil || !strings.Contains(err.Error(), "missing top level command: available commands are: c11, c12") {
		t.Errorf("expected a particular error: %v", err)
	}

	err = l1.DispatchWithArgs(ctx, "test", "c1", "cx")
	if err == nil || !strings.Contains(err.Error(), "cx is not one of the supported commands: c11, c12") {
		t.Errorf("expected a particular error: %v", err)
	}
}