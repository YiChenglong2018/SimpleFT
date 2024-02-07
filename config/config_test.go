package config

import (
	"fmt"
	"testing"
)

func TestConfigRead(t *testing.T) {
	config, err := LoadConfig("", "config")
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("name:", config.Name)
	fmt.Println("address:", config.AddrStr)
	fmt.Println("clusterPort:", config.ClusterPort)
	fmt.Println("clusterAddr:", config.ClusterAddr)
	fmt.Println("max_pool:", config.MaxPool)
	fmt.Println("probability:", config.Probability)
	fmt.Println("P2PListenPort:", config.P2PListenPort)
	fmt.Println("RPCListenPort:", config.RPCListenPort)
	fmt.Println("CanProposeBlock:", config.CanProposeBlock)
}
