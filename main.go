package main

import (
        "net/http"
	"io/ioutil"
	"log"
	"path/filepath"
	"encoding/json"
        "time"

        "github.com/btcsuite/btcd/wire"
        "github.com/btcsuite/btcrpcclient"
	"github.com/btcsuite/btcutil"
        "github.com/gorilla/securecookie"
        "github.com/gorilla/mux"
        "github.com/satori/go.uuid"
        "gopkg.in/redis.v4"
        "github.com/btcsuite/btcd/chaincfg"
)

var secureCookie *securecookie.SecureCookie
var client *redis.Client
var tileManager *TileManager

const (
    KEY_1 = "QQByVLj7UQHXmiWiHdV17HQQVLQXUjyB"
    KEY_2 = "HHQULVBjVXQHVQQX1LB7yLiWjHLQ7dH1QyijByUVVHVmQmXQiWijdUQQQU77ByXQ"
    N_ADS = 12
)

func TileHandler(w http.ResponseWriter, r *http.Request) {
    states := tileManager.GetState()
    encoder := json.NewEncoder(w)
    err := encoder.Encode(states)
    if err != nil {
        panic(err)
    }
}

func AddressesHandler(w http.ResponseWriter, r *http.Request) {

    // Get cookies
    var uniqueIdentifier uuid.UUID
    value := make(map[string]string)
    if cookie, err := r.Cookie("uuid"); err == nil {
        if err = secureCookie.Decode("uuid", cookie.Value, &value); err == nil {
            uniqueIdentifier, err = uuid.FromString(value["uuid"])
            if err != nil {
                panic(err)
            }
        } else {
            panic(err)
        }
    } else {
        uniqueIdentifier = uuid.NewV4()
        asd := map[string]string{
            "uuid": uniqueIdentifier.String(),
        }
        if encoded, err := secureCookie.Encode("uuid", asd); err == nil {
            http.SetCookie(w, &http.Cookie{
                Name: "uuid",
                Value: encoded,
            })
        } else {
            panic(err)
        }
    }
    log.Println(uniqueIdentifier.String())

    // Get keypair
    manager := NewKeyManager(client, uniqueIdentifier)
    chain, err := manager.GetChain()
    if err != nil {
        panic(err)
    }

    pkeys := make([]string, N_ADS)
    for i:=0; i < N_ADS; i++ {
        acct, _ := chain.Child(uint32(i))
        addr, _ := acct.Address(&chaincfg.MainNetParams)
        pkeys[i] = addr.EncodeAddress()
    }

    encoder := json.NewEncoder(w)
    encoder.Encode(pkeys)
    w.Header().Set("Content-Type", "application/json")
}

func init() {
        secureCookie = securecookie.New(
            []byte(KEY_2),
            []byte(KEY_1),
        )
        client = redis.NewClient(&redis.Options{
            Addr:     "localhost:6379",
            Password: "", // no password set
            DB:       0,  // use default DB
        })
        tileManager = NewTileManager(N_ADS, client)
}

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
		Host:         "localhost:18556",
		Endpoint:     "ws",
		User:         "admin",
		Pass:         "admin",
		Certificates: certs,
	}
	client, err := btcrpcclient.New(connCfg, &ntfnHandlers)
	if err != nil {
		log.Fatal(err)
	}
        log.Println(client)

        // address router
        r := mux.NewRouter()
        r.HandleFunc("/addresses", AddressesHandler)
        r.HandleFunc("/tiles", TileHandler)
        log.Fatal(http.ListenAndServe(":8000", r))
}
