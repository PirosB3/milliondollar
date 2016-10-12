package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcrpcclient"
	"github.com/btcsuite/btcutil"
	"github.com/gorilla/mux"
	"github.com/gorilla/securecookie"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/satori/go.uuid"
	"github.com/spf13/viper"
	"gopkg.in/redis.v4"
)

var (
	secureCookie     *securecookie.SecureCookie
	client           *redis.Client
	tileManager      *TileManager
	Info             *log.Logger
	Error            *log.Logger
	RPCClient        *btcrpcclient.Client
	BankAddress      btcutil.Address
	RootPage         []byte
	IndexRefreshLock sync.RWMutex
	dbs              *gorm.DB
	currentDirectory string
	N_ADS            int
	AD_COST          float64
	bank             string
	net              *chaincfg.Params
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

type PriceHandlerPayload struct {
	Price float64 `json:"price"`
}

func PriceMiddleware(w http.ResponseWriter, r *http.Request) {
	p := &PriceHandlerPayload{
		Price: AD_COST,
	}
	encoder := json.NewEncoder(w)
	encoder.Encode(p)
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

	// Get address in frame
	address := details.Keys.MakeAddresses(N_ADS)[data.FrameNumber]

	// Check balance
	balance := details.Keys.GetBalanceForAddress(address)
	if balance < AD_COST {
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
	addrInstance, _ := btcutil.DecodeAddress(address, net)
	txid := details.Keys.PerformPurchase(addrInstance, 0.5, BankAddress)

	// Set AD for 5 minutes
	tileManager.PurchaseTile(
		data.FrameNumber,
		data.Message,
		5*time.Minute,
	)

	return 200, map[string]string{
		"transaction_id": txid,
	}
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
		manager := NewKeyManager(client, uniqueIdentifier, dbs, RPCClient, net)
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
	// Initialize loggers
	Info = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	Error = log.New(os.Stdout, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)

	// get current directory of file
	_, filename, _, _ := runtime.Caller(1)
	currentDirectory = path.Dir(filename)

	// add configuration directory
	viper.SetConfigName("app")
	usr, _ := user.Current()
	viper.AddConfigPath(filepath.Join(usr.HomeDir, ".mdp/"))
	err := viper.ReadInConfig()
	if err != nil {
		Error.Fatal(err)
	}
	N_ADS = viper.GetInt("business.n_ads")
	AD_COST = viper.GetFloat64("business.ad_cost")

	// Initialize Cookies
	secureCookie = securecookie.New(
		[]byte(viper.GetString("cookie.key2")),
		[]byte(viper.GetString("cookie.key1")),
	)

	// Initialize Redis
	client = redis.NewClient(&redis.Options{
		Addr:     viper.GetString("db.redis"),
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// Initialize PG
	dbs, err = gorm.Open("postgres", viper.GetString("db.pg"))
	if err != nil {
		Error.Fatal(err)
	}

	// Initialize BTCD
	btcdHomeDir := btcutil.AppDataDir("btcd", false)
	certs, err := ioutil.ReadFile(filepath.Join(btcdHomeDir, "rpc.cert"))
	if err != nil {
		Error.Fatal(err)
	}
	connCfg := &btcrpcclient.ConnConfig{
		Host:         viper.GetString("db.btcd.host"),
		Endpoint:     "ws",
		User:         viper.GetString("db.btcd.username"),
		Pass:         viper.GetString("db.btcd.password"),
		Certificates: certs,
	}
	RPCClient, err = btcrpcclient.New(connCfg, nil)
	if err != nil {
		Error.Fatal(err)
	}

	// Init the tile manager and redeem adress
	tileManager = NewTileManager(N_ADS, client)
	bank = viper.GetString("business.bank")

	// Get params
	if viper.GetBool("db.btcd.is_simnet") {
		Info.Println("Using SimNet")
		net = &chaincfg.SimNetParams
	} else {
		Info.Println("Using MainNet")
		net = &chaincfg.MainNetParams
	}
}

func refreshRootPage() error {
	var err error
	RootPage, err = ioutil.ReadFile(currentDirectory + "/templates/index.html")
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

	// Get Address for purchase
	var err error
	BankAddress, err = btcutil.DecodeAddress(bank, net)
	if err != nil {
		Error.Fatal(err)
	}

	// address router
	Info.Println(currentDirectory)
	r := mux.NewRouter()
	r.HandleFunc("/price", PriceMiddleware).Methods("GET")
	r.HandleFunc("/addresses", AuthMiddleware(AddressesHandler)).Methods("GET")
	r.HandleFunc("/tiles", AuthMiddleware(TileHandler)).Methods("GET")
	r.HandleFunc("/tile", AuthMiddleware(TileLockHandler)).Methods("POST")
	r.HandleFunc("/purchase", AuthMiddleware(ResponseByReturnHandler(TilePurchasehandler))).Methods("POST")
	r.HandleFunc("/", RootHandler).Methods("GET")
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir(currentDirectory+"/static/"))))
	Error.Fatal(http.ListenAndServe(":8000", r))
}
