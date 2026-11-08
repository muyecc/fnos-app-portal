package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example-portal/internal/gateway"
)

func main() {
	configPath := flag.String("config", "/app/target/config.json", "portal configuration")
	health := flag.Bool("healthcheck", false, "check the local Unix listener")
	flag.Parse()
	b, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	var cfg gateway.Config
	if err = json.Unmarshal(b, &cfg); err != nil {
		log.Fatal(err)
	}
	if *health {
		c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", cfg.Socket)
		}}}
		r, e := c.Get("http://unix/__health")
		if e != nil {
			log.Fatal(e)
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			log.Fatal("unhealthy")
		}
		fmt.Println("healthy")
		return
	}
	h, err := gateway.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	ln, err := gateway.Listen(cfg.Socket)
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	server := &http.Server{Handler: h, ConnContext: gateway.ConnContext, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 65 * time.Second, MaxHeaderBytes: 65536}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-stop
		h.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("portal started prefix=%s frontend=%s socket=%s", cfg.Prefix, cfg.FrontendVersion, cfg.Socket)
	if err = server.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
