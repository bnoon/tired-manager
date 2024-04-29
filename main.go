package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var Version string

func loadJobs(fname string) [][]string {
	jobs := make([][]string, 0)
	jFile, err := os.Open(fname)
	if err != nil {
		log.Panicf("Unable to load jobs file %s: %s", fname, err)
	}
	scanner := bufio.NewScanner(jFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		job := strings.Split(line, " ")
		if len(job) < 2 {
			log.Panicf("Invalid job line: %s", line)
		}
		jobs = append(jobs, job)
	}
	return jobs
}

func loadTokens() map[string]string {
	tokens := make(map[string]string)
	raw_tokens := os.Getenv("TOKENS")
	for _, pair := range strings.Split(raw_tokens, "|") {
		if user, tkn, found := strings.Cut(pair, " "); found {
			tokens[tkn] = user
		} else {
			log.Panicf("invalid token pair: %s", pair)
		}
	}
	if len(tokens) == 0 {
		log.Panic("Missing valid tokens")
	}
	return tokens
}

func main() {
	fmt.Printf("Tired manager - version %s\n", Version)
	var port = flag.String("port", "8080", "port at which the proxy server listens for requests")
	var idleTime = flag.Int("idle-time", 60, "idle time in seconds after which the application shuts down, if no requests where received")
	var maxJobTime = flag.Int("job-time", 15*60, "max job duration in seconds")
	flag.Parse()

	// Server run context
	serverCtx, serverStopCtx := context.WithCancel(context.Background())

	log.Print("About to start manager...")

	mgr := StartIdleManager(serverCtx, *port, time.Duration(*idleTime)*time.Second, time.Duration(*maxJobTime)*time.Second)

	// Setting up signal capturing
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// wait for either: the manager to stop or the application to exit
	select {
	case err := <-mgr.Done():
		if err != nil {
			log.Printf("Manager error: %s", err)
		}
		log.Print("Manager finished")
	case sig := <-stop:
		log.Printf("Received signal '%s', shutdown application...", sig)
		// Shutdown signal with grace period of 30 seconds
		shutdownCtx, _ := context.WithTimeout(serverCtx, 30*time.Second)

		go func() {
			<-shutdownCtx.Done()
			if shutdownCtx.Err() == context.DeadlineExceeded {
				log.Fatal("graceful shutdown timed out.. forcing exit.")
			}
		}()

		// Trigger graceful shutdown
		err := mgr.Shutdown(shutdownCtx)
		if err != nil {
			log.Fatal(err)
		}
		serverStopCtx()
	}

	log.Print("Tired manager - exit")
}
