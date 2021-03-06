package main

import (
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/autowp/auth"
	sentry "github.com/getsentry/sentry-go"
)

func main() {

	config := auth.LoadConfig()

	auth.ValidateConfig(config)

	err := sentry.Init(sentry.ClientOptions{
		Dsn:         config.Sentry.DSN,
		Environment: config.Sentry.Environment,
	})

	if err != nil {
		log.Printf("Error: %v\n", err)
		os.Exit(1)
		return
	}

	wg := &sync.WaitGroup{}
	s, err := auth.NewService(wg, config)

	if err != nil {
		log.Printf("Error: %v\n", err)
		os.Exit(1)
		return
	}

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	for sig := range c {
		log.Printf("captured %v, stopping and exiting.", sig)

		sentry.Flush(time.Second * 5)

		s.Close()
		wg.Wait()
		os.Exit(0)
	}

	sentry.Flush(time.Second * 5)

	s.Close()
	wg.Wait()
	os.Exit(0)
}
