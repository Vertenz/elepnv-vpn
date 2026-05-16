// Command fakexray is a test helper that mimics the subset of the xray-core
// CLI the elepn daemon invokes. Built from acceptance tests; never shipped.
// See spec §16.2 in docs/superpowers/specs/.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type scaffold struct {
	TestExit      int    `json:"test_exit"`
	TestStderr    string `json:"test_stderr"`
	RunSocksPort  int    `json:"run_socks_port"`
	RunDieAfterMs int    `json:"run_die_after_ms"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fakexray version|run [...]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		// Match the format xray-core actually emits closely enough that
		// platform.Discover's version-extracting regex (if any) doesn't
		// misfire. The literal "Xray" prefix is the important bit.
		fmt.Println("Xray 25.0.0 (fakexray)")
		return
	case "run":
		cfgPath, isTest := parseRunArgs(os.Args[2:])
		sc := readScaffold(cfgPath)
		if isTest {
			if sc.TestStderr != "" {
				fmt.Fprint(os.Stderr, sc.TestStderr)
			}
			os.Exit(sc.TestExit)
		}
		// Live mode.
		if sc.RunSocksPort > 0 {
			go acceptSocks(sc.RunSocksPort)
		}
		if sc.RunDieAfterMs > 0 {
			go func() {
				time.Sleep(time.Duration(sc.RunDieAfterMs) * time.Millisecond)
				os.Exit(1)
			}()
		}
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
		<-ch
		return
	default:
		fmt.Fprintf(os.Stderr, "fakexray: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func parseRunArgs(args []string) (cfg string, isTest bool) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-test":
			isTest = true
		case "-c":
			if i+1 < len(args) {
				cfg = args[i+1]
				i++
			}
		}
	}
	return
}

func readScaffold(path string) scaffold {
	if path == "" {
		return scaffold{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return scaffold{}
	}
	var wrapper struct {
		FakeXray scaffold `json:"fakexray"`
	}
	_ = json.Unmarshal(data, &wrapper)
	return wrapper.FakeXray
}

func acceptSocks(port int) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 3)
			_, _ = c.Read(buf)
			_, _ = c.Write([]byte{0x05, 0x00})
		}(c)
	}
}
