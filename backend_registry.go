package main

import (
	"github.com/mattevans/edward/services"
	"github.com/mattevans/edward/services/backends/commandline"
	"github.com/mattevans/edward/services/backends/docker"
)

// RegisterBackends configures all supported service backends.
func RegisterBackends() {
	services.RegisterLegacyMarshaler(&commandline.LegacyUnmarshaler{})
	services.RegisterBackend(&commandline.Loader{})
	services.RegisterBackend(&docker.Loader{})
}
