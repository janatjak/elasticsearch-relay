package main

import (
	"fmt"
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

func RunWorker(queue *Queue, baseUrl string) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	for {
		for {
			relayRequest := queue.Get()
			if relayRequest == nil {
				break
			}

			// prepare request
			err := sendRequest(client, baseUrl, relayRequest)
			if err != nil {
				// fatal error
				fmt.Println("ERROR send request: ", relayRequest.Url, err)

				if relayRequest.Retries > 5 {
					// max 5 retries
					break
				}

				// put it back to queue
				relayRequest.Retries++
				queue.RePush(relayRequest)

				// wait 10 sec -> server is down?
				time.Sleep(time.Second * 10)
				break
			}
		}

		client.CloseIdleConnections()
		// fmt.Println("🧲 loop")

		// sleep 1 sec
		time.Sleep(time.Second)
	}
}
