// main.go
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/audioinj/time-automation/config"
	"github.com/audioinj/time-automation/executor"
	"github.com/audioinj/time-automation/notify"
	"github.com/audioinj/time-automation/scheduler"
	"github.com/audioinj/time-automation/tracker"
	"github.com/audioinj/time-automation/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[INIT] %v", err)
	}
	log.Println("[INIT] Configuration loaded.")

	notifier := notify.New(cfg.WebhookURL)
	exec := executor.New(*cfg)
	state := tracker.New(cfg.StateFile)
	sched := scheduler.New(*cfg, state, exec, notifier)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	webSrv := web.New(*cfg, state, cfg.WebPort)
	go webSrv.Start(ctx)

	log.Println("[START] Time automation running...")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[STOP] Shutting down gracefully.")
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[PANIC] recovered from panic in scheduler: %v", r)
					}
				}()
				sched.Run(ctx)
			}()
		}
	}
}
