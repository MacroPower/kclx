package helm

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Masterminds/semver/v3"
)

type Entry struct {
	Version string
	Created time.Time
}

type Index struct {
	Entries map[string]Entries `yaml:"entries"`
}

func (i *Index) GetEntries(chart string) (Entries, error) {
	entries, ok := i.Entries[chart]
	if !ok {
		return nil, fmt.Errorf("chart '%s' not found in index", chart)
	}
	return entries, nil
}

type Entries []Entry

func (e Entries) MaxVersion(constraints *semver.Constraints) (*semver.Version, error) {
	versions := semver.Collection{}
	for _, entry := range e {
		v, err := semver.NewVersion(entry.Version)

		// Invalid semantic version ignored
		if errors.Is(err, semver.ErrInvalidSemVer) {
			slog.Debug("invalid semantic version", "tag", entry.Version)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("invalid constraint in index: %w", err)
		}
		if constraints.Check(v) {
			versions = append(versions, v)
		}
	}
	if len(versions) == 0 {
		return nil, errors.New("constraint not found in index")
	}
	maxVersion := versions[0]
	for _, v := range versions {
		if v.GreaterThan(maxVersion) {
			maxVersion = v
		}
	}
	return maxVersion, nil
}
