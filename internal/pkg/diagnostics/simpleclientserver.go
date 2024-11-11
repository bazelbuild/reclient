package diagnostics

import (
	"context"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"strings"

	log "github.com/golang/glog"
)

// HelloService is a simple service to provide a HelloWorld RPC.
type HelloService struct{}

// HelloRequest identifies the hello world request.
type HelloRequest struct{}

// HelloResponse identifies the hello world response.
type HelloResponse struct {
	Message string
}

// HelloWorld is the RPC method that returns a hello world response.
func (s *HelloService) HelloWorld(req *HelloRequest, res *HelloResponse) error {
	res.Message = "Hello, world!"
	return nil
}

// CheckUDSAddrWorks checks if we are able to start a basic RPC
// server at the given UDS address. Useful to diagnost scandeps_server
// startup timeout issues.
func CheckUDSAddrWorks(ctx context.Context, addr string) error {
	if strings.HasPrefix(addr, "unix://") {
		addr = strings.ReplaceAll(addr, "unix://", "")
	} else if strings.HasPrefix(addr, "unix:") {
		addr = strings.ReplaceAll(addr, "unix:", "")
	} else {
		return fmt.Errorf("addr must begin with a unix:// or unix: prefix: %v", addr)
	}
	os.RemoveAll(addr)

	serverStarted := make(chan bool, 1)
	clientComplete := make(chan bool, 1)
	errs := make(chan error, 1)
	var listener net.Listener

	go func() {
		helloService := new(HelloService)
		err := rpc.Register(helloService)
		if err != nil {
			errs <- fmt.Errorf("error registering service: %v", err)
			return
		}

		listener, err = net.Listen("unix", addr)
		if err != nil {
			errs <- fmt.Errorf("listener error: %v", err)
			return
		}

		log.Infof("Diagnostic server listening on UNIX socket: %v", addr)
		serverStarted <- true
		rpc.Accept(listener)
	}()
	select {
	case <-ctx.Done():
		return fmt.Errorf("context timeout reached in CheckUDSAddrWorks(%v)", addr)

	case <-serverStarted:
		break

	case err := <-errs:
		return fmt.Errorf("failed to start RPC server at %v: %v", addr, err)
	}

	errs = make(chan error, 1)
	go func() {
		defer func() {
			clientComplete <- true
		}()
		client, err := rpc.Dial("unix", addr)
		if err != nil {
			errs <- fmt.Errorf("error connecting to server: %v", err)
			return
		}
		defer client.Close()

		req := &HelloRequest{}
		res := &HelloResponse{}

		err = client.Call("HelloService.HelloWorld", req, res)
		if err != nil {
			errs <- fmt.Errorf("error calling HelloWorld: %v", err)
		}
	}()
	log.V(1).Infof("DIAGNOSTICS: Waiting for client-server communication on address %v", addr)
	select {
	case <-ctx.Done():
		return fmt.Errorf("context timeout reached in CheckUDSAddrWorks(%v)", addr)

	case <-clientComplete:
		break

	case err := <-errs:
		return fmt.Errorf("failed to talk to RPC server at %v: %v", addr, err)
	}
	listener.Close()

	return nil
}
