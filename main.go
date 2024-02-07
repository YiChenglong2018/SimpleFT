package main

import (
	"github.com/seafooler/yimchain/config"
	"github.com/seafooler/yimchain/core"
	"time"
)

var conf *config.Config
var err error

func init() {
	conf, err = config.LoadConfig("", "config")
	if err != nil {
		panic(err)
	}
}

func main() {
	node := core.NewNode(conf)
	if err = node.StartP2PListen(); err != nil {
		panic(err)
	}
	if err = node.StartRPCListen(); err != nil {
		panic(err)
	}
	// wait for each node to start
	time.Sleep(time.Second * 10)
	if err = node.EstablishP2PConns(); err != nil {
		panic(err)
	}
	if node.CanProposeBlock() {
		go node.ProposeBlockLoop()
	}
	node.HandleMsgsLoop()
}
