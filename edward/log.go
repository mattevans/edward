package edward

import (
	"log"
	"sort"
	"time"

	"github.com/mattevans/edward/home"
	"github.com/mattevans/edward/instance/servicelogs"

	"github.com/pkg/errors"
	"github.com/mattevans/edward/instance"
	"github.com/mattevans/edward/services"
)

func (c *Client) Log(names []string, cancelChannel <-chan struct{}) error {
	if len(names) == 0 {
		return errors.New("at least one service or group must be specified")
	}
	if cancelChannel == nil {
		return errors.New("a cancellation channel is required")
	}

	sgs, err := c.getServicesOrGroups(names)
	if err != nil {
		return errors.WithStack(err)
	}

	var tailChannel = make(chan servicelogs.LogLine)
	var lines []servicelogs.LogLine
	for _, sg := range sgs {
		switch v := sg.(type) {
		case *services.ServiceConfig:
			newLines, err := followServiceLog(c.DirConfig.LogDir, v, tailChannel)
			if err != nil {
				return err
			}
			lines = append(lines, newLines...)
		case *services.ServiceGroupConfig:
			newLines, err := followGroupLog(c.DirConfig.LogDir, v, tailChannel)
			if err != nil {
				return err
			}
			lines = append(lines, newLines...)
		}
	}

	var stopChannel = make(chan struct{})
	statusTicker := time.NewTicker(time.Second * 5)
	go func() {
		for {
			select {
			case _ = <-statusTicker.C:
				running, err := checkAllRunning(c.DirConfig, sgs)
				if err != nil {
					log.Printf("Error checking service state for tailing: %v", err)
					continue
				}
				// All services stopped, notify the log process
				if !running {
					statusTicker.Stop()
					close(stopChannel)
					return
				}
			case _ = <-cancelChannel:
				close(stopChannel)
				return
			}
		}
	}()

	var logChannel = make(chan servicelogs.LogLine)
	c.UI.ShowLog(logChannel, services.CountServices(sgs) > 1)

	// Sort initial lines
	sort.Sort(byTime(lines))
	for _, line := range lines {
		logChannel <- line
	}

	var running = true
	for running {
		select {
		case logMessage := <-tailChannel:
			logChannel <- logMessage
		case <-stopChannel:
			running = false
		}
	}

	return nil
}

func checkAllRunning(dirConfig *home.EdwardConfiguration, sgs []services.ServiceOrGroup) (bool, error) {
	allServices := services.Services(sgs)
	for _, s := range allServices {
		running, err := instance.HasRunning(dirConfig, s)
		if err != nil {
			return false, errors.WithStack(err)
		}
		if running {
			return true, nil
		}
	}
	return false, nil
}
