package main

import (
	"context"
	"flag"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/Mwangi-Derrick/radixip/proto/radixip"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func main() {
	target := flag.String("target", "localhost:50051", "gRPC address")
	requests := flag.Int("requests", 1000, "number of RPCs")
	concurrency := flag.Int("concurrency", 32, "number of workers")
	ip := flag.String("ip", "203.0.113.250", "x-forwarded-for value")
	flag.Parse()

	conn, err := grpc.NewClient(*target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	client := pb.NewRadixServiceClient(conn)

	var counts sync.Map
	var next atomic.Int64
	var wg sync.WaitGroup
	for worker := 0; worker < *concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				n := int(next.Add(1))
				if n > *requests {
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				ctx = metadata.AppendToOutgoingContext(ctx, "x-forwarded-for", *ip)
				_, callErr := client.Lookup(ctx, &pb.LookupRequest{Ip: "192.0.2.1"})
				cancel()
				code := "OK"
				if callErr != nil {
					code = status.Code(callErr).String()
				}
				countsByCode(&counts, code)
			}
		}()
	}
	wg.Wait()

	fmt.Println("status, count")
	counts.Range(func(key, value interface{}) bool {
		fmt.Printf("%s, %d\n", key.(string), value.(*atomic.Int64).Load())
		return true
	})
}

func countsByCode(counts *sync.Map, code string) {
	value, _ := counts.LoadOrStore(code, &atomic.Int64{})
	value.(*atomic.Int64).Add(1)
}
