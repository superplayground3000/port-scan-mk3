// mock-target opens TCP ports and can count accepted connections. It can also
// install container-local firewall rules for deterministic timeouts.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type probeCounter struct {
	value atomic.Uint64
}

func (c *probeCounter) add() {
	c.value.Add(1)
}

func probeCounterHandler(counter *probeCounter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/count", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, counter.value.Load())
	})
	mux.HandleFunc("/reset", func(w http.ResponseWriter, _ *http.Request) {
		counter.value.Store(0)
		_, _ = fmt.Fprintln(w, 0)
	})
	return mux
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe local health port and exit")
	flag.Parse()

	healthPort := getenv("HEALTH_PORT", "19999")

	if *healthcheck {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+healthPort, 2*time.Second)
		if err != nil {
			os.Exit(1)
		}
		_ = conn.Close()
		os.Exit(0)
	}

	for _, p := range splitPorts(os.Getenv("FILTERED_PORTS")) {
		out, err := exec.Command("iptables", "-A", "INPUT", "-p", "tcp", "--dport", p, "-j", "DROP").CombinedOutput()
		if err != nil {
			log.Fatalf("iptables DROP tcp/%s failed: %v: %s", p, err, out)
		}
		log.Printf("installed DROP rule for tcp/%s", p)
	}

	startListener(healthPort, nil)
	for _, p := range splitPorts(os.Getenv("OPEN_PORTS")) {
		startListener(p, nil)
	}
	counter := &probeCounter{}
	for _, p := range splitPorts(os.Getenv("COUNTED_PORTS")) {
		startListener(p, counter.add)
	}
	if controlPort := strings.TrimSpace(os.Getenv("CONTROL_PORT")); controlPort != "" {
		go func() {
			if err := http.ListenAndServe(":"+controlPort, probeCounterHandler(counter)); err != nil {
				log.Fatalf("probe counter control server failed: %v", err)
			}
		}()
	}
	log.Printf("mock-target ready health=%s open=%q counted=%q filtered=%q",
		healthPort, os.Getenv("OPEN_PORTS"), os.Getenv("COUNTED_PORTS"), os.Getenv("FILTERED_PORTS"))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

func startListener(port string, onAccept func()) {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("listen on %s failed: %v", port, err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if onAccept != nil {
				onAccept()
			}
			_ = conn.Close()
		}
	}()
}

func splitPorts(raw string) []string {
	var out []string
	for _, p := range strings.Split(strings.TrimSpace(raw), ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
