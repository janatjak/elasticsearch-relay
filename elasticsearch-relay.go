package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"os"
	"runtime"
	"time"
)

// import _ "net/http/pprof"

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Missing baseUrl argument!")
		os.Exit(1)
	}

	baseUrl := os.Args[1]
	fmt.Println("Starting with base url: ", baseUrl)

	relayQueue := NewQueue()

	go RunWorker(relayQueue, baseUrl)

	go func() {
		for {
			// every 10 mins
			time.Sleep(time.Minute * 10)

			fmt.Println("Maintenance: Run GC Start")
			runtime.GC()
			fmt.Println("Maintenance: Run GC Finish")
		}
	}()

	// debug
	// go http.ListenAndServe(":8079", nil)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	// r := gin.Default()

	r.GET("/info", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"count": relayQueue.Len(),
		})
	})

	r.GET("/clean", func(c *gin.Context) {
		relayQueue.Clean()
		c.Status(200)
	})

	r.NoRoute(func(c *gin.Context) {
		body, _ := c.GetRawData()
		relayQueue.Push(c.Request, body)
		c.Status(200)
	})
	r.Run()
}
