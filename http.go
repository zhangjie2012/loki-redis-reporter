package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
)

func httpGetStatusCode(url string) (int, error) {
	resp, err := http.Get(url)
	if err != nil {
		return -1, err
	}
	return resp.StatusCode, nil
}

func httpPost(url string, bs []byte) error {
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(bs))
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("code = %s, body = %s, ", resp.Status, string(body))
	}

	return nil
}
