package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/btcsuite/btcrpcclient"
	"github.com/btcsuite/btcutil"
	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
	"github.com/satori/go.uuid"
	"gopkg.in/redis.v4"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
        "github.com/btcsuite/btcd/chaincfg"
)

var (
	secureCookie     *securecookie.SecureCookie
	client           *redis.Client
	tileManager      *TileManager
	Info             *log.Logger
	Error            *log.Logger
	RPCClient        *btcrpcclient.Client
	RootAddress      btcutil.Address
	RootPage         []byte
	IndexRefreshLock sync.RWMutex
	dbs       *gorm.DB
)

const (
	KEY_1   = "QQByVLj7UQHXmiWiHdV17HQQVLQXUjyB"
	KEY_2   = "HHQULVBjVXQHVQQX1LB7yLiWjHLQ7dH1QyijByUVVHVmQmXQiWijdUQQQU77ByXQ"
	N_ADS   = 6
	AD_COST = 2.0
)

type UserDetails struct {
	SessionId uuid.UUID
	Keys      AddressGenerator
}

type AddressBalancePair struct {
	Address string  `json:"address"`
	Balance float64 `json:"balance"`
}

type TileLockHandlerPayload struct {
	FrameNumber int `json:"frame_number"`
}

type TilePurchaseHandlerPayload struct {
	FrameNumber int    `json:"frame_number"`
	Message     string `json:"message"`
}

func RootHandler(w http.ResponseWriter, r *http.Request) {
	IndexRefreshLock.RLock()
	reader := bytes.NewReader(RootPage)
	_, err := reader.WriteTo(w)
	IndexRefreshLock.RUnlock()
	if err != nil {
		Error.Fatal(err)
	}
}

func ResponseByReturnHandler(
	fn func(http.ResponseWriter, *http.Request, *UserDetails) (int, interface{}),
) func(http.ResponseWriter, *http.Request, *UserDetails) {
	return func(w http.ResponseWriter, r *http.Request, details *UserDetails) {

		statusCode, data := fn(w, r, details)
		w.WriteHeader(statusCode)

		writer := json.NewEncoder(w)
		err := writer.Encode(data)
		if err != nil {
			Error.Fatal(err)
		}
	}
}

func TilePurchasehandler(w http.ResponseWriter, r *http.Request, details *UserDetails) (int, interface{}) {
	var data TilePurchaseHandlerPayload
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&data)
	if err != nil {
		Error.Fatal(err)
	}

	// Check balance
	balance, err := RPCClient.GetBalance(details.SessionId.String())
	if err != nil {
		Error.Fatal(err)
	}
	balanceF64 := balance.ToBTC()
	Info.Println(details.SessionId.String(), balanceF64)
	if balanceF64 < AD_COST {
		return 400, map[string]string{
			"error": "funds are insufficient",
		}
	}

	// Only one purchase at a time
	tileManager.PurchaseLock.Lock()
	defer tileManager.PurchaseLock.Unlock()

	// Ensure Tile was locked by current user
	canPurchase, err := tileManager.CanPurchase(data.FrameNumber, details.SessionId)
	if !canPurchase {
		return 400, map[string]string{
			"error": err.Error(),
		}
	}

	// Perform transaction
	hash, err := RPCClient.SendFrom(details.SessionId.String(), RootAddress, 200000000)
	if err != nil {
		Error.Fatal(err)
	}

	// Set AD for 5 minutes
	tileManager.PurchaseTile(
		data.FrameNumber,
		data.Message,
		5*time.Minute,
	)

	return 200, hash
}

func TileLockHandler(w http.ResponseWriter, r *http.Request, details *UserDetails) {
	var data TileLockHandlerPayload
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&data)
	if err != nil {
		Error.Fatal(err)
	}

	if data.FrameNumber < 0 || data.FrameNumber >= N_ADS {
		Error.Fatal("Number of ads invalid")
	}

	err, res := tileManager.Lock(
		data.FrameNumber, time.Minute*5, details.SessionId,
	)
	if err != nil {
		Error.Fatal(err)
	}

	payload := make(map[string]string)
	payload["State"] = res
	encoder := json.NewEncoder(w)
	err = encoder.Encode(payload)
	if err != nil {
		Error.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
}

type TileMessagePair struct {
	Message string        `json:"message"`
	State   string        `json:"state"`
	TTL     time.Duration `json:"ttl"`
}

func TileHandler(w http.ResponseWriter, r *http.Request, details *UserDetails) {
	states := tileManager.GetState(details.SessionId)
	results := make([]*TileMessagePair, len(states))
	for i, state := range states {
		key := tileManager.keyForTile(i)

		message := ""
		var ttl time.Duration = -1
		if state != "OPEN" {
			ttl, _ = client.TTL(key).Result()
			ttl /= 1000000000
		}

		if state == "PURCHASED" {
			bodyKey := tileManager.KeyForBody(i)
			message, _ = client.Get(bodyKey).Result()
		}

		results[i] = &TileMessagePair{
			Message: message,
			State:   state,
			TTL:     ttl,
		}
	}
	encoder := json.NewEncoder(w)
	err := encoder.Encode(results)
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
		manager := NewKeyManager(client, uniqueIdentifier, dbs)
		details := &UserDetails{
			SessionId: uniqueIdentifier,
			Keys:      manager,
		}

		fn(w, r, details)
	}
}

func AddressesHandler(w http.ResponseWriter, r *http.Request, details *UserDetails) {

	// Get keypair
	Info.Println(details.SessionId.String())
	pkeys := details.Keys.MakeAddresses(N_ADS)

	res := make([]*AddressBalancePair, len(pkeys))
        balances := details.Keys.GetAddressBalances(len(pkeys))
	for idx, key := range pkeys {
		res[idx] = &AddressBalancePair{
			Address: key,
			Balance: balances[idx],
		}
	}

	encoder := json.NewEncoder(w)
	encoder.Encode(res)
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

	var err error
	dbs, err = gorm.Open("postgres", "host=localhost port=32768 user=postgres sslmode=disable")
	if err != nil {
		Error.Fatal(err)
	}
}

func refreshRootPage() error {
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	var err error
	RootPage, err = ioutil.ReadFile(dir + "/templates/index.html")
	return err
}

func main() {
	refreshRootPage()

	// Refresh root periodically
	go func() {
		for {
			<-time.After(time.Second * 5)
			IndexRefreshLock.Lock()
			refreshRootPage()
			IndexRefreshLock.Unlock()
			Info.Println("Refreshed root page")
		}
	}()

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
	RPCClient, err = btcrpcclient.New(connCfg, nil)
	if err != nil {
		Error.Fatal(err)
	}

	if err = RPCClient.NotifyBlocks(); err != nil {
		Error.Fatal(err)
	}

	// Get Address for purchase
        addressString := "SkbWMEgsoVwVirAsviDb5QL3ug6gzpozN3"
	RootAddress, err = btcutil.DecodeAddress(addressString, &chaincfg.SimNetParams)
	if err != nil {
		Error.Fatal(err)
	}

	// address router
	r := mux.NewRouter()
	r.HandleFunc("/addresses", AuthMiddleware(AddressesHandler)).Methods("GET")
	r.HandleFunc("/tiles", AuthMiddleware(TileHandler)).Methods("GET")
	r.HandleFunc("/tile", AuthMiddleware(TileLockHandler)).Methods("POST")
	r.HandleFunc("/purchase", AuthMiddleware(ResponseByReturnHandler(TilePurchasehandler))).Methods("POST")
	r.HandleFunc("/", RootHandler).Methods("GET")
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))
	Error.Fatal(http.ListenAndServe(":8000", r))
}
