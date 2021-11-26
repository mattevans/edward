package instance

import (
	"encoding/json"
	"io/ioutil"
	"path"

	"github.com/pkg/errors"
	"github.com/mattevans/edward/services"
)

func LoadRunningServices(stateDir string) ([]services.ServiceOrGroup, error) {
	stateFiles, err := ioutil.ReadDir(stateDir)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	var services []services.ServiceOrGroup
	for _, file := range stateFiles {
		// Skip directories (these contain instance state)
		if file.IsDir() {
			continue
		}

		command := &Instance{}
		raw, err := ioutil.ReadFile(path.Join(stateDir, file.Name()))
		if err != nil {
			return nil, errors.WithStack(err)
		}
		json.Unmarshal(raw, command)
		command.Service.ConfigFile = command.ConfigFile
		services = append(services, command.Service)
	}
	return services, nil
}
