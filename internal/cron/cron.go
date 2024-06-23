package cron

import (
	"context"
	"github.com/robfig/cron/v3"
)

type Cron struct {
	cron *cron.Cron
	ctx  context.Context
}

type EntryID cron.EntryID

func NewCron(ctx context.Context) (*Cron, error) {
	return &Cron{
		cron: cron.New(cron.WithSeconds()),
		ctx:  ctx,
	}, nil
}

func (c *Cron) Start() {
	go func() {
		defer c.cron.Stop()
		c.cron.Start()

		for {
			select {
			case <-c.ctx.Done():
				break
			}
		}
	}()
}

func (c *Cron) AddFunc(spec string, cmd func()) (EntryID, error) {
	id, err := c.cron.AddFunc(spec, cmd)
	return EntryID(id), err
}
