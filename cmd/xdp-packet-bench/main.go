package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	radixipxdp "github.com/Mwangi-Derrick/radixip/lib/go/xdp"
)

func main() {
	iface := flag.String("iface", "", "ingress interface carrying benchmark packets")
	source := flag.String("source", "", "source IP to add to the XDP blocklist")
	destination := flag.String("destination", "", "destination IP address")
	clientNamespace := flag.String("client-netns", "", "namespace from which to generate packets")
	mode := flag.String("mode", "skb", "XDP mode: skb or drv")
	duration := flag.Int("duration", 5, "flood duration in seconds")
	_ = flag.String("sender-bin", "", "optional sender binary path for compatibility")
	senderOnly := flag.Bool("sender-only", false, "internal: run in sender namespace sending raw high-throughput packets")
	flag.Parse()

	// Internal high-throughput sender mode executed inside client-netns
	if *senderOnly {
		runSender(*source, *destination, *duration)
		return
	}

	if *iface == "" || *source == "" || *destination == "" || *clientNamespace == "" {
		fail("iface, source, destination, and client-netns are required")
	}
	if *duration < 1 || *duration > 60 {
		fail("duration must be from 1 to 60 seconds")
	}
	if *mode != "skb" && *mode != "drv" {
		fail("mode must be skb or drv")
	}

	sourceIP, err := netip.ParseAddr(*source)
	if err != nil {
		fail("parse source IP: %v", err)
	}
	destinationIP, err := netip.ParseAddr(*destination)
	if err != nil {
		fail("parse destination IP: %v", err)
	}
	if sourceIP.Is4() != destinationIP.Is4() {
		fail("source and destination IP families must match")
	}
	bits := 128
	if sourceIP.Is4() {
		bits = 32
	}
	blockPrefix := netip.PrefixFrom(sourceIP, bits)

	loader, err := radixipxdp.Load(radixipxdp.Config{
		Iface:      *iface,
		DriverMode: *mode == "drv",
		MinV4:      1,
		MinV6:      1,
	})
	if err != nil {
		fail("load XDP object: %v", err)
	}
	defer loader.Close()

	if err := loader.Put(blockPrefix); err != nil {
		fail("insert source IP into blocklist: %v", err)
	}
	if err := loader.Attach(); err != nil {
		fail("attach XDP mode %s to %s: %v", *mode, *iface, err)
	}
	if (*mode == "drv" && loader.AttachedMode != "drv") || (*mode == "skb" && loader.AttachedMode != "skb") {
		fail("requested mode %s but loader attached mode %s", *mode, loader.AttachedMode)
	}

	before, err := loader.Stats()
	if err != nil {
		fail("read initial XDP counters: %v", err)
	}

	selfExe, err := os.Executable()
	if err != nil {
		fail("locate executable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*duration+5)*time.Second)
	defer cancel()

	// Launch high-throughput parallel UDP sender inside client netns
	cmd := exec.CommandContext(ctx, "ip", "netns", "exec", *clientNamespace,
		selfExe,
		"-sender-only",
		"-source", sourceIP.String(),
		"-destination", destinationIP.String(),
		"-duration", strconv.Itoa(*duration),
	)

	start := time.Now()
	output, runErr := cmd.CombinedOutput()
	elapsed := time.Since(start)

	if ctx.Err() != nil {
		fail("packet generator timed out after %s: %s", elapsed, strings.TrimSpace(string(output)))
	}
	if runErr != nil {
		fail("run packet generator: %v: %s", runErr, strings.TrimSpace(string(output)))
	}

	after, err := loader.Stats()
	if err != nil {
		fail("read final XDP counters: %v", err)
	}
	dropped := counterDelta(before.V4Drop, after.V4Drop)
	if !sourceIP.Is4() {
		dropped = counterDelta(before.V6Drop, after.V6Drop)
	}
	if dropped == 0 {
		fail("XDP recorded zero drops; verify the ingress interface, namespace path, and source IP. output: %s", strings.TrimSpace(string(output)))
	}

	seconds := elapsed.Seconds()
	fmt.Printf("runtime=go requested_mode=%s attached_mode=%s iface=%s source=%s destination=%s duration_seconds=%.3f xdp_dropped_packets=%d xdp_drop_pps=%.0f\n",
		*mode, loader.AttachedMode, *iface, sourceIP, destinationIP, seconds, dropped, float64(dropped)/seconds)
	fmt.Printf("xdp_pass_delta=%d xdp_parse_error_delta=%d\n",
		counterDelta(before.V4Pass, after.V4Pass)+counterDelta(before.V6Pass, after.V6Pass),
		counterDelta(before.ParseErr, after.ParseErr))
}

func runSender(srcIPStr, dstIPStr string, durationSec int) {
	numWorkers := runtime.NumCPU()
	if numWorkers < 2 {
		numWorkers = 2
	}
	if numWorkers > 8 {
		numWorkers = 8
	}

	payload := make([]byte, 64)
	dstAddr := &net.UDPAddr{
		IP:   net.ParseIP(dstIPStr),
		Port: 9999,
	}

	deadline := time.Now().Add(time.Duration(durationSec) * time.Second)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			srcAddr := &net.UDPAddr{
				IP:   net.ParseIP(srcIPStr),
				Port: 20000 + workerID,
			}
			conn, err := net.DialUDP("udp", srcAddr, dstAddr)
			if err != nil {
				return
			}
			defer conn.Close()

			for time.Now().Before(deadline) {
				// Tight send loop
				for j := 0; j < 64; j++ {
					_, _ = conn.Write(payload)
				}
			}
		}(i)
	}

	wg.Wait()
}

func counterDelta(before, after uint64) uint64 {
	if after < before {
		return 0
	}
	return after - before
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "xdp-packet-bench: "+format+"\n", args...)
	os.Exit(1)
}
