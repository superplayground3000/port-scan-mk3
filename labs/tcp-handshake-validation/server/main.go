// Command target is a minimal TCP listener used to observe how port-scan-mk3
// establishes and tears down connections. It accepts connections on an open
// port and, crucially, does NOT initiate the close itself: it blocks on Read
// until the peer (the scanner) closes, so the four-way teardown is driven by
// the scanner. This lets the packet capture attribute the FIN to the scanner.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"time"
)

func main() {
	addr := flag.String("addr", ":9000", "listen address")
	healthcheck := flag.Bool("healthcheck", false, "dial the listen port and exit 0/1")
	flag.Parse()

	// Health probe runs over loopback (127.0.0.1), so it never appears on the
	// eth0 capture that the scanner analyzes.
	if *healthcheck {
		conn, err := net.DialTimeout("tcp", "127.0.0.1"+*addr, 2*time.Second)
		if err != nil {
			os.Exit(1)
		}
		_ = conn.Close()
		return
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}
	log.Printf("target listening on %s", *addr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		log.Printf("accepted connection from %s", conn.RemoteAddr())
		go func(c net.Conn) {
			defer c.Close()
			// Block until the scanner closes (Read returns EOF on the peer FIN),
			// so the scanner is the side that initiates teardown.
			buf := make([]byte, 1)
			_, _ = c.Read(buf)
		}(conn)
	}
}
