package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcrpcclient"
	"github.com/btcsuite/btcutil"
	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
	"github.com/satori/go.uuid"
	"gopkg.in/redis.v4"
)

var (
	secureCookie *securecookie.SecureCookie
	client       *redis.Client
	tileManager  *TileManager
	Info         *log.Logger
	Error        *log.Logger
	RPCClient    *btcrpcclient.Client
)

const (
	KEY_1 = "QQByVLj7UQHXmiWiHdV17HQQVLQXUjyB"
	KEY_2 = "HHQULVBjVXQHVQQX1LB7yLiWjHLQ7dH1QyijByUVVHVmQmXQiWijdUQQQU77ByXQ"
	N_ADS = 12
)

type UserDetails struct {
	SessionId uuid.UUID
	Keys      *KeyManager
}

type TileLockHandlerPayload struct {
	FrameNumber int `json:"frame_number"`
}

func TileLockHandler(w http.ResponseWriter, r *http.Request, details *UserDetails) {
	var data TileLockHandlerPayload
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&data)
	if err != nil {
		Error.Fatal(err)
	}

	err, res := tileManager.Lock(
		data.FrameNumber, time.Minute*5, details.SessionId,
	)
	if err != nil {
		Error.Fatal(err)
	}

	payload := make(map[string]int)
	payload["State"] = res
	encoder := json.NewEncoder(w)
	err = encoder.Encode(payload)
	if err != nil {
		Error.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
}

func TileHandler(w http.ResponseWriter, r *http.Request, details *UserDetails) {
	states := tileManager.GetState(details.SessionId)
	encoder := json.NewEncoder(w)
	err := encoder.Encode(states)
	if err != nil {
		Error.Fatal(err)
	}
}

func AuthMiddleware(fn func(http.ResponseWriter, *http.Request, *UserDetails)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		// Get cookies
		uuidFetched := false
		var uniqueIdentifier uuid.UUID
		if cookie, err := r.Cookie("uuid"); err == nil {
			value := make(map[string]string)
			if err = secureCookie.Decode("uuid", cookie.Value, &value); err == nil {
				uniqueIdentifier, err = uuid.FromString(value["uuid"])
				if err != nil {
					Error.Fatal(err)
				}
				uuidFetched = true
			} else {
				Error.Fatal(err)
			}
		}

		if !uuidFetched {
			uniqueIdentifier = uuid.NewV4()
			value := map[string]string{
				"uuid": uniqueIdentifier.String(),
			}
			if encoded, err := secureCookie.Encode("uuid", value); err == nil {
				http.SetCookie(w, &http.Cookie{
					Name:  "uuid",
					Value: encoded,
				})
			} else {
				Error.Fatal(err)
			}
		}
		manager := NewKeyManager(client, uniqueIdentifier)
		details := &UserDetails{
			SessionId: uniqueIdentifier,
			Keys:      manager,
		}

		fn(w, r, details)
	}
}

func AddressesHandler(w http.ResponseWriter, r *http.Request, details *UserDetails) {

	// Get keypair
	chain, err := details.Keys.GetChain()
	if err != nil {
		panic(err)
	}

	pkeys := make([]string, N_ADS)
	for i := 0; i < N_ADS; i++ {
		acct, _ := chain.Child(uint32(i))
		addr, _ := acct.Address(&chaincfg.SimNetParams)
		pkeys[i] = addr.EncodeAddress()

		amount, _ := RPCClient.GetBalance(addr.EncodeAddress())
		Info.Println(amount)
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

	Info = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	Error = log.New(os.Stdout, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
}

func main() {
	ntfnHandlers := btcrpcclient.NotificationHandlers{
		OnBlockConnected: func(hash *wire.ShaHash, height int32, time time.Time) {
			Info.Printf("Block connected: %v (%d) %v", hash, height, time)
		},
		OnBlockDisconnected: func(hash *wire.ShaHash, height int32, time time.Time) {
			Info.Printf("Block disconnected: %v (%d) %v", hash, height, time)
		},
	}

	btcdHomeDir := btcutil.AppDataDir("btcd", false)
	certs, err := ioutil.ReadFile(filepath.Join(btcdHomeDir, "rpc.cert"))
	if err != nil {
		Error.Fatal(err)
	}

	connCfg := &btcrpcclient.ConnConfig{
		Host:         "localhost:18556",
		Endpoint:     "ws",
		User:         "admin",
		Pass:         "admin",
		Certificates: certs,
	}
	RPCClient, err = btcrpcclient.New(connCfg, &ntfnHandlers)
	if err != nil {
		Error.Fatal(err)
	}

	// address router
	r := mux.NewRouter()
	r.HandleFunc("/addresses", AuthMiddleware(AddressesHandler)).Methods("GET")
	r.HandleFunc("/tiles", AuthMiddleware(TileHandler)).Methods("GET")
	r.HandleFunc("/tile", AuthMiddleware(TileLockHandler)).Methods("POST")
	Error.Fatal(http.ListenAndServe(":8000", r))
}
