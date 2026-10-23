package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/telesma-app/windows-proxy/internal/infra/transport"
	"github.com/telesma-app/windows-proxy/pkg/proxy"

	"golang.org/x/sys/windows/svc"
)

func main() {
	var svcDebugMode, transportDebugMode bool
	flag.BoolVar(&svcDebugMode, "svcDebug", false, "Service debug mode")
	flag.BoolVar(&transportDebugMode, "transportDebug", false, "Transport debug mode")
	flag.Parse()

	level := slog.LevelInfo
	if svcDebugMode {
		level = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))

	prx := proxy.New(proxy.WithLogger(logger))
	delivery := transport.NewDelivery(logger, &transport.Config{
		Address:                 "localhost:44080",
		Debug:                   transportDebugMode,
		AllowAuthenticatedUsers: true,
	}, prx)

	prog := &program{
		logger:   logger,
		delivery: delivery,
	}

	inService, err := svc.IsWindowsService()
	if err != nil {
		logger.Error("Failed to determine if we are running in service", "err", err)
		os.Exit(1)
	}

	prog.run(svcName, !inService)
}
