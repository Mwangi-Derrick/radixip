package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Mwangi-Derrick/radixip/lib/go/engine"
	"github.com/Mwangi-Derrick/radixip/lib/go/adapters/gin"
	"github.com/Mwangi-Derrick/radixip/lib/go/adapters/echo"
	"github.com/Mwangi-Derrick/radixip/lib/go/adapters/fiber"
	"github.com/Mwangi-Derrick/radixip/lib/go/adapters/interceptor"

	gogin "github.com/gin-gonic/gin"
	goecho "github.com/labstack/echo/v4"
	gofiber "github.com/gofiber/fiber/v2"
	"google.golang.org/grpc"
)

// EngineAdapter adapts radixip_engine to the middleware Engine interface.
type EngineAdapter struct {
	inner *engine.EngineWrapper
}

func (a *EngineAdapter) Lookup(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return a.inner.Lookup(ip) != nil
}

func (a *EngineAdapter) Insert(prefix *net.IPNet, meta engine.Metadata) error {
	return a.inner.Insert(prefix, meta)
}

func (a *EngineAdapter) Remove(prefix *net.IPNet) *engine.Metadata {
	return a.inner.Remove(prefix)
}

func main() {
	log.Println("🚀 Starting Go Kitchen Sink Test App")
	configPath := "../../config/radixip.yaml"

	// 1. Initialize Shared RadixIP Engine
	radixEngine := engine.NewEngineWrapper(engine.EngineConcurrent, engine.AtomicRadixNode)
	adapter := &EngineAdapter{inner: radixEngine}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Gin Server (Port 8081)
	wg.Add(1)
	go func() {
		defer wg.Done()
		gogin.SetMode(gogin.ReleaseMode)
		r := gogin.New()
		r.Use(gogin.Recovery())
		
		mw, stop, err := radixipgin.NewFromYAML(configPath, adapter)
		if err != nil {
			log.Fatalf("Gin middleware error: %v", err)
		}
		defer stop()
		r.Use(mw)
		
		r.GET("/api/v1/public", func(c *gogin.Context) { c.String(200, "gin public ok") })
		r.GET("/api/v1/auth", func(c *gogin.Context) { c.String(200, "gin auth ok") })
		
		go func() {
			<-ctx.Done()
			log.Println("Shutting down Gin...")
		}()
		log.Println("🍸 Gin listening on :8081")
		if err := r.Run(":8081"); err != nil {
			log.Printf("Gin exited: %v", err)
		}
	}()

	// 3. Echo Server (Port 8082)
	wg.Add(1)
	go func() {
		defer wg.Done()
		e := goecho.New()
		e.HideBanner = true
		e.HidePort = true
		
		mw, stop, err := radixipecho.NewFromYAML(configPath, adapter)
		if err != nil {
			log.Fatalf("Echo middleware error: %v", err)
		}
		defer stop()
		e.Use(mw)
		
		e.GET("/api/v1/public", func(c goecho.Context) error { return c.String(200, "echo public ok") })
		e.GET("/api/v1/auth", func(c goecho.Context) error { return c.String(200, "echo auth ok") })
		
		go func() {
			<-ctx.Done()
			e.Shutdown(context.Background())
			log.Println("Shutting down Echo...")
		}()
		log.Println("🔊 Echo listening on :8082")
		if err := e.Start(":8082"); err != nil {
			log.Printf("Echo exited: %v", err)
		}
	}()

	// 4. Fiber Server (Port 8083)
	wg.Add(1)
	go func() {
		defer wg.Done()
		app := gofiber.New(gofiber.Config{DisableStartupMessage: true})
		
		mw, stop, err := radixipfiber.NewFromYAML(configPath, adapter)
		if err != nil {
			log.Fatalf("Fiber middleware error: %v", err)
		}
		defer stop()
		app.Use(mw)
		
		app.Get("/api/v1/public", func(c *gofiber.Ctx) error { return c.SendString("fiber public ok") })
		app.Get("/api/v1/auth", func(c *gofiber.Ctx) error { return c.SendString("fiber auth ok") })
		
		go func() {
			<-ctx.Done()
			app.Shutdown()
			log.Println("Shutting down Fiber...")
		}()
		log.Println("⚡ Fiber listening on :8083")
		if err := app.Listen(":8083"); err != nil {
			log.Printf("Fiber exited: %v", err)
		}
	}()

	// 5. gRPC Server (Port 50051)
	wg.Add(1)
	go func() {
		defer wg.Done()
		unary, stream, stop, err := radixipgrpc.NewFromYAML(configPath, adapter)
		if err != nil {
			log.Fatalf("gRPC interceptor error: %v", err)
		}
		defer stop()
		
		s := grpc.NewServer(
			grpc.UnaryInterceptor(unary),
			grpc.StreamInterceptor(stream),
		)
		
		lis, err := net.Listen("tcp", ":50051")
		if err != nil {
			log.Fatalf("gRPC failed to listen: %v", err)
		}
		
		go func() {
			<-ctx.Done()
			s.GracefulStop()
			log.Println("Shutting down gRPC...")
		}()
		log.Println("📞 gRPC listening on :50051")
		if err := s.Serve(lis); err != nil {
			log.Printf("gRPC exited: %v", err)
		}
	}()

	// Wait for interrupt
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down servers...")
	cancel()
	wg.Wait()
	log.Println("Done.")
}
