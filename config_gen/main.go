/*
Package main in the directory config_gen implements a tool to read configuration from a template,
and generate customized configuration files for each node.
The generated configuration file particularly contains the public/private keys for TS and ED25519.
*/
package main

import (
	"encoding/hex"
	"fmt"
	"github.com/seafooler/yimchain/sign"
	"github.com/spf13/viper"
	"sort"
	"strings"
)

func main() {

	viperRead := viper.New()

	// for environment variables
	viperRead.SetEnvPrefix("")
	viperRead.AutomaticEnv()
	replacer := strings.NewReplacer(".", "_")
	viperRead.SetEnvKeyReplacer(replacer)

	viperRead.SetConfigName("config_template")
	viperRead.AddConfigPath("./")

	err := viperRead.ReadInConfig()
	if err != nil {
		panic(err)
	}

	// deal with cluster as a string map
	clusterMapInterface := viperRead.GetStringMap("ips")
	nodeNumber := len(clusterMapInterface)
	clusterMapString := make(map[string]string, nodeNumber)

	clusterName := make([]string, nodeNumber)

	i := 0
	for name, addr := range clusterMapInterface {
		if addrAsString, ok := addr.(string); ok {
			clusterMapString[name] = addrAsString
			clusterName[i] = name
			i++
		} else {
			panic("cluster in the config file cannot be decoded correctly")
		}
	}

	sort.Strings(clusterName)

	// deal with p2p_listen_port as a string map
	p2pPortMapInterface := viperRead.GetStringMap("peers_p2p_port")
	if nodeNumber != len(p2pPortMapInterface) {
		panic("p2p_listen_port does not match with cluster")
	}
	mapNameToP2PPort := make(map[string]int, nodeNumber)
	for name, _ := range clusterMapString {
		portAsInterface, ok := p2pPortMapInterface[name]
		if !ok {
			panic("p2p_listen_port does not match with cluster")
		}
		if portAsInt, ok := portAsInterface.(int); ok {
			mapNameToP2PPort[name] = portAsInt
		} else {
			panic("p2p_listen_port contains a non-int value")
		}
	}

	// create the ED25519 keys
	privKeysED25519 := make(map[string]string, nodeNumber)
	pubKeysED25519 := make(map[string]string, nodeNumber)

	var privKeyED, pubKeyED []byte

	for name, _ := range clusterMapString {
		privKeyED, pubKeyED = sign.GenED25519Keys()
		privKeysED25519[name] = hex.EncodeToString(privKeyED)
		pubKeysED25519[name] = hex.EncodeToString(pubKeyED)
	}

	// create the threshold signature keys
	numT := nodeNumber - nodeNumber/3
	shares, pubPoly := sign.GenTSKeys(numT, nodeNumber)
	expectation := viperRead.GetInt("expectation")
	maxPool := viperRead.GetInt("max_pool")
	rpcListenPort := viperRead.GetInt("rpc_listen_port")
	sortitionEnabled := viperRead.GetBool("sortition_enabled")
	logLevel := viperRead.GetInt("log_level")
	canProposeBlock := viperRead.GetBool("can_propose_block")

	// write to configure files
	for i, name := range clusterName {
		viperWrite := viper.New()
		viperWrite.SetConfigFile(fmt.Sprintf("%s.yaml", name))
		share := shares[i]
		shareAsBytes, err := sign.EncodeTSPartialKey(share)
		if err != nil {
			panic("encode the share")
		}
		tsPubKeyAsBytes, err := sign.EncodeTSPublicKey(pubPoly)
		if err != nil {
			panic("encode the share")
		}
		viperWrite.Set("name", name)
		viperWrite.Set("address", clusterMapString[name])
		viperWrite.Set("expectation", expectation)
		viperWrite.Set("p2p_listen_port", mapNameToP2PPort[name])
		viperWrite.Set("peers_p2p_port", p2pPortMapInterface)
		viperWrite.Set("max_pool", maxPool)
		viperWrite.Set("rpc_listen_port", rpcListenPort)
		viperWrite.Set("PrivKeyED", privKeysED25519[name])
		viperWrite.Set("cluster_pubkeyed", pubKeysED25519)
		viperWrite.Set("TSShare", hex.EncodeToString(shareAsBytes))
		viperWrite.Set("TSPubKey", hex.EncodeToString(tsPubKeyAsBytes))
		viperWrite.Set("sortition_enabled", sortitionEnabled)
		viperWrite.Set("log_level", logLevel)
		viperWrite.Set("can_propose_block", canProposeBlock)
		viperWrite.Set("cluster_ips", clusterMapInterface)
		viperWrite.WriteConfig()
	}
}
