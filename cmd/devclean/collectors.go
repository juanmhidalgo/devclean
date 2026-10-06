package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/juanmhidalgo/devclean/internal/collect"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
	"github.com/juanmhidalgo/devclean/internal/platform"
)

// realCollectors returns the factory that builds the six production
// collectors from the platform and the run's config and history.
func realCollectors(plat platform.Platform, now func() time.Time) func(config.Config, history.History) []collect.Collector {
	return func(cfg config.Config, hist history.History) []collect.Collector {
		home, _ := os.UserHomeDir()
		pipenv := os.Getenv("WORKON_HOME")
		if pipenv == "" {
			pipenv = filepath.Join(plat.DataDir(), "virtualenvs")
		}
		return []collect.Collector{
			&collect.Projects{Config: cfg, Mount: plat.MountFor, History: hist, Now: now},
			&collect.Docker{Config: cfg, History: hist, Now: now},
			&collect.Venvs{PipenvDir: pipenv, PoetryDir: filepath.Join(plat.CacheDir(), "pypoetry", "virtualenvs")},
			&collect.Caches{Config: cfg, Mount: plat.MountFor, Home: home, CacheDir: plat.CacheDir(), DataDir: plat.DataDir()},
			&collect.System{Items: plat.SystemItems},
			&collect.Watch{Config: cfg, Home: home},
		}
	}
}
