package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strings"
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

func RunWorker(queue *Queue, baseUrl string, victoriaLogsUrl string, debug bool) {
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
			} else {
				if debug {
					fmt.Println("[WORKER] SUCCESS send request", relayRequest.Uuid.String(), ": ", relayRequest.Url)
				}

				if victoriaLogsUrl != "" {
					if kind := victoriaLogsKind(relayRequest); kind != vlKindNone {
						errVl := sendToVictoriaLogs(client, victoriaLogsUrl, kind, relayRequest)
						if errVl != nil {
							fmt.Println("[WORKER] ERROR send request to VictoriaLogs", relayRequest.Uuid, ": ", relayRequest.Url, errVl)
						} else if debug {
							fmt.Println("[WORKER] SUCCESS send request to VictoriaLogs", relayRequest.Uuid.String(), ": ", relayRequest.Url)
						}
					}
				}
			}
		}

		client.CloseIdleConnections()
		// fmt.Println("🧲 loop")

		// sleep 1 sec
		time.Sleep(time.Second)
	}
}

const (
	vlKindNone = ""
	vlKindBulk = "bulk"
	vlKindDoc  = "doc"
)

func victoriaLogsKind(relayRequest *RelayRequest) string {
	if relayRequest.Method != "POST" {
		return vlKindNone
	}
	parts := splitRequestPath(relayRequest.Url)
	// /_bulk or /<index>/_bulk
	if len(parts) <= 2 && parts[len(parts)-1] == "_bulk" {
		return vlKindBulk
	}
	// /<index>/_doc/<id>
	if len(parts) == 3 && parts[1] == "_doc" {
		return vlKindDoc
	}
	return vlKindNone
}

// RequestURI might have query params.
func splitRequestPath(url string) []string {
	if idx := strings.IndexAny(url, "?#"); idx != -1 {
		url = url[:idx]
	}
	return strings.Split(strings.Trim(url, "/"), "/")
}

func buildVictoriaLogsBody(kind string, relayRequest *RelayRequest) ([]byte, error) {
	if kind == vlKindBulk {
		// bulk body is already in the NDJSON format VictoriaLogs accepts
		return relayRequest.Body, nil
	}

	// single _doc insert -> wrap into a one-item bulk payload
	if len(relayRequest.Headers["Content-Encoding"]) > 0 {
		return nil, fmt.Errorf("cannot transform _doc request with Content-Encoding")
	}

	index := splitRequestPath(relayRequest.Url)[0]

	var doc json.RawMessage
	if err := json.Unmarshal(relayRequest.Body, &doc); err != nil {
		return nil, fmt.Errorf("invalid document body: %w", err)
	}
	docJson, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}

	action, err := json.Marshal(map[string]map[string]string{
		"create": {"_index": index},
	})
	if err != nil {
		return nil, err
	}

	var body bytes.Buffer
	body.Write(action)
	body.WriteByte('\n')
	body.Write(docJson)
	body.WriteByte('\n')
	return body.Bytes(), nil
}

func sendToVictoriaLogs(client *http.Client, victoriaLogsUrl string, kind string, relayRequest *RelayRequest) error {
	body, err := buildVictoriaLogsBody(kind, relayRequest)
	if err != nil {
		return err
	}

	url := strings.TrimRight(victoriaLogsUrl, "/") + "/insert/elasticsearch/_bulk"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	// Authorization is intentionally not copied from the original request:
	// the http client fills in basic auth from the userinfo in victoriaLogsUrl.
	req.Header.Set("Content-Type", "application/json")
	if kind == vlKindBulk {
		if ce := relayRequest.Headers["Content-Encoding"]; len(ce) > 0 {
			req.Header["Content-Encoding"] = ce
		}
	}

	res, err := client.Do(req)
	if res != nil {
		defer res.Body.Close()
	}
	if err != nil {
		return err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		resBody, _ := io.ReadAll(res.Body)
		return fmt.Errorf("invalid response: %d %s", res.StatusCode, resBody)
	}

	_, _ = io.Copy(io.Discard, res.Body)
	return nil
}
