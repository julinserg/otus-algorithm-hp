package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/julinserg/OtusAlgorithmHomeProject/internal/app"
	"github.com/julinserg/OtusAlgorithmHomeProject/internal/logger"
	internalhttp "github.com/julinserg/OtusAlgorithmHomeProject/internal/server/http"
	sqlstorage "github.com/julinserg/OtusAlgorithmHomeProject/internal/storage/sql"
)

var configFile string

func init() {
	flag.StringVar(&configFile, "config", "/etc/minisearch/config.toml", "Path to configuration file")
}

/*
 goose -dir migrations postgres "user=sergey password=sergey dbname=minisearch sslmode=disable" up
*/

func main() {
	flag.Parse()

	if flag.Arg(0) == "version" {
		printVersion()
		return
	}

	config := NewConfig()
	err := config.Read(configFile)
	if err != nil {
		log.Println("error read config: " + err.Error())
		return
	}

	f, err := os.OpenFile("minisearch.logfile", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o666)
	if err != nil {
		log.Println("error opening logfile: " + err.Error())
		return
	}
	defer f.Close()

	logg := logger.New(config.Logger.Level, f)

	sqlstor := sqlstorage.New()
	ctxDB, cancelDB := context.WithCancel(context.Background())
	defer cancelDB()
	if err := sqlstor.Connect(ctxDB, config.PSQL.DSN); err != nil {
		logg.Error("cannot connect to psql: " + err.Error())
		return
	}
	defer func() {
		if err := sqlstor.Close(); err != nil {
			logg.Error("cannot close psql connection: " + err.Error())
		}
	}()

	minisearch := app.New(logg, sqlstor)

	endpoint := net.JoinHostPort(config.HTTP.Host, config.HTTP.Port)
	server := internalhttp.NewServer(logg, minisearch, endpoint)

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	go func() {
		<-ctx.Done()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		defer cancel()

		if err := server.Stop(ctx); err != nil {
			logg.Error("failed to stop http server: " + err.Error())
		}
	}()

	logg.Info("minisearch is running...")

	if err := server.Start(ctx); err != nil {
		logg.Error("failed to start http server: " + err.Error())
		cancel()
		os.Exit(1) //nolint:gocritic
	}
}
