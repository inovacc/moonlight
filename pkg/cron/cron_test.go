package cron

import (
	"context"
	"fmt"
	"testing"
)

func TestNewCronScheduler(t *testing.T) {
	// TestNewCronScheduler tests the NewCronScheduler function.
	// It should return a new instance of Cron.
	ctx := context.Background()
	c, err := NewCronScheduler(ctx)
	if err != nil {
		t.Fatalf("NewCronScheduler() error = %v", err)
	}
	if c == nil {
		t.Fatalf("NewCronScheduler() = nil")
	}

	ch := make(chan struct{})

	c.AddFunc(Minute, func() {
		fmt.Println("Every minute")
		close(ch)
	})

	<-ch
}
