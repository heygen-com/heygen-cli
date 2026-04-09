package main

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/heygen-com/heygen-cli/internal/config"
	"github.com/heygen-com/heygen-cli/internal/output"
)

func formatterForArgs(args []string, out, errOut io.Writer) output.Formatter {
	if wantsHumanOutput(args) {
		return output.NewHumanFormatter(out, errOut)
	}
	return output.NewJSONFormatter(out, errOut)
}

func wantsHumanOutput(args []string) bool {
	// 1. --human flag takes highest priority (both --human and --human=false)
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--human" {
			return true
		}
		if strings.HasPrefix(arg, "--human=") {
			value := strings.TrimPrefix(arg, "--human=")
			enabled, err := strconv.ParseBool(value)
			// Explicit --human=true/false overrides env and config
			if err == nil {
				return enabled
			}
		}
	}

	// 2. HEYGEN_OUTPUT env var
	if envVal := os.Getenv("HEYGEN_OUTPUT"); envVal != "" {
		return strings.EqualFold(envVal, "human")
	}

	// 3. config file (config set output human)
	fp := &config.FileProvider{}
	val, found, err := fp.Get(config.KeyOutput)
	if err == nil && found {
		return strings.EqualFold(val, "human")
	}

	return false
}
