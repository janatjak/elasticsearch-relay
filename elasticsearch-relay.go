package main

import (
	"bytes"
	"fmt"
	"github.com/enriquebris/goconcurrentqueue"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
)

type RelayRequest struct {
	Method  string
	Url     string
	Headers map[string][]string
	Body    []byte
	Retries uint8
}

var queue = goconcurrentqueue.NewFIFO()

func worker(baseUrl string) {
	client := http.Client{
		Timeout: 10 * time.Second,
	}

	for {
		for {
			item, isEmpty := queue.Dequeue()
			if isEmpty != nil {
				break
			}

			relayRequest, ok := item.(*RelayRequest)
			if !ok {
				fmt.Println("ERROR: invalid element", relayRequest)
				continue
			}

			// prepare request
			req, err := http.NewRequest(relayRequest.Method, baseUrl+relayRequest.Url, bytes.NewReader(relayRequest.Body))
			if err != nil {
				fmt.Println("FATAL error: ", err)
			}
			req.Header = relayRequest.Headers

			res, err := client.Do(req)
			client.CloseIdleConnections()
			if err != nil {
				// fatal error
				fmt.Println("ERROR send request: ", req.URL.Path, err)

				if relayRequest.Retries > 5 {
					// max 5 retries
					break
				}

				// put it back to queue
				relayRequest.Retries++
				queue.Enqueue(relayRequest)

				// wait 10 sec -> server is down?
				time.Sleep(time.Second * 10)
				break
			}

			// non 2xx -> ignored
			if res.StatusCode < 200 || res.StatusCode >= 300 {
				resBody := ""
				if res.Body != nil {
					buf := new(bytes.Buffer)
					buf.ReadFrom(res.Body)
					resBody = buf.String()
				}

				fmt.Println("Invalid response: ", res.StatusCode, req.URL.Path, resBody)
			}
		}

		// fmt.Println("🧲 loop")

		// sleep 1 sec
		time.Sleep(time.Second)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Missing baseUrl argument!")
		os.Exit(1)
	}

	baseUrl := os.Args[1]
	fmt.Println("Starting with base url: ", baseUrl)

	go worker(baseUrl)

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

		err = queue.Enqueue(&RelayRequest{
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
