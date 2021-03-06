// Copyright 2020 cloudeng llc. All rights reserved.
// Use of this source code is governed by the Apache-2.0
// license that can be found in the LICENSE file.

package cloudpath_test

import (
	"testing"

	"cloudeng.io/path/cloudpath"
)

func TestUnix(t *testing.T) {
	data := []matcherTestSpec{
		{
			"/",
			cloudpath.UnixFileSystem, "localhost", "", "/", '/', nil,
		},
		{
			"./",
			cloudpath.UnixFileSystem, "localhost", "", "./", '/', nil,
		},
		{
			".",
			cloudpath.UnixFileSystem, "localhost", "", ".", '/', nil,
		},
		{
			"..",
			cloudpath.UnixFileSystem, "localhost", "", "..", '/', nil,
		},
		{
			"/a/b",
			cloudpath.UnixFileSystem, "localhost", "", "/a/b", '/', nil,
		},
		{
			"file:///a/b/c/",
			cloudpath.UnixFileSystem, "localhost", "", "/a/b/c/", '/', nil,
		},
		{
			"file://ignored/a/b/c/",
			cloudpath.UnixFileSystem, "localhost", "", "/a/b/c/", '/', nil,
		},
	}
	if err := testMatcher(cloudpath.UnixMatcher, data); err != nil {
		t.Errorf("%v", err)
	}
	if err := testNoMatch(cloudpath.UnixMatcher, []string{
		"",
	}); err != nil {
		t.Errorf("%v", err)
	}

	for _, d := range data {
		if got, want := cloudpath.IsLocal(d.input), true; got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}
