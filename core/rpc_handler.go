package core

import (
	"bytes"
	"sync"
)

// RPCHandler defines a RPC handler to receive the requests from the client.
type RPCHandler struct {
	nodeName string
	nextSN   uint64
	reqPool  *RequestPool
	lock     sync.Mutex
}

// RequestPool includes all the pending requests for block package.
type RequestPool struct {
	pool []RequestWrappedByServer
	sync.Mutex
}

// RequestWrappedByServer wraps the original request with the node name and sn.
type RequestWrappedByServer struct {
	Request
	ServerName string // the server receiving the request
	SN         uint64
}

// Equal compares if two variables of RequestWrappedByServer are equal.
func (rwbs *RequestWrappedByServer) Equal(rwbs2 *RequestWrappedByServer) bool {
	if rwbs.ServerName != rwbs2.ServerName || rwbs.SN != rwbs2.SN {
		return false
	}
	if rwbs.Request.Cmd == nil {
		if rwbs2.Request.Cmd != nil {
			return false
		}
	} else {
		if rwbs2.Request.Cmd == nil {
			return false
		}
	}
	return bytes.Equal(rwbs.Request.Cmd, rwbs2.Request.Cmd)
}

// Request defines the format of requests from clients.
type Request struct {
	Cmd []byte
}

// Reply defines the format of replies to clients.
type Reply struct {
	ServerName string
	SN         uint64
}

// NewRequest creates a new RequestWrappedByServer and append it to the request pool.
func (c *RPCHandler) NewRequest(req *Request, reply *Reply) error {
	c.lock.Lock()
	nextSN := c.nextSN
	c.nextSN++
	c.lock.Unlock()
	wrappedRequest := &RequestWrappedByServer{
		Request:    *req,
		ServerName: c.nodeName,
		SN:         nextSN,
	}

	c.reqPool.Lock()
	c.reqPool.pool = append(c.reqPool.pool, *wrappedRequest)
	c.reqPool.Unlock()

	reply.ServerName = c.nodeName
	reply.SN = nextSN
	//fmt.Printf("A request has been received by node: %s with sn: %d\n", reply.ServerName, reply.SN)
	return nil
}
