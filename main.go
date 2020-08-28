package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	v4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/dylannz-sailthru/neptune-cli/cli"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

func main() {
	err := godotenv.Load(".env.default")
	if err != nil {
		logrus.Fatal(err)
	}
	err = godotenv.Overload(".env")
	if err != nil {
		logrus.Warn(err)
	}

	awsSession := session.Must(session.NewSession())
	signingClient := &Signer{
		client: http.DefaultClient,
		creds:  awsSession.Config.Credentials,
		signer: v4.NewSigner(awsSession.Config.Credentials),
		region: *awsSession.Config.Region,
	}

	envs := strings.Split(os.Getenv("AWS_NEPTUNE_PROXY_ENVIRONMENTS"), ",")
	wg := sync.WaitGroup{}
	for k, v := range envs {
		if v == "" {
			logrus.Fatal("Empty environment name, has AWS_NEPTUNE_PROXY_ENVIRONMENTS been configured correctly?")
		}

		listenPort := os.Getenv(fmt.Sprintf("AWS_NEPTUNE_PROXY_%s_LISTEN_PORT", v))

		if listenPort == "" {
			logrus.Fatalf("Listen port not configured for environment %d (%s)", k, v)
		}
		_, err := strconv.Atoi(listenPort)
		if err != nil {
			logrus.Fatalf("Listen port not a number for environment %d (%s)", k, v)
		}

		host := os.Getenv(fmt.Sprintf("AWS_NEPTUNE_PROXY_%s_HOST", v))
		if host == "" {
			logrus.Fatalf("Host not configured for environment %d (%s)", k, v)
		}

		director := &Director{
			Env:        v,
			TargetHost: host,
		}

		proxy := &httputil.ReverseProxy{
			Director: director.Director,
			Transport: &RoundTripper{
				Env:    v,
				Signer: signingClient,
			},
		}

		logrus.Info("listening on ", listenPort)

		wg.Add(1)
		go func() {
			defer wg.Done()
			m := http.NewServeMux()
			m.HandleFunc("/", proxy.ServeHTTP)
			listen := ":" + listenPort
			logrus.Fatal(http.ListenAndServe(listen, m))
		}()
	}

	go func() {
		wg.Wait()
		logrus.Info("Proxy has stopped")
	}()

	// TODO: currently it starts an interactive CLI for whichever environment is first
	// in the list. Would be good to make this configurable.
	for _, v := range envs {
		listenPort := os.Getenv(fmt.Sprintf("AWS_NEPTUNE_PROXY_%s_LISTEN_PORT", v))
		cli.Start(listenPort)
		break
	}
}

type Director struct {
	Env        string
	ProxyHost  string
	TargetHost string
}

func (d Director) Director(req *http.Request) {
	logrus.Infof("[%s] %s https://%s%s", d.Env, req.Method, d.TargetHost, req.URL.String())
	req.Host = d.TargetHost
	req.URL.Host = d.TargetHost
}

type RoundTripper struct {
	Env    string
	Signer *Signer
}

func (rt *RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Del("X-Forwarded-For")
	rt.Signer.SignRequest(req)
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		logrus.Errorf("[%s] %s: %s", rt.Env, req.URL.String(), err.Error())
		return nil, err
	}
	msg := fmt.Sprintf("[%s] %s %s", rt.Env, res.Status, req.URL.String())
	if res.StatusCode >= 400 {
		logrus.Error(msg)
	} else {
		logrus.Info(msg)
	}
	return res, err
}

type Signer struct {
	client *http.Client
	creds  *credentials.Credentials
	signer *v4.Signer
	region string
}

func (st Signer) SignRequest(req *http.Request) {
	req.URL.Scheme = "https"
	if strings.Contains(req.URL.RawPath, "%2C") {
		// Escaping path
		req.URL.RawPath = url.PathEscape(req.URL.RawPath)
	}
	now := time.Now().UTC()
	req.Header.Set("Date", now.Format(time.RFC3339))
	var err error
	switch req.Body {
	case nil:
		_, err = st.signer.Sign(req, nil, "neptune-db", st.region, now)
	default:
		buf, err := ioutil.ReadAll(req.Body)
		if err != nil {
			logrus.Error(err)
			return
		}
		req.Body = ioutil.NopCloser(bytes.NewReader(buf))
		_, err = st.signer.Sign(req, bytes.NewReader(buf), "neptune-db", st.region, time.Now().UTC())
	}
	if err != nil {
		logrus.Error(err)
	}
}
