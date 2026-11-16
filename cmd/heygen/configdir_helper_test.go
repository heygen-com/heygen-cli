package main

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
