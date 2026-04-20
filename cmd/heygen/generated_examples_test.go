package main

import (
	"fmt"
	"testing"

	"github.com/heygen-com/heygen-cli/gen"
)

func TestAllGeneratedCommandsHaveExamples(t *testing.T) {
	for group, specs := range gen.Groups {
		for _, spec := range specs {
			t.Run(fmt.Sprintf("%s/%s", group, spec.Name), func(t *testing.T) {
				if len(spec.Examples) == 0 {
					t.Errorf("command %s %s (%s %s) has no examples — add to codegen/examples/%s.yaml",
						group, spec.Name, spec.Method, spec.Endpoint, group)
				}
			})
		}
	}
}
