# GoVRageRemote

Implements the Space Engineers Remote API protocol. https://spaceengineers.fandom.com/wiki/Setting_up_a_Space_Engineers_Dedicated_Server#Remote_API

## Installation

```
go get gopkg.in/uranoxyd/govrageremote.v1
```

## Usage Example

```go
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
```

## License

GNU GPL
