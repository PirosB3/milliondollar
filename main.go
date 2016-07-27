package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
        "time"

        "github.com/btcsuite/btcd/wire"
        "github.com/btcsuite/btcrpcclient"
	"github.com/btcsuite/btcutil"
)

func main() {

	ntfnHandlers := btcrpcclient.NotificationHandlers{
		OnBlockConnected: func(hash *wire.ShaHash, height int32, time time.Time) {
			log.Printf("Block connected: %v (%d) %v", hash, height, time)
		},
		OnBlockDisconnected: func(hash *wire.ShaHash, height int32, time time.Time) {
			log.Printf("Block disconnected: %v (%d) %v", hash, height, time)
		},
	}

    	btcdHomeDir := btcutil.AppDataDir("btcd", false)
	certs, err := ioutil.ReadFile(filepath.Join(btcdHomeDir, "rpc.cert"))
	if err != nil {
		log.Fatal(err)
	}

        connCfg := &btcrpcclient.ConnConfig{
		Host:         "localhost:8334",
		Endpoint:     "ws",
		User:         "admin",
		Pass:         "admin",
		Certificates: certs,
	}
	client, err := btcrpcclient.New(connCfg, &ntfnHandlers)
	if err != nil {
		log.Fatal(err)
	}
        fmt.Println(client)

        for {
            <- time.After(2 * time.Second)
            blockCount, err := client.GetBlockCount()
            if err != nil {
                    log.Fatal(err)
            }
            log.Printf("Block count: %d", blockCount)
        }
}
