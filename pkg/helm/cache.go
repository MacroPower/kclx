package helm

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Rather than storing a mapping of key->path in memory, uses very simple
// bijective encoding/decoding functions to convert keys to paths. This allows
// cache preservation across multiple KCL run invocations.
type TempPaths struct {
	root string
	lock sync.RWMutex
}

func NewTempPaths(root string) *TempPaths {
	chartPaths := filepath.Join(root, "charts")
	err := os.MkdirAll(chartPaths, 0o700)
	if err != nil {
		panic(err)
	}
	return &TempPaths{
		root: filepath.Join(root, "charts"),
	}
}

func (p *TempPaths) keyToPath(key string) string {
	return filepath.Join(p.root, encodeKey(key))
}

func (p *TempPaths) pathToKey(path string) string {
	key, err := decodeKey(filepath.Base(path))
	if err != nil {
		panic(fmt.Errorf("failed to decode key for %s: %w", path, err))
	}
	return key
}

func (p *TempPaths) Add(_ string, _ string) {
}

// GetPath generates a path for the given key or returns previously generated one.
func (p *TempPaths) GetPath(key string) (string, error) {
	return p.keyToPath(key), nil
}

// GetPathIfExists gets a path for the given key if it exists. Otherwise, returns an empty string.
func (p *TempPaths) GetPathIfExists(key string) string {
	p.lock.RLock()
	defer p.lock.RUnlock()

	if _, err := os.Stat(p.keyToPath(key)); err == nil {
		return p.keyToPath(key)
	}
	return ""
}

// GetPaths gets a copy of the map of paths.
func (p *TempPaths) GetPaths() map[string]string {
	p.lock.RLock()
	defer p.lock.RUnlock()

	ds, err := os.ReadDir(p.root)
	if err != nil {
		panic(err)
	}

	paths := map[string]string{}
	for _, d := range ds {
		path := filepath.Join(p.root, d.Name())
		paths[p.pathToKey(path)] = path
	}
	return paths
}

func encodeKey(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

func decodeKey(s string) (string, error) {
	d, err := base64.URLEncoding.DecodeString(s)
	return string(d), err
}
