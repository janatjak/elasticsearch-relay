package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
)

// https://medium.com/hackernoon/avoiding-memory-leak-in-golang-api-1843ef45fca8
func sendRequest(client *http.Client, baseUrl string, relayRequest *RelayRequest) error {
	// prepare request
	req, err := http.NewRequest(relayRequest.Method, baseUrl+relayRequest.Url, bytes.NewReader(relayRequest.Body))
	if err != nil {
		fmt.Println("FATAL error: ", err)
	}
	req.Header = relayRequest.Headers

	res, err := client.Do(req)
	if res != nil {
		defer res.Body.Close() // avoid memory leak
	}
	if err != nil {
		return err
	}

	// non 2xx -> ignored
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := ioutil.ReadAll(res.Body)
		fmt.Printf("Invalid response: %d %s %s\n", res.StatusCode, relayRequest.Url, body)
	} else {
		_, err = io.Copy(ioutil.Discard, res.Body) // WE READ THE BODY
	}

	return nil
}
