package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type IdleManager struct {
	idleTime   time.Duration
	jobTime    time.Duration
	timer      *time.Timer
	server     *http.Server
	chanFinish chan error
}

func StartIdleManager(ctx context.Context, port string, idleTime, maxJobTime time.Duration) *IdleManager {
	mgr := &IdleManager{
		idleTime:   idleTime,
		jobTime:    maxJobTime,
		timer:      time.NewTimer(idleTime),
		server:     &http.Server{Addr: fmt.Sprintf(":%s", port)},
		chanFinish: make(chan error, 1),
	}

	mgr.server.Handler = mgr.Handle(ctx)

	go func() {
		// wait for the idleTimer to expire, or the context to cancel
		select {
		case <-mgr.TimerDone():
			log.Printf("Idle time (%s) expired, shutting down proxy...", mgr.idleTime.String())
		case <-ctx.Done():
			log.Print("Shutting down proxy...")
		}
		ctx, cancelShutdown := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancelShutdown()
		if err := mgr.server.Shutdown(ctx); err != nil {
			mgr.chanFinish <- fmt.Errorf("error while shutting down proxy server: %w", err)
		}
	}()

	// start proxy
	go func() {
		log.Printf("Start job server, serving at http://%s", mgr.server.Addr)
		if err := mgr.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			mgr.chanFinish <- fmt.Errorf("error during handling proxy request: %w", err)
		}
		// Proxy stopped
		log.Print("Job server shut down")
		close(mgr.chanFinish)
	}()

	return mgr
}

// Returns a channel that returns the current time when the timer expires
func (p *IdleManager) TimerDone() <-chan time.Time {
	return p.timer.C
}

// Channel that is closed when the proxy server is shut down.
// If any error occurred during start or shut down of the proxy server, it is sent through the channel.
func (p *IdleManager) Done() <-chan error {
	return p.chanFinish
}

func (p *IdleManager) Shutdown(ctx context.Context) error {
	return p.server.Shutdown(ctx)
}

func (p *IdleManager) Handle(ctx context.Context) http.Handler {
	r := chi.NewRouter()
	jobs := loadJobs("jobs")
	tokens := loadTokens()

	r.Use(CheckBearer(tokens))
	r.Use(middleware.Logger)

	for _, j := range jobs {
		job := j
		r.Get(job[0], func(w http.ResponseWriter, r *http.Request) {
			p.timer.Reset(p.jobTime)
			cmd := exec.CommandContext(ctx, job[1], job[2:]...)
			log.Println(cmd)
			out, err := cmd.CombinedOutput()
			if err != nil {
				w.Write([]byte(fmt.Sprint(err)))
			} else {
				w.Write(out)
			}
			p.timer.Reset(p.idleTime)
		})
	}

	return r
}

func CheckBearer(creds map[string]string) func(next http.Handler) http.Handler {
	var bearerKey = http.CanonicalHeaderKey("Bearer")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token := r.Header.Get(bearerKey); token != "" {
				if user, ok := creds[token]; ok {
					r.Header["x-user"] = []string{user}
					log.Printf("request from %s", user)
					next.ServeHTTP(w, r)
					return
				}
			}
			w.WriteHeader(http.StatusUnauthorized)
		})
	}
}
