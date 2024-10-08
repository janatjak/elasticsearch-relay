package main

import (
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"time"
)

type RelayRequest struct {
	Uuid    uuid.UUID
	Method  string
	Url     string
	Headers map[string][]string
	Body    []byte
	Retries uint8
}

func RunWorker(queue *Queue, baseUrl string, debug bool) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	for {
		for {
			relayRequest := queue.Get()
			if relayRequest == nil {
				break
			}

			relayRequest.Retries = relayRequest.Retries + 1

			// prepare request
			err := sendRequest(client, baseUrl, relayRequest)
			if err != nil {
				// fatal error
				fmt.Println("[WORKER] ERROR send request", relayRequest.Uuid, ": ", relayRequest.Url, relayRequest.Retries, err)

				if relayRequest.Retries > 5 {
					// max 5 retries
					fmt.Println("[WORKER] Removed from queue: ", relayRequest.Url)
					break
				}

				// put it back to queue
				queue.RePush(relayRequest)

				// wait 10 sec -> server is down?
				time.Sleep(time.Second * 10)
				break
			} else if debug {
				fmt.Println("[WORKER] SUCCESS send request", relayRequest.Uuid.String(), ": ", relayRequest.Url)
			}
		}

		client.CloseIdleConnections()
		// fmt.Println("🧲 loop")

		// sleep 1 sec
		time.Sleep(time.Second)
	}
}
