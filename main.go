package main

import (
	"context"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/robertlestak/ethindexer/internal/cache"
	"github.com/robertlestak/ethindexer/internal/db"
	"github.com/robertlestak/ethindexer/internal/index"
	"github.com/robertlestak/ethindexer/internal/web3crypto"
	"github.com/robertlestak/ethindexer/internal/work"
	log "github.com/sirupsen/logrus"
)

// server is the main REST server
func server() {
	r := mux.NewRouter()
	r.HandleFunc("/contract/txs", index.HandleGetTransactionsByContract)
	r.HandleFunc("/contract/holders", index.HandleGetCurrentHoldersOfContract)
	r.HandleFunc("/contract/tokens", index.HandleGetTokenIDsByContract)
	r.HandleFunc("/address/received", index.HandleGetTransactionsReceivedByAddress)
	r.HandleFunc("/address/sent", index.HandleGetTransactionsSentFromAddress)
	r.HandleFunc("/txs", index.HandleGetTransactionByID)
	r.Use(cache.Middleware)
	http.ListenAndServe(":"+os.Getenv("PORT"), r)
}

func init() {
	ll := log.InfoLevel
	if os.Getenv("LOG_LEVEL") != "" {
		var err error
		ll, err = log.ParseLevel(os.Getenv("LOG_LEVEL"))
		if err != nil {
			log.Fatal(err)
		}
	}
	log.SetLevel(ll)
	l := log.WithFields(log.Fields{
		"action": "init",
	})
	l.Debug("start")
	cerr := web3crypto.Init()
	if cerr != nil {
		l.WithError(cerr).Fatal("Error initializing crypto")
	}
	l.Debug("db.Init")
	derr := db.Init()
	if derr != nil {
		l.WithError(derr).Fatal("Failed to initialize database")
	}
	l.Debug("init database tables")
	if merr := db.DB.AutoMigrate(&index.ChainState{}); merr != nil {
		l.WithError(merr).Fatal("Failed to migrate ChainState")
	}
	if merr := db.DB.AutoMigrate(&index.Transaction{}); merr != nil {
		l.WithError(merr).Fatal("Failed to migrate Transaction")
	}
	if merr := db.DB.AutoMigrate(&index.ChainStateBackfill{}); merr != nil {
		l.WithError(merr).Fatal("Failed to migrate ChainStateBackfill")
	}
	go db.Healthchecker()
	l.Debug("work.Init")
	werr := work.Init()
	if werr != nil {
		l.WithError(werr).Fatal("Failed to initialize work")
	}
	l.Debug("cache.Init")
	cerr = cache.Init()
	if cerr != nil {
		l.WithError(cerr).Fatal("Failed to initialize cache")
	}
	l.Debug("end")
}

func main() {
	l := log.WithFields(log.Fields{
		"action": "main",
	})
	runMode := "indexer"
	if len(os.Args) > 1 {
		runMode = os.Args[1]
	}
	l.WithFields(
		log.Fields{
			"runMode": runMode,
		},
	).Debug("start")
	switch runMode {
	case "backfill":
		index.BackFillChains(context.Background(), web3crypto.Clients, runMode)
	case "indexer":
		index.IndexChains(context.Background(), web3crypto.Clients, runMode)
	case "worker":
		index.IndexChains(context.Background(), web3crypto.Clients, runMode)
	case "server":
		server()
	default:
		l.Fatal("Invalid run mode")
	}
	l.Debug("end")
}
