// Copyright 2017-2018 The Argo Authors
// Modifications Copyright 2024-2025 Jacob Colvin
// Licensed under the Apache License, Version 2.0

//nolint:testpackage
package pathutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_resolveSymlinkRecursive(t *testing.T) {
	t.Parallel()

	testsDir, err := filepath.Abs("./testdata")
	if err != nil {
		panic(err)
	}
	t.Run("Resolve non-symlink", func(t *testing.T) {
		t.Parallel()
		r, err := resolveSymbolicLinkRecursive(testsDir+"/foo", 2)
		require.NoError(t, err)
		assert.Equal(t, testsDir+"/foo", r)
	})
	t.Run("Successfully resolve symlink", func(t *testing.T) {
		t.Parallel()
		t.Skip("TODO: Fix this test")
		r, err := resolveSymbolicLinkRecursive(testsDir+"/bar", 2)
		require.NoError(t, err)
		assert.Equal(t, testsDir+"/foo", r)
	})
	t.Run("Do not allow symlink at all", func(t *testing.T) {
		t.Parallel()
		t.Skip("TODO: Fix this test")
		r, err := resolveSymbolicLinkRecursive(testsDir+"/bar", 0)
		require.Error(t, err)
		assert.Equal(t, "", r)
	})
	t.Run("Error because too nested symlink", func(t *testing.T) {
		t.Parallel()
		t.Skip("TODO: Fix this test")
		r, err := resolveSymbolicLinkRecursive(testsDir+"/bam", 2)
		require.Error(t, err)
		assert.Equal(t, "", r)
	})
	t.Run("No such file or directory", func(t *testing.T) {
		t.Parallel()
		r, err := resolveSymbolicLinkRecursive(testsDir+"/foobar", 2)
		require.NoError(t, err)
		assert.Equal(t, testsDir+"/foobar", r)
	})
}

func Test_isURLSchemeAllowed(t *testing.T) {
	t.Parallel()

	type testdata struct {
		name     string
		scheme   string
		allowed  []string
		expected bool
	}
	tts := []testdata{
		{
			name:     "Allowed scheme matches",
			scheme:   "http",
			allowed:  []string{"http", "https"},
			expected: true,
		},
		{
			name:     "Allowed scheme matches only partially",
			scheme:   "http",
			allowed:  []string{"https"},
			expected: false,
		},
		{
			name:     "Scheme is not allowed",
			scheme:   "file",
			allowed:  []string{"http", "https"},
			expected: false,
		},
		{
			name:     "Empty scheme with valid allowances is forbidden",
			scheme:   "",
			allowed:  []string{"http", "https"},
			expected: false,
		},
		{
			name:     "Empty scheme with empty allowances is forbidden",
			scheme:   "",
			allowed:  []string{},
			expected: false,
		},
		{
			name:     "Some scheme with empty allowances is forbidden",
			scheme:   "file",
			allowed:  []string{},
			expected: false,
		},
	}
	for _, tt := range tts {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := isURLSchemeAllowed(tt.scheme, tt.allowed)
			assert.Equal(t, tt.expected, r)
		})
	}
}

var allowedRemoteProtocols = []string{"http", "https"}

func Test_resolveFilePath(t *testing.T) {
	t.Parallel()

	t.Run("Resolve normal relative path into absolute path", func(t *testing.T) {
		t.Parallel()
		p, remote, err := ResolveValueFilePathOrURL("/foo/bar", "/foo", "baz/bim.yaml", allowedRemoteProtocols)
		require.NoError(t, err)
		assert.False(t, remote)
		assert.Equal(t, "/foo/bar/baz/bim.yaml", string(p))
	})
	t.Run("Resolve normal relative path into absolute path", func(t *testing.T) {
		t.Parallel()
		p, remote, err := ResolveValueFilePathOrURL("/foo/bar", "/foo", "baz/../../bim.yaml", allowedRemoteProtocols)
		require.NoError(t, err)
		assert.False(t, remote)
		assert.Equal(t, "/foo/bim.yaml", string(p))
	})
	t.Run("Error on path resolving outside repository root", func(t *testing.T) {
		t.Parallel()
		p, remote, err := ResolveValueFilePathOrURL("/foo/bar", "/foo", "baz/../../../bim.yaml", allowedRemoteProtocols)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "outside repository root")
		assert.False(t, remote)
		assert.Equal(t, "", string(p))
	})
	t.Run("Return verbatim URL", func(t *testing.T) {
		t.Parallel()
		url := "https://some.where/foo,yaml"
		p, remote, err := ResolveValueFilePathOrURL("/foo/bar", "/foo", url, allowedRemoteProtocols)
		require.NoError(t, err)
		assert.True(t, remote)
		assert.Equal(t, url, string(p))
	})
	t.Run("URL scheme not allowed", func(t *testing.T) {
		t.Parallel()
		url := "file:///some.where/foo,yaml"
		p, remote, err := ResolveValueFilePathOrURL("/foo/bar", "/foo", url, allowedRemoteProtocols)
		require.Error(t, err)
		assert.False(t, remote)
		assert.Equal(t, "", string(p))
	})
	t.Run("Implicit URL by absolute path", func(t *testing.T) {
		t.Parallel()
		p, remote, err := ResolveValueFilePathOrURL("/foo/bar", "/foo", "/baz.yaml", allowedRemoteProtocols)
		require.NoError(t, err)
		assert.False(t, remote)
		assert.Equal(t, "/foo/baz.yaml", string(p))
	})
	t.Run("Relative app path", func(t *testing.T) {
		t.Parallel()
		p, remote, err := ResolveValueFilePathOrURL(".", "/foo", "/baz.yaml", allowedRemoteProtocols)
		require.NoError(t, err)
		assert.False(t, remote)
		assert.Equal(t, "/foo/baz.yaml", string(p))
	})
	t.Run("Relative repo path", func(t *testing.T) {
		t.Parallel()
		c, err := os.Getwd()
		require.NoError(t, err)
		p, remote, err := ResolveValueFilePathOrURL(".", ".", "baz.yaml", allowedRemoteProtocols)
		require.NoError(t, err)
		assert.False(t, remote)
		assert.Equal(t, c+"/baz.yaml", string(p))
	})
	t.Run("Overlapping root prefix without trailing slash", func(t *testing.T) {
		t.Parallel()
		p, remote, err := ResolveValueFilePathOrURL(".", "/foo", "../foo2/baz.yaml", allowedRemoteProtocols)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "outside repository root")
		assert.False(t, remote)
		assert.Equal(t, "", string(p))
	})
	t.Run("Overlapping root prefix with trailing slash", func(t *testing.T) {
		t.Parallel()
		p, remote, err := ResolveValueFilePathOrURL(".", "/foo/", "../foo2/baz.yaml", allowedRemoteProtocols)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "outside repository root")
		assert.False(t, remote)
		assert.Equal(t, "", string(p))
	})
	t.Run("Garbage input as values file", func(t *testing.T) {
		t.Parallel()
		p, remote, err := ResolveValueFilePathOrURL(
			".", "/foo/", "kfdj\\ks&&&321209.,---e32908923%$§!\"", allowedRemoteProtocols)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "outside repository root")
		assert.False(t, remote)
		assert.Equal(t, "", string(p))
	})
	t.Run("NUL-byte path input as values file", func(t *testing.T) {
		t.Parallel()
		p, remote, err := ResolveValueFilePathOrURL(".", "/foo/", "\000", allowedRemoteProtocols)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "outside repository root")
		assert.False(t, remote)
		assert.Equal(t, "", string(p))
	})
	t.Run("Resolve root path into absolute path - jsonnet library path", func(t *testing.T) {
		t.Parallel()
		p, err := ResolveFileOrDirectoryPath("/foo", "/foo", "./")
		require.NoError(t, err)
		assert.Equal(t, "/foo", string(p))
	})
}
