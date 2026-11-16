package auth

import "github.com/heygen-com/heygen-cli/internal/paths"

func mustConfigDir() string {
	dir, err := paths.ConfigDir()
	if err != nil {
		panic(err)
	}
	return dir
}
