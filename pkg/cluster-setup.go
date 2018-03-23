package cluster_config

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ClusterSetup struct {
	Action      string `json:"action"`
	RemoteNode  string `json:"remote_node,omitempty"`
	Host        string `json:"host,omitempty"`
	Port        string `json:"port,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	BindAddress string `json:"bind_address,omitempty"`
	NodeCount   int    `json:"node_count,omitempty"`
}

func CreateAdmin(ipAddresses []string, auth BasicAuth, insecure bool) error {
	for _, ip := range ipAddresses {
		client := NewCouchdbClient(fmt.Sprintf("http://%s:5984", ip), BasicAuth{}, insecure)
		_, err := client.Request(
			"PUT",
			fmt.Sprintf("%s/_node/couchdb@%s/_config/admins/%s", client.BaseUri, ip, auth.Username),
			strings.NewReader(fmt.Sprintf("\"%s\"", auth.Password)))
		// TODO when the admin already exists, then don't fail.
		if err != nil {
			return err
		}
	}
	return nil
}

func CreateCoreDatabases(databaseNames []string, ipAddresses []string, auth BasicAuth, insecure bool) error {
	for _, ip := range ipAddresses {
		client := NewCouchdbClient(fmt.Sprintf("http://%s:5984", ip), auth, insecure)
		for _, dbName := range databaseNames {
			_, err := client.Request("PUT", fmt.Sprintf("%s/%s", client.BaseUri, dbName), nil)
			// TODO when the database already exists, then don't fail.
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func SetupClusterNodes(ipAddresses []string, adminAuth BasicAuth, insecure bool) error {
	hosts := make([]string, len(ipAddresses))
	for i, ip := range ipAddresses {
		hosts[i] = fmt.Sprintf("%s:5984", ip)
	}
	err := AwaitNodes(hosts, Available)
	if err != nil {
		return err
	}

	err = CreateAdmin(ipAddresses, adminAuth, insecure)
	if err != nil {
		return err
	}

	err = CreateCoreDatabases([]string{"_users", "_replicator"}, ipAddresses, adminAuth, insecure)
	if err != nil {
		return err
	}

	setupNodeIp := ipAddresses[:1]
	otherNodeIps := ipAddresses[1:]
	nodeCount := len(ipAddresses)

	client := NewCouchdbClient(fmt.Sprintf("http://%s:5984", setupNodeIp), adminAuth, insecure)

	body, err := json.Marshal(ClusterSetup{
		Action:      "enable_cluster",
		Username:    adminAuth.Username,
		Password:    adminAuth.Password,
		BindAddress: "0.0.0.0",
		NodeCount:   nodeCount})
	if err != nil {
		return err
	}
	client.Request("POST", fmt.Sprintf("http://%s:5984/_cluster_setup", setupNodeIp), strings.NewReader(string(body)))

	for _, ip := range otherNodeIps {
		body, err = json.Marshal(ClusterSetup{
			Action:      "enable_cluster",
			RemoteNode:  ip,
			Port:        "5984",
			Username:    adminAuth.Username,
			Password:    adminAuth.Password,
			BindAddress: "0.0.0.0",
			NodeCount:   nodeCount})
		if err != nil {
			return err
		}
		client.Request("POST", fmt.Sprintf("http://%s:5984/_cluster_setup", setupNodeIp), strings.NewReader(string(body)))

		body, err = json.Marshal(ClusterSetup{
			Action:   "add_node",
			Host:     ip,
			Port:     "5984",
			Username: adminAuth.Username,
			Password: adminAuth.Password})
		if err != nil {
			return err
		}
		client.Request("POST", fmt.Sprintf("http://%s:5984/_cluster_setup", setupNodeIp), strings.NewReader(string(body)))
	}

	body, err = json.Marshal(ClusterSetup{
		Action: "finish_cluster"})
	if err != nil {
		return err
	}
	client.Request("POST", fmt.Sprintf("http://%s:5984/_cluster_setup", setupNodeIp), strings.NewReader(string(body)))

	return nil
}
