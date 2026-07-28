package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/heygen-com/heygen-cli/gen"
)

// pollOnlyFlags are registered by the builder only when poll_configs.go has an
// entry for the command. An example using one on any other command is shipped
// guidance that fails with "unknown flag" the moment an agent copies it.
var pollOnlyFlags = []string{"--wait", "--timeout"}

func TestGeneratedExamplesDoNotUsePollFlagsWithoutPollConfig(t *testing.T) {
	for group, specs := range gen.Groups {
		for _, spec := range specs {
			key := group + "/" + spec.Name
			if pollConfigs[key] != nil {
				continue
			}
			t.Run(key, func(t *testing.T) {
				for _, ex := range spec.Examples {
					for _, flag := range pollOnlyFlags {
						if strings.Contains(ex, flag) {
							t.Errorf("%s has no poll config, so %s is not a registered flag, "+
								"but an example uses it — fix codegen/examples/%s.yaml and regenerate:\n%s",
								key, flag, group, ex)
						}
					}
				}
			})
		}
	}
}

// `-d` accepts stdin ("-"), inline JSON, or a bare file path. It has no curl-style
// "@file" form, so `-d @foo.json` is read as a literal filename and fails.
func TestGeneratedExamplesDoNotUseAtFileSyntax(t *testing.T) {
	for group, specs := range gen.Groups {
		for _, spec := range specs {
			t.Run(fmt.Sprintf("%s/%s", group, spec.Name), func(t *testing.T) {
				for _, ex := range spec.Examples {
					if strings.Contains(ex, "-d @") || strings.Contains(ex, "--data @") {
						t.Errorf("%s %s example uses curl-style @file, which readData does not support; "+
							"drop the '@' in codegen/examples/%s.yaml and regenerate:\n%s",
							group, spec.Name, group, ex)
					}
				}
			})
		}
	}
}
