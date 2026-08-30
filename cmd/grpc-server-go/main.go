package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	radixip "github.com/Mwangi-Derrick/radixip/lib/go/engine"
	pb "github.com/Mwangi-Derrick/radixip/proto/radixip"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
)

var (
	lookupsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "radixip_lookups_total",
			Help: "Total number of IP lookups performed",
		},
		[]string{"result"},
	)
	insertsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "radixip_inserts_total",
			Help: "Total number of prefix insertions",
		},
		[]string{"status"},
	)
	removalsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "radixip_removals_total",
			Help: "Total number of prefix removals",
		},
	)
	lookupDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "radixip_lookup_duration_seconds",
			Help:    "Duration of IP lookup requests in seconds",
			Buckets: prometheus.DefBuckets,
		},
	)
	activeRoutes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "radixip_active_routes",
			Help: "Current total number of active routes in tree",
		},
	)
)

func init() {
	prometheus.MustRegister(lookupsTotal)
	prometheus.MustRegister(insertsTotal)
	prometheus.MustRegister(removalsTotal)
	prometheus.MustRegister(lookupDuration)
	prometheus.MustRegister(activeRoutes)
}

type server struct {
	pb.UnimplementedRadixServiceServer
	engine *radixip.EngineWrapper
}

func newServer() *server {
	engine := radixip.NewEngineWrapperWithTree(radixip.EngineStandard, radixip.AtomicRadixNode, true)
	return &server{engine: engine}
}

func (s *server) Insert(ctx context.Context, req *pb.InsertRequest) (*pb.InsertResponse, error) {
	_, ipnet, err := net.ParseCIDR(req.Prefix)
	if err != nil {
		insertsTotal.WithLabelValues("error").Inc()
		return &pb.InsertResponse{Success: false, ErrorMessage: err.Error()}, nil
	}

	var meta radixip.Metadata
	if req.Metadata != nil {
		meta.Value = req.Metadata.Value
		if req.Metadata.Attributes != nil {
			meta.Attributes = req.Metadata.Attributes
		}
	}

	err = s.engine.Insert(ipnet, meta)
	if err != nil {
		insertsTotal.WithLabelValues("error").Inc()
		return &pb.InsertResponse{Success: false, ErrorMessage: err.Error()}, nil
	}

	insertsTotal.WithLabelValues("success").Inc()
	activeRoutes.Set(float64(s.engine.Size()))
	return &pb.InsertResponse{Success: true, IsNew: true}, nil
}

func (s *server) Lookup(ctx context.Context, req *pb.LookupRequest) (*pb.LookupResponse, error) {
	start := time.Now()
	defer func() {
		lookupDuration.Observe(time.Since(start).Seconds())
	}()

	ip := net.ParseIP(req.Ip)
	if ip == nil {
		lookupsTotal.WithLabelValues("invalid").Inc()
		return &pb.LookupResponse{Found: false}, nil
	}

	result := s.engine.Lookup(ip)
	if result == nil {
		lookupsTotal.WithLabelValues("miss").Inc()
		return &pb.LookupResponse{Found: false}, nil
	}

	lookupsTotal.WithLabelValues("hit").Inc()
	return &pb.LookupResponse{
		Found: true,
		Metadata: &pb.Metadata{
			Value:      result.Value,
			Attributes: result.Attributes,
		},
	}, nil
}

func (s *server) Remove(ctx context.Context, req *pb.RemoveRequest) (*pb.RemoveResponse, error) {
	_, ipnet, err := net.ParseCIDR(req.Prefix)
	if err != nil {
		return &pb.RemoveResponse{Found: false}, nil
	}

	removed := s.engine.Remove(ipnet)
	if removed == nil {
		return &pb.RemoveResponse{Found: false}, nil
	}

	removalsTotal.Inc()
	activeRoutes.Set(float64(s.engine.Size()))
	return &pb.RemoveResponse{
		Found: true,
		Metadata: &pb.Metadata{
			Value:      removed.Value,
			Attributes: removed.Attributes,
		},
	}, nil
}

func (s *server) Contains(ctx context.Context, req *pb.ContainsRequest) (*pb.ContainsResponse, error) {
	_, ipnet, err := net.ParseCIDR(req.Prefix)
	if err != nil {
		return &pb.ContainsResponse{Contains: false}, nil
	}
	return &pb.ContainsResponse{Contains: s.engine.Contains(ipnet)}, nil
}

func (s *server) Clear(ctx context.Context, req *pb.ClearRequest) (*pb.ClearResponse, error) {
	s.engine.Clear()
	activeRoutes.Set(0)
	return &pb.ClearResponse{Success: true}, nil
}

func (s *server) GetStats(ctx context.Context, req *pb.StatsRequest) (*pb.StatsResponse, error) {
	stats := s.engine.Stats()
	return &pb.StatsResponse{
		Inserts:  int64(stats.Inserts),
		Lookups:  int64(stats.Lookups),
		Hits:     int64(stats.Hits),
		Misses:   int64(stats.Misses),
		Removals: int64(stats.Removals),
		Size:     int64(stats.Size),
	}, nil
}

func (s *server) StreamInsert(stream pb.RadixService_StreamInsertServer) error {
	var count uint64
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.StreamInsertResponse{InsertedCount: count})
		}
		if err != nil {
			return err
		}
		_, ipnet, parseErr := net.ParseCIDR(req.Prefix)
		if parseErr == nil {
			var meta radixip.Metadata
			if req.Metadata != nil {
				meta.Value = req.Metadata.Value
				meta.Attributes = req.Metadata.Attributes
			}
			if err := s.engine.Insert(ipnet, meta); err == nil {
				count++
			}
		}
	}
}

func main() {
	grpcPort := os.Getenv("GRPC_PORT")
	if grpcPort == "" {
		grpcPort = "50051"
	}
	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9090"
	}

	// Start Prometheus HTTP Metrics Server
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Printf("[Go gRPC Server] Metrics endpoint listening on :%s/metrics\n", metricsPort)
		if err := http.ListenAndServe(":"+metricsPort, nil); err != nil {
			log.Fatalf("Metrics server failed: %v", err)
		}
	}()

	// Start gRPC Server
	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", grpcPort, err)
	}

	grpcServer := grpc.NewServer()
	srv := newServer()
	pb.RegisterRadixServiceServer(grpcServer, srv)

	log.Printf("[Go gRPC Server] RadixIP gRPC service listening on :%s\n", grpcPort)

	// Graceful shutdown handling
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	<-stop
	log.Println("[Go gRPC Server] Shutting down gracefully...")
	grpcServer.GracefulStop()
	fmt.Println("Server stopped")
}
