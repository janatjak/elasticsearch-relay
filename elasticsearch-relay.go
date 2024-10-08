package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

// import _ "net/http/pprof"

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Missing baseUrl argument!")
		os.Exit(1)
	}

	debugMode := os.Getenv("APP_DEBUG") == "1"

	baseUrl := os.Args[1]
	fmt.Println("Starting with base url: ", baseUrl)

	relayQueue := NewQueue()

	go RunWorker(relayQueue, baseUrl, debugMode)

	go func() {
		for {
			// every 10 mins
			time.Sleep(time.Minute * 10)

			fmt.Println("[Maintenance] Start cleanup")
			runtime.GC()
			debug.FreeOSMemory()

			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			fmt.Printf("[Maintenance] Alloc: %vMB, TotalAlloc: %vMB, Sys: %vMB\n", bToMb(m.Alloc), bToMb(m.TotalAlloc), bToMb(m.Sys))
		}
	}()

	// debug
	// go http.ListenAndServe(":8079", nil)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// r := gin.Default()

	r.GET("/info", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"count":      relayQueue.Len(),
			"totalCount": relayQueue.TotalCount(),
		})
	})

	r.GET("/health-check", func(c *gin.Context) {
		c.Status(200)
	})

	r.NoRoute(func(c *gin.Context) {
		id := uuid.New()
		body, _ := c.GetRawData()
		relayQueue.Push(id, c.Request, body)
		c.Status(200)

		if debugMode {
			fmt.Println("[Server] Request", id, "accepted: ", c.Request.RequestURI)
		}
	})
	r.Run()
}
