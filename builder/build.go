package builder

import (
	"bufio"
	"io"
	"strings"

	"github.com/pkg/errors"
	"github.com/mattevans/edward/home"
	"github.com/mattevans/edward/instance"
	"github.com/mattevans/edward/instance/processes"
	"github.com/mattevans/edward/services"
	"github.com/mattevans/edward/tracker"
)

type builder struct {
	Cfg       services.OperationConfig
	Overrides services.ContextOverride
}

func New(cfg services.OperationConfig, overrides services.ContextOverride) *builder {
	return &builder{
		Cfg:       cfg,
		Overrides: overrides,
	}
}

func (b *builder) Build(dirConfig *home.EdwardConfiguration, task tracker.Task, service ...*services.ServiceConfig) error {
	for _, service := range service {
		if b.Cfg.IsExcluded(service) {
			return nil
		}
		err := b.BuildWithTracker(dirConfig, task.Child(service.GetName()), service, false)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// BuildWithTracker builds a service.
// If force is false, the build will be skipped if the service is already running.
func (b *builder) BuildWithTracker(dirConfig *home.EdwardConfiguration, task tracker.Task, service *services.ServiceConfig, force bool) error {
	if !service.Backend().HasBuildStep() {
		return nil
	}
	if task == nil {
		return errors.New("task is nil")
	}
	job := task.Child("Build")
	job.SetState(tracker.TaskStateInProgress)

	c, err := instance.Load(dirConfig, &processes.Processes{}, service, b.Overrides)
	if err != nil {
		return errors.WithStack(err)
	}
	if !force && c.Pid != 0 {
		job.SetState(tracker.TaskStateWarning, "Already running")
		return nil
	}

	err = instance.DeleteAllStatusesForService(service, dirConfig.StateDir)
	if err != nil {
		return errors.WithStack(err)
	}

	builder, err := services.GetBuilder(b.Cfg, service)
	if err != nil {
		return errors.WithStack(err)
	}

	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			job.AddOutput(scanner.Text())
		}
	}()
	err = builder.Build(b.Cfg.WorkingDir, c.Getenv, w)

	if err != nil {
		job.SetState(tracker.TaskStateFailed, err.Error(), strings.Join(job.Output(), "\n"))
		return errors.WithMessage(err, "running build command")
	}
	job.SetState(tracker.TaskStateSuccess)
	return nil
}
