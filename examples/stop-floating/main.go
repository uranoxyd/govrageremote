package main

import (
	"gopkg.in/uranoxyd/govrageremote.v1"
)

func main() {
	client := govrageremote.NewVRageRemoteClient("http://localhost:8080", "RTOLNUrsQ2ZUW1ZDYqkKwA==")

	response, err := client.GetFloatingObjects()
	if err != nil {
		panic(err)
	}

	for _, floating := range response.Data.FloatingObjects {
		floating.Stop()
	}
}
