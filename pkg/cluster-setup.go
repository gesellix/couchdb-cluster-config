package cluster_config

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type IpAddress string

type IpAddresses []IpAddress

func ToIpAddresses(s []string) IpAddresses {
	c := make(IpAddresses, len(s))
	for i, v := range s {
		c[i] = IpAddress(v)
	}
	return c
}

type ClusterSetupConfig struct {
	IpAddresses IpAddresses
	Delay       time.Duration
	Timeout     time.Duration
}

type ClusterSetup struct {
	Action                string   `json:"action"`
	RemoteNode            string   `json:"remote_node,omitempty"`
	RemoteCurrentUser     string   `json:"remote_current_user,omitempty"`
	RemoteCurrentPassword string   `json:"remote_current_password,omitempty"`
	Host                  string   `json:"host,omitempty"`
	Port                  int      `json:"port,omitempty"`
	Username              string   `json:"username,omitempty"`
	Password              string   `json:"password,omitempty"`
	BindAddress           string   `json:"bind_address,omitempty"`
	NodeCount             int      `json:"node_count,omitempty"`
	EnsureDbsExist        []string `json:"ensure_dbs_exist,omitempty"`
}

type ClusterSetupResponse struct {
	State string `json:"state"`
}

type UuidsResponse struct {
	Uuids []string `json:"uuids"`
}

func AdminExists(ip IpAddress, auth BasicAuth, insecure bool) (bool, error) {
	client := NewCouchdbClient(fmt.Sprintf("http://%s:5984", ip), BasicAuth{}, insecure)
	resp, err := client.Request(
		"POST",
		fmt.Sprintf("%s/_session", client.BaseUri),
		strings.NewReader(fmt.Sprintf("{\"name\":\"%s\",\"password\":\"%s\"}", auth.Username, auth.Password)))
	if err != nil {
		return false, err
	}
	return resp.StatusCode == 200, nil
}

func CreateAdmin(ipAddresses IpAddresses, auth BasicAuth, insecure bool) error {
	for _, ip := range ipAddresses {
		if ok, err := AdminExists(ip, auth, insecure); !ok {
			if err != nil {
				return err
			}
			client := NewCouchdbClient(fmt.Sprintf("http://%s:5984", ip), BasicAuth{}, insecure)
			_, err = client.Request(
				"PUT",
				fmt.Sprintf("%s/_node/couchdb@%s/_config/admins/%s", client.BaseUri, ip, auth.Username),
				strings.NewReader(fmt.Sprintf("\"%s\"", auth.Password)))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func DatabaseExists(dbName string, ip IpAddress, auth BasicAuth, insecure bool) (bool, error) {
	client := NewCouchdbClient(fmt.Sprintf("http://%s:5984", ip), auth, insecure)
	resp, err := client.Request(
		"GET",
		fmt.Sprintf("%s/%s", client.BaseUri, dbName),
		nil)
	if err != nil {
		return false, err
	}
	return resp.StatusCode == 200, nil
}

func CreateCoreDatabases(databaseNames []string, ipAddresses IpAddresses, auth BasicAuth, insecure bool) error {
	for _, ip := range ipAddresses {
		client := NewCouchdbClient(fmt.Sprintf("http://%s:5984", ip), auth, insecure)
		for _, dbName := range databaseNames {
			if ok, err := DatabaseExists(dbName, ip, auth, insecure); !ok {
				if err != nil {
					return err
				}
				_, err := client.Request(
					"PUT",
					fmt.Sprintf("%s/%s", client.BaseUri, dbName),
					nil)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func SetupClusterNodes(config ClusterSetupConfig, adminAuth BasicAuth, insecure bool) error {
	hosts := make([]string, len(config.IpAddresses))
	for i, ip := range config.IpAddresses {
		hosts[i] = fmt.Sprintf("%s:5984", ip)
	}
	err := AwaitNodes(hosts, config.Delay, config.Timeout, Available)
	if err != nil {
		return err
	}

	err = CreateAdmin(config.IpAddresses, adminAuth, insecure)
	if err != nil {
		return err
	}

	setupNodeIp := config.IpAddresses[:1][0]
	otherNodeIps := config.IpAddresses[1:]
	nodeCount := len(config.IpAddresses)

	client := NewCouchdbClient(fmt.Sprintf("http://%s:5984", setupNodeIp), adminAuth, insecure)

	resp, err := client.Request("GET",
		fmt.Sprintf("http://%s:5984/_uuids?count=2", setupNodeIp),
		nil)
	if err != nil {
		return err
	}
	var uuids UuidsResponse
	if resp.StatusCode == 200 {
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		err = json.Unmarshal(respBody, &uuids)
		if err != nil {
			return err
		}
	}

	// Ensure that the coordinating node has a valid uuid.
	// Relates to https://github.com/apache/couchdb/issues/2858
	_, err = client.Request(
		"PUT",
		fmt.Sprintf("http://%s:5984/_node/_local/_config/couchdb/uuid", setupNodeIp),
		strings.NewReader(fmt.Sprintf("\"%s\"", uuids.Uuids[:1][0])))
	if err != nil {
		return err
	}

	clusterEnabled := false
	resp, err = client.Request("GET",
		fmt.Sprintf("http://%s:5984/_cluster_setup", setupNodeIp),
		nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == 200 {
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		var clusterSetupResponse ClusterSetupResponse
		err = json.Unmarshal(respBody, &clusterSetupResponse)
		if err != nil {
			return err
		}
		if clusterSetupResponse.State == "cluster_finished" {
			// cluster already set up
			fmt.Println("cluster setup already finished.")
			return nil
		}
		if clusterSetupResponse.State == "cluster_enabled" {
			clusterEnabled = true
		}
	}

	var body []byte
	if !clusterEnabled {
		body, err = json.Marshal(ClusterSetup{
			Action:      "enable_cluster",
			Username:    adminAuth.Username,
			Password:    adminAuth.Password,
			BindAddress: "0.0.0.0",
			Port:        5984,
			NodeCount:   nodeCount})
		if err != nil {
			return err
		}
		_, err = client.Request(
			"POST",
			fmt.Sprintf("http://%s:5984/_cluster_setup", setupNodeIp),
			strings.NewReader(string(body)))
		if err != nil {
			return err
		}
	}

	for _, ip := range otherNodeIps {
		body, err = json.Marshal(ClusterSetup{
			Action:                "enable_cluster",
			RemoteNode:            string(ip),
			Port:                  5984,
			RemoteCurrentUser:     adminAuth.Username,
			RemoteCurrentPassword: adminAuth.Password,
			Username:              adminAuth.Username,
			Password:              adminAuth.Password,
			BindAddress:           "0.0.0.0",
			NodeCount:             nodeCount})
		if err != nil {
			return err
		}
		_, err = client.Request(
			"POST",
			fmt.Sprintf("http://%s:5984/_cluster_setup", setupNodeIp),
			strings.NewReader(string(body)))
		if err != nil {
			return err
		}

		body, err = json.Marshal(ClusterSetup{
			Action:   "add_node",
			Host:     string(ip),
			Port:     5984,
			Username: adminAuth.Username,
			Password: adminAuth.Password})
		if err != nil {
			return err
		}
		_, err = client.Request(
			"POST",
			fmt.Sprintf("http://%s:5984/_cluster_setup", setupNodeIp),
			strings.NewReader(string(body)))
		if err != nil {
			return err
		}
	}

	body, err = json.Marshal(ClusterSetup{
		Action: "finish_cluster"})
	if err != nil {
		return err
	}
	res, err := client.RequestBody(
		"POST",
		fmt.Sprintf("http://%s:5984/_cluster_setup", setupNodeIp),
		strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	fmt.Println(fmt.Sprintf("finished cluster setup: %+v", string(res)))

	// Those databases should be created during the cluster setup (`ensure_dbs_exist` defaults to [_users, _replicator]).
	//err = CreateCoreDatabases([]string{"_users", "_replicator"}, config.IpAddresses, adminAuth, insecure)
	//if err != nil {
	//	return err
	//}

	return nil
}
