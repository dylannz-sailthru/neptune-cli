package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/sirupsen/logrus"
)

func readline() []byte {
	buf := bufio.NewReader(os.Stdin)
	fmt.Print("> ")
	b, err := buf.ReadBytes('\n')
	if err != nil {
		logrus.Fatal(err)
	}

	return b
}

type GremlinRequest struct {
	Gremlin string `json:"gremlin"`
}

func NewGremlinRequest(b []byte) GremlinRequest {
	return GremlinRequest{
		Gremlin: string(b),
	}
}

func Start(port string) {
	StartGremlin(port)
}

func StartGremlin(port string) {
	fmt.Println("CTRL+C to exit")
	for true {
		b := readline()
		requestBody, err := json.Marshal(NewGremlinRequest(b))
		if err != nil {
			logrus.Fatal(err)
		}

		u := "http://localhost:" + port + "/gremlin"
		req, err := http.NewRequest(http.MethodPost, u, bytes.NewBuffer(requestBody))
		if err != nil {
			logrus.Fatal(err)
		}

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			logrus.Fatal(err)
		}

		responseBytes, err := ioutil.ReadAll(res.Body)
		if err != nil {
			logrus.Fatal(err)
		}

		fmt.Println("< " + string(responseBytes))
	}
}
