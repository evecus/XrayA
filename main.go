package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/xraya/xraya/internal/api"
	"github.com/xraya/xraya/internal/core"
)

//go:embed web
var webFS embed.FS

func main() {
	var (
		dirFlag  string
		portFlag int
	)
	flag.StringVar(&dirFlag, "d", "", "data directory (default: <exe-dir>/data)")
	flag.StringVar(&dirFlag, "datadir", "", "data directory (default: <exe-dir>/data)")
	flag.IntVar(&portFlag, "p", 2017, "web UI port")
	flag.IntVar(&portFlag, "port", 2017, "web UI port")
	flag.Parse()

	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("executable path: %v", err)
	}

	dataDir := filepath.Join(filepath.Dir(exe), "data")
	if dirFlag != "" {
		abs, err := filepath.Abs(dirFlag)
		if err != nil {
			log.Fatalf("invalid -d: %v", err)
		}
		dataDir = abs
	}

	for _, d := range []string{
		dataDir,
		filepath.Join(dataDir, "run"),
	} {
		if err := os.MkdirAll(d, 0750); err != nil {
			log.Fatalf("mkdir %s: %v", d, err)
		}
	}

	listen := fmt.Sprintf("0.0.0.0:%d", portFlag)

	manager, err := core.NewManager(dataDir)
	if err != nil {
		log.Fatalf("init manager: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		sig := <-sigCh
		log.Printf("xraya: signal %v — shutting down", sig)
		manager.Stop()
		os.Exit(0)
	}()

	manager.AutoStart()

	srv := api.NewServer(manager, webFS)
	log.Printf("xraya: listening on %s  data=%s", listen, dataDir)
	if err := srv.Run(listen); err != nil {
		log.Fatalf("server: %v", err)
	}
}
