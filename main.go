// main.go
package main

import (
	"context"
	"flag"
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
	checkAuth := flag.Bool("check-auth", false, "Check SSO login without booking time")
	flag.Parse()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[INIT] %v", err)
	}
	log.Println("[INIT] Configuration loaded.")

	notifier := notify.New(cfg.WebhookURL)
	exec := executor.New(*cfg)
	if *checkAuth {
		if err := exec.CheckAuth(context.Background()); err != nil {
			log.Fatal(err)
		}
		log.Println("SSO session verified; no time booked")
		return
	}
	state := tracker.New(cfg.StateFile)
	sched := scheduler.New(*cfg, state, exec, notifier)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	webSrv := web.New(*cfg, state, exec, notifier, cfg.WebPort)
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
