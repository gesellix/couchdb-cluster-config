package cluster_config

import (
	"errors"
	"fmt"
	"net"
	"time"
)

func AwaitNodes(ipAddresses []string) error {
	resc, errc := make(chan bool), make(chan error)

	for _, ip := range ipAddresses {
		go func(ip string) {
			success, err := awaitNode(ip)
			if err != nil {
				errc <- err
				return
			}
			resc <- success
		}(ip)
	}

	for i := 0; i < len(ipAddresses); i++ {
		select {
		case res := <-resc:
			fmt.Println(res)
		case err := <-errc:
			//fmt.Println(err)
			return err
		}
	}
	return nil
}

func awaitNode(ip string) (bool, error) {
	timeout := time.After(10 * time.Second)
	tick := time.Tick(500 * time.Millisecond)
	for {
		select {
		case <-timeout:
			fmt.Println("timeout")
			return false, errors.New(fmt.Sprintf("timed out @%s", ip))
		case <-tick:
			fmt.Println(fmt.Sprintf("tick@%s", ip))

			ok, err := fetch(ip)
			if err != nil {
				return false, err
			} else if ok {
				return true, nil
			}
		}
	}
}

func fetch(ip string) (bool, error) {
	ipAndPort := fmt.Sprintf("%s:5984", ip)
	conn, err := net.DialTimeout("tcp", ipAndPort, time.Second)
	if err != nil {
		if err, ok := err.(net.Error); ok && err.Timeout() {
			return false, nil
		}
		return false, err
	}
	defer conn.Close()
	return true, nil
}
