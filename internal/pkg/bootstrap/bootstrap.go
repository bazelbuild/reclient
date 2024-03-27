// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package bootstrap starts/shuts down the proxy server.
package bootstrap

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ppb "github.com/bazelbuild/reclient/api/proxy"
	spb "github.com/bazelbuild/reclient/api/stats"
	"github.com/bazelbuild/reclient/internal/pkg/event"
	"github.com/bazelbuild/reclient/internal/pkg/ipc"
	"github.com/bazelbuild/reclient/internal/pkg/reproxypid"
	cpb "github.com/bazelbuild/remote-apis-sdks/go/api/command"
	"github.com/bazelbuild/remote-apis-sdks/go/pkg/command"
	"google.golang.org/grpc/connectivity"

	log "github.com/golang/glog"
)

const (
	// Proxyname is the name of the RE proxy process.
	Proxyname = "reproxy"

	// oeFile is the file where stdout and stderr of reproxy are redirected.
	oeFile = "reproxy_outerr.log"

	// The initial dial timeout seconds when checking whether reproxy is alive or not.
	initialDialTimeout = 3
)

// ShutDownProxy sends a Shutdown rpc to stop the reproxy process and waits until
// the process terminates. It will stop waiting if ctx it canceled.
// The serverAddress value is mapped to process ID through PID file created during reproxy start.
// The PID file is always removed by the end of the function.
func ShutDownProxy(serverAddr string, shutdownSeconds int) (*spb.Stats, error) {
	return shutdownReproxy(serverAddr, shutdownSeconds, false)
}

// ShutdownProxyAsync sends a Shutdown rpc to stop the reproxy process and waits until *either*
// valid stats are returned via RPC or the process terminates. It will stop waiting if ctx it canceled.
// The serverAddress value is mapped to process ID through PID file created during reproxy start.
// The PID file is only removed if this function detects the process has terminated before returning.
func ShutdownProxyAsync(serverAddr string, shutdownSeconds int) (*spb.Stats, error) {
	return shutdownReproxy(serverAddr, shutdownSeconds, true)
}

func shutdownReproxy(serverAddr string, shutdownSeconds int, async bool) (*spb.Stats, error) {
	log.Infof("Shutting down %v...", Proxyname)
	pf, err := reproxypid.ReadFile(serverAddr)
	if err != nil {
		return nil, fmt.Errorf("Error reading reproxy pid file: %v", err)
	}
	alive, err := pf.IsAlive()
	if err != nil {
		return nil, fmt.Errorf("Failed to check whether the reproxy process %d is alive: %v", pf.Pid, err)
	} else if !alive {
		pf.Delete()
		return nil, fmt.Errorf("Reproxy is not running with pid=%d; nothing to shutdown", pf.Pid)
	}

	log.Infof("Sending a shutdown request to reproxy")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(shutdownSeconds)*time.Second)
	defer cancel()
	statCh := make(chan *spb.Stats)
	go sendShutdownRPC(ctx, serverAddr, statCh)
	deadCh := make(chan error)
	go pf.PollForDeath(ctx, 50*time.Millisecond, deadCh)
	var earlyStatCh chan *spb.Stats
	if async {
		earlyStatCh = statCh
	}
	select {
	case st := <-earlyStatCh:
		return st, nil
	case deadErr := <-deadCh:
		if deadErr != nil {
			err = fmt.Errorf("Failed to check whether the reproxy process %v is alive: %w", pf.Pid, deadErr)
		}
	case <-ctx.Done():
		err = fmt.Errorf("Reproxy process %v still running after %v seconds. Check the logs and/or consider increasing the timeout: %w", pf.Pid, shutdownSeconds, ctx.Err())
	}
	select {
	// Check for stats proto from RPC one last time
	case st := <-statCh:
		return st, err
	default:
		return nil, err
	}
}

func sendShutdownRPC(ctx context.Context, serverAddr string, statCh chan *spb.Stats) {
	rpcCtx, rpcCancel := context.WithCancel(ctx)
	defer rpcCancel()
	conn, err := ipc.DialContext(rpcCtx, serverAddr)
	if err != nil {
		log.Infof("Reproxy is not responding at %v, assumed to be already shut down", serverAddr)
		return
	}
	proxy := ppb.NewCommandsClient(conn)
	if resp, err := proxy.Shutdown(rpcCtx, &ppb.ShutdownRequest{}); err != nil {
		log.Warningf("Reproxy Shutdown() returned error. This may be caused by it closing connections before responding to Shutdown: %v", err)
	} else {
		log.Infof("Sent a shutdown request to reproxy")
		if resp.Stats != nil {
			statCh <- resp.Stats
		}
	}
	conn.Close()
}

// StartProxy starts the proxy; if the proxy is already running, it is shut down first.
func StartProxy(ctx context.Context, serverAddr, proxyPath string, waitSeconds, shutdownSeconds int, args ...string) error {
	cmd := exec.Command(proxyPath, args...)
	return startProxy(ctx, serverAddr, cmd, waitSeconds, shutdownSeconds, time.Now())
}

// StartProxyWithOutput starts the proxy; if the proxy is already running, it is shut down first.
// Redirects stdout and stderr to file under given output directory.
func StartProxyWithOutput(ctx context.Context, serverAddr, proxyPath, outputDir string, waitSeconds, shutdownSeconds int, startTime time.Time, args ...string) error {
	logFilename := filepath.Join(outputDir, oeFile)
	logFile, err := os.Create(logFilename)
	if err != nil {
		log.Errorf("Failed to create log file %s: %v", logFilename, err)
		return err
	}
	cmd := exec.Command(proxyPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	log.Infof("%v", cmd)
	err = startProxy(ctx, serverAddr, cmd, waitSeconds, shutdownSeconds, startTime)
	if err != nil {
		logFile.Close()
		// Intimate the user about why reproxy didn't start.
		out, ferr := os.ReadFile(logFilename)
		if ferr != nil {
			log.Warningf("Unable to read reproxy start logfile: %v", err)
			return err
		}
		log.Errorf("Unable to start reproxy: %q", string(out))
		return err
	}
	go func() {
		cmd.Wait()
		logFile.Close()
	}()
	return err
}

func startProxy(ctx context.Context, serverAddr string, cmd *exec.Cmd, waitSeconds, shutdownSeconds int, startTime time.Time) (err error) {
	defer func() {
		if err != nil && cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()
	_, shutdownErr := ShutDownProxy(serverAddr, shutdownSeconds)
	if shutdownErr == nil {
		log.Infof("Previous reproxy instance shutdown successfully")
	} else {
		log.Warningf("Error shutting down previous reproxy instance: %v", shutdownErr)
	}
	httpProxy := os.Getenv("RBE_HTTP_PROXY")
	if httpProxy != "" {
		// Pass http_proxy to be used with all grpc connections inside reproxy.
		// However, don't use it for any other connections, in particular not when
		// trying to check reproxy is up, or rewrapper. After Go 1.16, we should
		// pass https_proxy as well. See https://github.com/golang/go/issues/40909
		// for context on the change in behavior.
		cmd.Env = append(os.Environ(), "https_proxy="+httpProxy, "http_proxy="+httpProxy)
	}
	log.Infof("Starting %v on %v...", cmd, serverAddr)
	if err := cmd.Start(); err != nil {
		return err
	}

	// Check if the reproxy failed quickly after starting, if so return the error right away.
	startupErr := make(chan error)
	go func() {
		startupErr <- cmd.Wait()
	}()

	if err := reproxypid.WriteFile(serverAddr, cmd.Process.Pid); err != nil {
		return err
	}

	waitMillis := (time.Second * time.Duration(waitSeconds)).Milliseconds()
	for i := 0; i < int(waitMillis); {
		select {
		case err := <-startupErr:
			if err != nil {
				log.Infof("Encountered startup error: %v", err)
				return err
			}
			continue

		default:
			tctx, _ := context.WithTimeout(ctx, 50*time.Millisecond)
			i += 50
			conn, err := ipc.DialContextWithBlock(tctx, serverAddr)
			if err != nil {
				log.Infof("Reproxy not started yet: %v", err)
				continue
			}
			defer conn.Close()
			if conn.GetState() == connectivity.Ready {
				log.Info("Proxy started successfully.")
				_, err := ppb.NewStatsClient(conn).AddProxyEvents(ctx, &ppb.AddProxyEventsRequest{
					EventTimes: map[string]*cpb.TimeInterval{
						event.BootstrapStartup: command.TimeIntervalToProto(
							&command.TimeInterval{
								From: startTime,
								To:   time.Now(),
							}),
					},
				})
				if err != nil {
					log.Warningf("Error sending startup stats to reproxy: %v", err)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("proxy failed to start within %v seconds", waitSeconds)
}

func pidFilePath(serverAddr string) (string, error) {
	address := ""
	if strings.HasPrefix(serverAddr, "unix://") {
		address = strings.TrimPrefix(serverAddr, "unix://") + ".pid"
	} else {
		_, port, err := net.SplitHostPort(serverAddr)
		if err != nil {
			return "", err
		}
		address = filepath.Join(os.TempDir(), Proxyname+"_"+port+".pid")
		err = os.MkdirAll(filepath.Dir(address), 0755)
		if err != nil {
			return "", fmt.Errorf("failed to create dir for pid file %q: %v", address, err)
		}
	}
	return address, nil
}

func readPidFromFile(fp string) (int, error) {
	contents, err := os.ReadFile(fp)
	if err != nil {
		return 0, fmt.Errorf("failed to read pid file %v: %v", fp, err)
	}
	pid, err := strconv.Atoi(string(contents))
	if err != nil {
		return 0, fmt.Errorf("cannot parse pid from contents of %v (%v): %v", fp, string(contents), err)
	}
	return pid, nil
}
