package client

import (
	"encoding/json"
	"fmt"
	"github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/rancher/rancher-volume/api"
	"github.com/rancher/rancher-volume/util"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	addr      string
	scheme    string
	transport *http.Transport
}

var (
	log             = logrus.WithFields(logrus.Fields{"pkg": "client"})
	sockFile string = "/var/run/rancher/volume.sock"

	client Client
)

func (c *Client) call(method, path string, data interface{}, headers map[string][]string) (io.ReadCloser, int, error) {
	params, err := util.EncodeData(data)
	if err != nil {
		return nil, -1, err
	}

	if data != nil {
		if headers == nil {
			headers = make(map[string][]string)
		}
		headers["Context-Type"] = []string{"application/json"}
	}

	body, _, statusCode, err := c.clientRequest(method, path, params, headers)

	return body, statusCode, err
}

func (c *Client) HTTPClient() *http.Client {
	return &http.Client{Transport: c.transport}
}

func getRequestPath(path string) string {
	return fmt.Sprintf("/v1%s", path)
}

func (c *Client) clientRequest(method, path string, in io.Reader, headers map[string][]string) (io.ReadCloser, string, int, error) {
	req, err := http.NewRequest(method, getRequestPath(path), in)
	if err != nil {
		return nil, "", -1, err
	}
	req.Header.Set("User-Agent", "Rancher-Volume-Client/"+api.API_VERSION)
	req.URL.Host = c.addr
	req.URL.Scheme = c.scheme

	resp, err := c.HTTPClient().Do(req)
	statusCode := -1
	if resp != nil {
		statusCode = resp.StatusCode
	}
	if err != nil {
		return nil, "", statusCode, err
	}
	if statusCode < 200 || statusCode >= 400 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, "", statusCode, err
		}
		if len(body) == 0 {
			return nil, "", statusCode, fmt.Errorf("Incompatable version")
		}
		return nil, "", statusCode, fmt.Errorf("Error response from server, %v", string(body))
	}
	return resp.Body, resp.Header.Get("Context-Type"), statusCode, nil
}

func sendRequest(method, request string, data interface{}) (io.ReadCloser, error) {
	log.Debugf("Sending request %v %v", method, request)
	if data != nil {
		log.Debugf("With data %+v", data)
	}
	rc, _, err := client.call(method, request, data, nil)
	if err != nil {
		return nil, err
	}
	return rc, nil
}

func sendRequestAndPrint(method, request string, data interface{}) error {
	rc, err := sendRequest(method, request, data)
	if err != nil {
		return err
	}
	defer rc.Close()

	b, err := ioutil.ReadAll(rc)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func cmdNotFound(c *cli.Context, command string) {
	panic(fmt.Errorf("Unrecognized command", command))
}

func NewCli(version string) *cli.App {
	app := cli.NewApp()
	app.Name = "rancher-volume"
	app.Version = version
	app.Author = "Sheng Yang <sheng.yang@rancher.com>"
	app.Usage = "A volume manager capable of snapshot and delta backup"
	app.CommandNotFound = cmdNotFound
	app.Commands = []cli.Command{
		serverCmd,
		infoCmd,
		volumeCreateCmd,
		volumeDeleteCmd,
		volumeMountCmd,
		volumeUmountCmd,
		volumeListCmd,
		volumeInspectCmd,
		snapshotCmd,
		backupCmd,
	}
	return app
}

func InitClient() {
	client.addr = sockFile
	client.scheme = "http"
	client.transport = &http.Transport{
		DisableCompression: true,
		Dial: func(_, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", sockFile, 10*time.Second)
		},
	}
}

func getOrRequestUUID(c *cli.Context, key string, required bool) (string, error) {
	var err error
	var id string
	if key == "" {
		id = c.Args().First()
	} else {
		id, err = util.GetLowerCaseFlag(c, key, required, err)
		if err != nil {
			return "", err
		}
	}
	if id == "" && !required {
		return "", nil
	}

	if util.ValidateUUID(id) {
		return id, nil
	}

	return requestUUID(id)
}

func requestUUID(id string) (string, error) {
	// Identify by name
	v := url.Values{}
	v.Set(api.KEY_NAME, id)

	request := "/uuid?" + v.Encode()
	rc, err := sendRequest("GET", request, nil)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	resp := &api.UUIDResponse{}
	if err := json.NewDecoder(rc).Decode(resp); err != nil {
		return "", err
	}
	if resp.UUID == "" {
		return "", fmt.Errorf("Cannot find volume with name or id %v", id)
	}
	return resp.UUID, nil
}