// mock-target opens a configurable set of TCP ports (OPEN_PORTS), optionally installs
// iptables DROP rules for FILTERED_PORTS (requires NET_ADMIN) to produce real connect
// timeouts, and always listens on HEALTH_PORT for the container healthcheck.
// Invoked with -healthcheck it dials HEALTH_PORT and exits 0/1.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

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

	startListener(healthPort)
	for _, p := range splitPorts(os.Getenv("OPEN_PORTS")) {
		startListener(p)
	}
	log.Printf("mock-target ready health=%s open=%q filtered=%q",
		healthPort, os.Getenv("OPEN_PORTS"), os.Getenv("FILTERED_PORTS"))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

func startListener(port string) {
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
