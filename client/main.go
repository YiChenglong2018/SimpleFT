package main

import (
	"fmt"
	"github.com/seafooler/yimchain/core"
	"github.com/urfave/cli"
	"log"
	"net/rpc"
	"os"
	"strconv"
)

type CMD struct {
	rpcAddress string
	rpcPort    int
	reqText    string
}

func (cmd *CMD) sendRequest() {
	client, err := rpc.DialHTTP("tcp", cmd.rpcAddress+":"+strconv.Itoa(cmd.rpcPort))
	if err != nil {
		panic(err)
	}

	req := core.Request{
		Cmd: []byte(cmd.reqText),
	}

	var reply core.Reply

	err = client.Call("RPCHandler.NewRequest", req, &reply)
	if err != nil {
		panic(err)
	} else {
		fmt.Printf("Reply from node0: %v\n", reply)
	}
}

func (cmd *CMD) Run() {
	app := &cli.App{
		Name: "A client to communicate with yimchain node",
	}

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "rpcaddress, a",
			Usage:       "Daemon RPC `ADDRESS` to connect to",
			Required:    false,
			Value:       "127.0.0.1",
			Destination: &cmd.rpcAddress,
		},
		cli.IntFlag{
			Name:        "rpcport, p",
			Usage:       "Daemon RPC `PORT` to connect to",
			Required:    true,
			Destination: &cmd.rpcPort,
		},
	}

	app.Commands = []cli.Command{
		// send request CMD
		{
			Name:        "sendrequest",
			Usage:       "send a request to be executed by yimchain",
			Description: "Send a request",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:        "request, r",
					Required:    true,
					Destination: &cmd.reqText,
				},
			},
			Action: func(c *cli.Context) error {
				cmd.sendRequest()
				return nil
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Panic(err)
	}
}

func main() {
	cmd := new(CMD)
	cmd.Run()
}
