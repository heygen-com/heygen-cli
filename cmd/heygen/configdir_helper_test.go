package main

import "github.com/heygen-com/heygen-cli/internal/paths"

func mustConfigDir() string {
	dir, err := paths.ConfigDir()
	if err != nil {
		panic(err)
	}
	return dir
}

func mustUpdateCachePath() string {
	path, err := updateCachePath()
	if err != nil {
		panic(err)
	}
	return path
}

func mustTelemetryNoticePath() string {
	path, err := telemetryNoticePath()
	if err != nil {
		panic(err)
	}
	return path
}
