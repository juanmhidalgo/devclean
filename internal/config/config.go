// Package config loads devclean's TOML configuration: built-in defaults
// overlaid by whatever keys the file sets. It never computes config paths;
// the caller passes the file path and the home directory.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the effective configuration.
//
// TOML representation:
//   - durations (scan_burst.window, observe_interval) are strings parsed by
//     time.ParseDuration, e.g. "5m", "24h";
//   - thresholds.<category> are integer days;
//   - clean_cadence is a calendar expression handed to the scheduler as-is
//     (default "weekly");
//   - artifact_types are appended to the built-in types; every other list
//     and table key replaces or (for maps) merges key by key into defaults.
type Config struct {
	Scan            Scan           `toml:"scan"`
	Thresholds      map[string]int `toml:"thresholds"`
	PressurePercent int            `toml:"pressure_percent"`
	ScanBurst       ScanBurst      `toml:"scan_burst"`
	Docker          Docker         `toml:"docker"`
	Watch           []string       `toml:"watch"`
	ArtifactTypes   []ArtifactType `toml:"artifact_types"`
	CachePaths      []CachePath    `toml:"cache_paths"`
	ObserveInterval Duration       `toml:"observe_interval"`
	CleanCadence    string         `toml:"clean_cadence"`
	NotifyCommand   string         `toml:"notify_command"`
}

// Scan configures the filesystem walk. SkipHidden is not configurable.
type Scan struct {
	Roots      []string `toml:"roots"`
	Excludes   []string `toml:"excludes"`
	SkipHidden bool     `toml:"-"`
}

// ScanBurst: MinArtifacts atime reads within Window count as a scan burst.
type ScanBurst struct {
	MinArtifacts int      `toml:"min_artifacts"`
	Window       Duration `toml:"window"`
}

// Docker holds the ephemeral-image rules ("key=value" label, name globs).
type Docker struct {
	EphemeralLabel string   `toml:"ephemeral_label"`
	EphemeralGlobs []string `toml:"ephemeral_globs"`
}

// ArtifactType identifies an artifact by directory Name and Marker file.
// MarkerLocation is "beside" (next to the artifact) or "inside" it.
type ArtifactType struct {
	Name           string `toml:"name"`
	Marker         string `toml:"marker"`
	MarkerLocation string `toml:"marker_location"`
}

// CachePath is a user-defined cache directory and its tier name.
type CachePath struct {
	Path string `toml:"path"`
	Tier string `toml:"tier"`
}

// Duration is a time.Duration that decodes from a TOML string like "5m".
type Duration time.Duration

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

const defaultThresholdDays = 90

// ThresholdDays returns the staleness threshold in days for a category.
func (c Config) ThresholdDays(category string) int {
	if d, ok := c.Thresholds[category]; ok {
		return d
	}
	return defaultThresholdDays
}

// Default returns the built-in configuration for the given home directory.
func Default(home string) Config {
	return Config{
		Scan: Scan{Roots: []string{home}, SkipHidden: true},
		Thresholds: map[string]int{
			"projects": defaultThresholdDays,
			"docker":   defaultThresholdDays,
		},
		PressurePercent: 85,
		ScanBurst:       ScanBurst{MinArtifacts: 5, Window: Duration(5 * time.Minute)},
		Docker:          Docker{EphemeralLabel: "devclean.ephemeral=true"},
		ArtifactTypes: []ArtifactType{
			{Name: "node_modules", Marker: "package.json", MarkerLocation: "beside"},
			{Name: ".venv", Marker: "pyvenv.cfg", MarkerLocation: "inside"},
			{Name: "venv", Marker: "pyvenv.cfg", MarkerLocation: "inside"},
			{Name: ".tox", Marker: "tox.ini", MarkerLocation: "beside"},
			{Name: ".tox", Marker: "pyproject.toml", MarkerLocation: "beside"},
			{Name: "target", Marker: "Cargo.toml", MarkerLocation: "beside"},
		},
		ObserveInterval: Duration(24 * time.Hour),
		CleanCadence:    "weekly",
	}
}

// Load returns Default(home) overlaid with the keys present in the file at
// path. A missing file is not an error and yields the defaults.
func Load(path, home string) (Config, error) {
	cfg := Default(home)
	extraTypes := cfg.ArtifactTypes
	cfg.ArtifactTypes = nil
	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		cfg.ArtifactTypes = extraTypes
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Config{}, fmt.Errorf("%s: unknown key %q", path, undecoded[0].String())
	}
	for i, a := range cfg.ArtifactTypes {
		if a.Marker == "" {
			return Config{}, fmt.Errorf("%s: artifact_types[%d] (%q): missing required key marker", path, i, a.Name)
		}
	}
	for i, c := range cfg.CachePaths {
		switch c.Tier {
		case "garbage", "caches", "manual":
		default:
			return Config{}, fmt.Errorf("%s: cache_paths[%d] (%q): invalid tier %q (want garbage, caches or manual)", path, i, c.Path, c.Tier)
		}
	}
	cfg.ArtifactTypes = append(extraTypes, cfg.ArtifactTypes...)
	cfg.Scan.Roots = ExpandPaths(cfg.Scan.Roots, home, home)
	cfg.Scan.Excludes = ExpandPaths(cfg.Scan.Excludes, home, home)
	return cfg, nil
}

// ExpandPath turns a path into the absolute, cleaned form the walker
// produces: a leading "~" becomes home and a relative path is joined to base.
// Excludes are compared to walked paths textually, so both sides must share
// this form. Paths from the config file use home as base, so they mean the
// same thing whatever directory devclean runs from (a scheduled run starts in
// home, a manual one anywhere); command-line paths use the working
// directory. Glob metacharacters pass through unchanged.
func ExpandPath(p, home, base string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

// ExpandPaths applies ExpandPath to every entry.
func ExpandPaths(ps []string, home, base string) []string {
	if ps == nil {
		return nil
	}
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = ExpandPath(p, home, base)
	}
	return out
}
