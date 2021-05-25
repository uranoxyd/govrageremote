package main

import (
	"fmt"

	"gopkg.in/uranoxyd/govrageremote.v1"
)

func main() {
	client := govrageremote.NewVRageRemoteClient("http://localhost:8080", "RTOLNUrsQ2ZUW1ZDYqkKwA==")

	response, err := client.GetServerInfo()
	if err != nil {
		panic(err)
	}

	fmt.Println("Server is running version:", response.Data.Version)
}
