package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	version "github.com/hashicorp/go-version"
	"github.com/pkg/errors"
	"github.com/mattevans/edward/services"
)

// Config defines the structure for the Edward project configuration file
type Config struct {
	workingDir string

	TelemetryScript  string                   `json:"telemetryScript,omitempty"`
	MinEdwardVersion string                   `json:"edwardVersion,omitempty"`
	Imports          []string                 `json:"imports,omitempty"`
	ImportedGroups   []GroupDef               `json:"-"`
	ImportedServices []services.ServiceConfig `json:"-"`
	Env              []string                 `json:"env,omitempty"`
	Groups           []GroupDef               `json:"groups,omitempty"`
	Services         []services.ServiceConfig `json:"services"`

	ServiceMap map[string]*services.ServiceConfig      `json:"-"`
	GroupMap   map[string]*services.ServiceGroupConfig `json:"-"`

	FilePath string `json:"-"`
}

// GroupDef defines a group based on a list of children specified by name
type GroupDef struct {
	Name        string   `json:"name"`
	Aliases     []string `json:"aliases,omitempty"`
	Description string   `json:"description,omitempty"`
	Children    []string `json:"children"`
	Env         []string `json:"env,omitempty"`
}

// LoadConfig loads configuration from an io.Reader with the working directory explicitly specified
func LoadConfig(filePath string, edwardVersion string) (Config, error) {
	reader, err := os.Open(filePath)
	if err != nil {
		return Config{}, errors.WithStack(err)
	}
	workingDir := path.Dir(filePath)
	config, err := loadConfigContents(reader, workingDir)
	config.FilePath = filePath
	if err != nil {
		return Config{}, errors.WithStack(err)
	}
	if config.MinEdwardVersion != "" && edwardVersion != "" {
		// Check that this config is supported by this version
		minVersion, err1 := version.NewVersion(config.MinEdwardVersion)
		if err1 != nil {
			return Config{}, errors.WithStack(err)
		}
		currentVersion, err2 := version.NewVersion(edwardVersion)
		if err2 != nil {
			return Config{}, errors.WithStack(err)
		}
		if currentVersion.LessThan(minVersion) {
			return Config{}, errors.New("this config requires at least version " + config.MinEdwardVersion)
		}
	}
	err = config.initMaps()

	log.Printf("Config loaded with: %d groups and %d services\n", len(config.GroupMap), len(config.ServiceMap))
	return config, errors.WithStack(err)
}

// Reader from os.Open
func loadConfigContents(reader io.Reader, workingDir string) (Config, error) {
	log.Printf("Loading config with working dir %v.\n", workingDir)

	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(reader)
	if err != nil {
		return Config{}, errors.Wrap(err, "could not read config")
	}

	data := buf.Bytes()
	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		if syntax, ok := err.(*json.SyntaxError); ok && syntax.Offset != 0 {
			start := strings.LastIndex(string(data[:syntax.Offset]), "\n") + 1
			line, pos := strings.Count(string(data[:start]), "\n")+1, int(syntax.Offset)-start-1
			return Config{}, errors.Wrapf(err, "could not parse config file (line %v, char %v)", line, pos)
		}
		return Config{}, errors.Wrap(err, "could not parse config file")
	}

	config.workingDir = workingDir

	err = config.loadImports()
	if err != nil {
		return Config{}, errors.WithStack(err)
	}

	return config, nil
}

// Save saves config to an io.Writer
func (c Config) Save(writer io.Writer) error {
	log.Printf("Saving config")
	content, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		return errors.WithStack(err)
	}
	_, err = writer.Write(content)
	return errors.WithStack(err)
}

// NewConfig creates a Config from slices of services and groups
func NewConfig(newServices []services.ServiceConfig, newGroups []services.ServiceGroupConfig) Config {
	log.Printf("Creating new config with %d services and %d groups.\n", len(newServices), len(newGroups))

	// Find Env settings common to all services
	var allEnvSlices [][]string
	for _, s := range newServices {
		allEnvSlices = append(allEnvSlices, s.Env)
	}
	env := stringSliceIntersect(allEnvSlices)

	// Remove common settings from services
	var svcs []services.ServiceConfig
	for _, s := range newServices {
		s.Env = stringSliceRemoveCommon(env, s.Env)
		svcs = append(svcs, s)
	}

	cfg := Config{
		Env:      env,
		Services: svcs,
		Groups:   []GroupDef{},
	}

	cfg.AddGroups(newGroups)

	log.Printf("Config created: %v", cfg)

	return cfg
}

// EmptyConfig creates a Config with no services or groups
func EmptyConfig(workingDir string) Config {
	log.Printf("Creating empty config\n")

	cfg := Config{
		workingDir: workingDir,
	}

	cfg.ServiceMap = make(map[string]*services.ServiceConfig)
	cfg.GroupMap = make(map[string]*services.ServiceGroupConfig)

	return cfg
}

// NormalizeServicePaths will modify the Paths for each of the provided services
// to be relative to the working directory of this config file
func (c *Config) NormalizeServicePaths(searchPath string, newServices []*services.ServiceConfig) ([]*services.ServiceConfig, error) {
	log.Printf("Normalizing paths for %d services.\n", len(newServices))
	var outServices []*services.ServiceConfig
	for _, s := range newServices {
		curService := *s
		fullPath := filepath.Join(searchPath, *curService.Path)
		relPath, err := filepath.Rel(c.workingDir, fullPath)
		if err != nil {
			return outServices, errors.WithStack(err)
		}
		curService.Path = &relPath
		outServices = append(outServices, &curService)
	}
	return outServices, nil
}

// AppendServices adds services to an existing config without replacing existing services
func (c *Config) AppendServices(newServices []*services.ServiceConfig) error {
	log.Printf("Appending %d services.\n", len(newServices))
	if c.ServiceMap == nil {
		c.ServiceMap = make(map[string]*services.ServiceConfig)
	}
	for _, s := range newServices {
		if _, found := c.ServiceMap[s.Name]; !found {
			c.ServiceMap[s.Name] = s
			c.Services = append(c.Services, *s)
		}
	}
	return nil
}

// AppendGroups adds groups to an existing config without replacing existing groups
func (c *Config) AppendGroups(groups []*services.ServiceGroupConfig) error {
	var groupsDereferenced []services.ServiceGroupConfig
	for _, group := range groups {
		groupsDereferenced = append(groupsDereferenced, *group)
	}
	return errors.WithStack(c.AddGroups(groupsDereferenced))
}

func (c *Config) RemoveGroup(name string) error {
	if _, ok := c.GroupMap[name]; !ok {
		return errors.New("Group not found")
	}
	delete(c.GroupMap, name)

	existingGroupDefs := c.Groups
	c.Groups = make([]GroupDef, 0, len(existingGroupDefs))
	for _, group := range existingGroupDefs {
		if group.Name != name {
			c.Groups = append(c.Groups, group)
		}
	}
	return nil
}

// AddGroups adds a slice of groups to the Config
func (c *Config) AddGroups(groups []services.ServiceGroupConfig) error {
	log.Printf("Adding %d groups.\n", len(groups))
	for _, group := range groups {
		grp := GroupDef{
			Name:        group.Name,
			Aliases:     group.Aliases,
			Description: group.Description,
			Children:    []string{},
			Env:         group.Env,
		}
		for _, cg := range group.Groups {
			if cg != nil {
				grp.Children = append(grp.Children, cg.Name)
			}
		}
		for _, cs := range group.Services {
			if cs != nil {
				grp.Children = append(grp.Children, cs.Name)
			}
		}
		c.Groups = append(c.Groups, grp)
	}
	return nil
}

func (c *Config) loadImports() error {
	log.Printf("Loading imports\n")
	for _, i := range c.Imports {
		var cPath string
		if filepath.IsAbs(i) {
			cPath = i
		} else {
			cPath = filepath.Join(c.workingDir, i)
		}

		log.Printf("Loading: %v\n", cPath)

		r, err := os.Open(cPath)
		if err != nil {
			return errors.WithStack(err)
		}
		cfg, err := loadConfigContents(r, filepath.Dir(cPath))
		if err != nil {
			return errors.WithMessage(err, i)
		}

		err = c.importConfig(cfg)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

func (c *Config) importConfig(second Config) error {
	for _, service := range append(second.Services, second.ImportedServices...) {
		c.ImportedServices = append(c.ImportedServices, service)
	}
	for _, group := range append(second.Groups, second.ImportedGroups...) {
		c.ImportedGroups = append(c.ImportedGroups, group)
	}
	return nil
}

func (c *Config) combinePath(path string) *string {
	if filepath.IsAbs(path) || strings.HasPrefix(path, "$") {
		return &path
	}
	fullPath := filepath.Join(c.workingDir, path)
	return &fullPath
}

func addToMap(m map[string]struct{}, values ...string) {
	for _, v := range values {
		m[v] = struct{}{}
	}
}

func intersect(m map[string]struct{}, values ...string) []string {
	var out []string
	for _, v := range values {
		if _, ok := m[v]; ok {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func (c *Config) initMaps() error {
	var err error
	var svcs = make(map[string]*services.ServiceConfig)
	var servicesSkipped = make(map[string]struct{})

	var namesInUse = make(map[string]struct{})

	for _, s := range append(c.Services, c.ImportedServices...) {
		sc := s
		sc.Env = append(sc.Env, c.Env...)
		sc.ConfigFile, err = filepath.Abs(c.FilePath)
		if err != nil {
			return errors.WithStack(err)
		}
		if sc.MatchesPlatform() {
			if i := intersect(namesInUse, append(sc.Aliases, sc.Name)...); len(i) > 0 {
				return fmt.Errorf("Duplicate name or alias: %v", strings.Join(i, ", "))
			}
			svcs[sc.Name] = &sc
			addToMap(namesInUse, append(sc.Aliases, sc.Name)...)
		} else {
			servicesSkipped[sc.Name] = struct{}{}
		}
	}

	var groups = make(map[string]*services.ServiceGroupConfig)
	// First pass: Services
	var orphanNames = make(map[string]struct{})
	for _, g := range append(c.Groups, c.ImportedGroups...) {
		var childServices []*services.ServiceConfig

		for _, name := range g.Children {
			if s, ok := svcs[name]; ok {
				if s.Path != nil {
					s.Path = c.combinePath(*s.Path)
				}
				childServices = append(childServices, s)
			} else if _, skipped := servicesSkipped[name]; !skipped {
				orphanNames[name] = struct{}{}
			}
		}

		if i := intersect(namesInUse, append(g.Aliases, g.Name)...); len(i) > 0 {
			return fmt.Errorf("Duplicate name or alias: %v", strings.Join(i, ", "))
		}

		groups[g.Name] = &services.ServiceGroupConfig{
			Name:        g.Name,
			Aliases:     g.Aliases,
			Description: g.Description,
			Services:    childServices,
			Groups:      []*services.ServiceGroupConfig{},
			Env:         g.Env,
			ChildOrder:  g.Children,
		}
		addToMap(namesInUse, append(g.Aliases, g.Name)...)
	}

	// Second pass: Groups
	for _, g := range append(c.Groups, c.ImportedGroups...) {
		childGroups := []*services.ServiceGroupConfig{}

		for _, name := range g.Children {
			if gr, ok := groups[name]; ok {
				delete(orphanNames, name)
				childGroups = append(childGroups, gr)
			}
			if hasChildCycle(groups[g.Name], childGroups) {
				return errors.New("group cycle: " + g.Name)
			}
		}
		groups[g.Name].Groups = childGroups
	}

	if len(orphanNames) > 0 {
		var keys []string
		for k := range orphanNames {
			keys = append(keys, k)
		}
		return errors.New("A service or group could not be found for the following names: " + strings.Join(keys, ", "))
	}

	c.ServiceMap = svcs
	c.GroupMap = groups
	return nil
}

func hasChildCycle(parent *services.ServiceGroupConfig, children []*services.ServiceGroupConfig) bool {
	for _, sg := range children {
		if parent == sg {
			return true
		}
		if hasChildCycle(parent, sg.Groups) {
			return true
		}
	}
	return false
}

func stringSliceIntersect(slices [][]string) []string {
	var counts = make(map[string]int)
	for _, s := range slices {
		for _, v := range s {
			counts[v]++
		}
	}

	var outSlice []string
	for v, count := range counts {
		if count == len(slices) {
			outSlice = append(outSlice, v)
		}
	}
	return outSlice
}

func stringSliceRemoveCommon(common []string, original []string) []string {
	var commonMap = make(map[string]interface{})
	for _, s := range common {
		commonMap[s] = struct{}{}
	}
	var outSlice []string
	for _, s := range original {
		if _, ok := commonMap[s]; !ok {
			outSlice = append(outSlice, s)
		}
	}
	return outSlice
}
