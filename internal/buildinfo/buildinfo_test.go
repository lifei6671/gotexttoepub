package buildinfo

import "testing"

func TestDefaultBuildInfo(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "version", value: Version()},
		{name: "build time", value: BuildTime()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.value == "" {
				t.Fatalf("%s must not be empty", test.name)
			}
		})
	}
}
