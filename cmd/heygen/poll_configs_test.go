package main

import (
	"testing"

	"github.com/heygen-com/heygen-cli/gen"
)

func TestPollConfigs_AllReferencedCommandsExist(t *testing.T) {
	for key := range pollConfigs {
		found := false
		for group, specs := range gen.Groups {
			for _, spec := range specs {
				if key == group+"/"+spec.Name {
					found = true
					break
				}
			}
			if found {
				break
			}
		}

		if !found {
			t.Errorf("poll config key %q does not match any generated spec", key)
		}
	}
}
