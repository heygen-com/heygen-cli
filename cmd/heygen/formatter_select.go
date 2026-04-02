package main

import (
	"io"
	"strconv"
	"strings"

	"github.com/heygen-com/heygen-cli/internal/output"
)

func formatterForArgs(args []string, out, errOut io.Writer) output.Formatter {
	if wantsHumanOutput(args) {
		return output.NewHumanFormatter(out, errOut)
	}
	return output.NewJSONFormatter(out, errOut)
}

func wantsHumanOutput(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--human" {
			return true
		}
		if strings.HasPrefix(arg, "--human=") {
			value := strings.TrimPrefix(arg, "--human=")
			enabled, err := strconv.ParseBool(value)
			return err == nil && enabled
		}
	}
	return false
}
