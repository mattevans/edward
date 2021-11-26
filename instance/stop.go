package instance

import (
	"github.com/pkg/errors"
	"github.com/mattevans/edward/home"
	"github.com/mattevans/edward/instance/processes"
	"github.com/mattevans/edward/services"
	"github.com/mattevans/edward/tracker"
	"github.com/mattevans/edward/worker"
)

// Stop stops this service
func Stop(dirConfig *home.EdwardConfiguration, c *services.ServiceConfig, cfg services.OperationConfig, overrides services.ContextOverride, task tracker.Task, pool *worker.Pool) error {
	instance, err := Load(dirConfig, &processes.Processes{}, c, overrides)
	if err != nil {
		return errors.WithStack(err)
	}
	if instance.Pid == 0 {
		instance.clearState()
		return nil
	}
	err = pool.Enqueue(func() error {
		return errors.WithStack(instance.StopSync(cfg, overrides, task))
	})
	return errors.WithStack(err)
}
