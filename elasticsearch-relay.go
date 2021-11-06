package main

import (
	"fmt"
	"github.com/enriquebris/goconcurrentqueue"
	"github.com/janatjak/elasticsearch-relay/worker"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Missing baseUrl argument!")
		os.Exit(1)
	}

	baseUrl := os.Args[1]
	fmt.Println("Starting with base url: ", baseUrl)

	var queue = goconcurrentqueue.NewFIFO()

	go worker.Run(queue, baseUrl)

	go func() {
		for {
			// every 10 mins
			time.Sleep(time.Minute * 10)

			fmt.Println("Maintenance: Run GC Start")
			runtime.GC()
			fmt.Println("Maintenance: Run GC Finish")
		}
	}()

	http.HandleFunc("/info", func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(200)
		writer.Write([]byte(fmt.Sprintf("{\"count\":%d}", queue.GetLen())))
	})

	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			writer.WriteHeader(500)
			return
		}

		err = queue.Enqueue(&worker.RelayRequest{
			Method:  request.Method,
			Url:     request.RequestURI,
			Headers: request.Header,
			Body:    body,
			Retries: 0,
		})
		if err != nil {
			writer.WriteHeader(500)
			return
		}
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
