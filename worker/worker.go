package worker

import (
	"bytes"
	"fmt"
	"github.com/enriquebris/goconcurrentqueue"
	"net/http"
	"time"
)

type RelayRequest struct {
	Method  string
	Url     string
	Headers map[string][]string
	Body    []byte
	Retries uint8
}

func Run(queue *goconcurrentqueue.FIFO, baseUrl string) {
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
