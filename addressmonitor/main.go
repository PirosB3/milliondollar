package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcrpcclient"
	"github.com/btcsuite/btcutil"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/spf13/viper"
	"gopkg.in/redis.v4"
)

var (
	client    *redis.Client
	Info      *log.Logger
	Error     *log.Logger
	RPCClient *btcrpcclient.Client
	dbs       *gorm.DB
)

type Transaction struct {
	gorm.Model
	Uid           string  `gorm:"index;unique"`
	TransactionId string  `gorm:"not null`
	Idx           uint32  `gorm:"not null`
	Address       string  `gorm:"not null"`
	Amount        float64 `gorm:"not null"`
	Spent         bool    `gorm:"not null"`
}

func init() {
	Info = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	Error = log.New(os.Stdout, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)

	// add configuration directory
	viper.SetConfigName("app")
	usr, _ := user.Current()
	viper.AddConfigPath(filepath.Join(usr.HomeDir, ".mdp/"))
	err := viper.ReadInConfig()
	if err != nil {
		Error.Fatal(err)
	}

	dbs, err = gorm.Open("postgres", viper.GetString("db.pg"))
	if err != nil {
		Error.Fatal(err)
	}
	dbs.AutoMigrate(&Transaction{})

	client = redis.NewClient(&redis.Options{
		Addr:     viper.GetString("db.redis"),
		Password: "", // no password set
		DB:       0,  // use default DB
	})

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
}

func GetLastSyncedBlock() int64 {
	res, err := client.Get("last_synced_block").Result()
	if err == redis.Nil {
		return 0
	} else if err != nil {
		Error.Fatal(err)
	}

	i, err := strconv.ParseInt(res, 10, 64)
	if err != nil {
		Error.Fatal(err)
	}
	return i
}

func GetCurrentBlockCount() int64 {
	count, err := RPCClient.GetBlockCount()
	if err != nil {
		Error.Fatal(err)
	}
	return count
}

func OperateMempool() {
	failCount := make(map[*chainhash.Hash]int)
	for {
		results, err := RPCClient.GetRawMempool()
		if err != nil {
			Error.Panic(err)
		}

		for _, result := range results {
			rawTx, err := RPCClient.GetRawTransaction(result)
			if err != nil {
				if val, ok := failCount[result]; ok {
					if val >= 2 {
						Error.Panic(err)
					} else {
						failCount[result] += 1
					}
				} else {
					failCount[result] = 0
				}
				Error.Println("Hash", result, "has failed", failCount[result], "times")
				continue
			} else {
				delete(failCount, result)
			}
			rawTxMsg := rawTx.MsgTx()

			data := make([]byte, 0, rawTxMsg.SerializeSize())
			buf := bytes.NewBuffer(data)
			rawTxMsg.Serialize(buf)

			_, err = RPCClient.DecodeRawTransaction(buf.Bytes())
			if err != nil {
				Error.Panic(err)
			}

			for _, input := range rawTxMsg.TxIn {

				// Mark every input as spent for N * 2 seconds
				hash := input.PreviousOutPoint.Hash.String()
				idx := input.PreviousOutPoint.Index
				client.Set(
					fmt.Sprintf("spent_tx_in_mempool:%s:%d", hash, idx), "1", time.Second*7,
				)
				Info.Println("Added hash", hash, "to mempool")
			}

		}
		time.Sleep(time.Second * 5)
	}
}

func main() {

	go OperateMempool()

	for {
		currentBlock := GetCurrentBlockCount()
		lastSyncedBlock := GetLastSyncedBlock()

		if currentBlock == lastSyncedBlock {
			Info.Println("Blocks are up to date, sleeping for 5s")
			time.Sleep(time.Second * 5)
			continue
		}

		for i := lastSyncedBlock; i <= currentBlock; i++ {

			hash, err := RPCClient.GetBlockHash(i)
			if err != nil {
				Error.Fatal(err)
			}
			Info.Println("Syncing block", i, hash)
			block, err := RPCClient.GetBlock(hash)
			if err != nil {
				Error.Fatal(err)
			}

			txs := block.Transactions()
			for _, tk := range txs {
				msgTx := tk.MsgTx()

				data := make([]byte, 0, msgTx.SerializeSize())
				buf := bytes.NewBuffer(data)
				msgTx.Serialize(buf)

				res2, err := RPCClient.DecodeRawTransaction(buf.Bytes())
				if err != nil {
					Error.Fatal(err)
				}

				transactionId := res2.Txid
				// Process spent inputs
				for _, input := range res2.Vin {
					inputTransaction := input.Txid
					idx := input.Vout

					// Try to fetch the transaction
					idxStr := strconv.FormatUint(uint64(idx), 10)
					var transaction Transaction
					err := dbs.Model(&transaction).Where(
						"uid = ?",
						inputTransaction+":"+idxStr,
					).Update("spent", true).Error

					if err == nil {
						Info.Printf("Marked transaction %s idx %d as spent", inputTransaction, idx)
					}

				}

				// Process unspent outputs
				for _, output := range res2.Vout {
					for _, address := range output.ScriptPubKey.Addresses {

						// If address is not known, ignore
						if seen, _ := client.SIsMember("known_addresses", address).Result(); !seen {
							continue
						}

						idx := output.N
						value := output.Value

						idxStr := strconv.FormatUint(uint64(idx), 10)
						transaction := &Transaction{
							Uid:           transactionId + ":" + idxStr,
							TransactionId: transactionId,
							Idx:           idx,
							Address:       address,
							Amount:        value,
							Spent:         false,
						}
						dbs.Create(transaction)
						Info.Printf("Address %s received %f funds from TX %s idx %d\n", address, value, transactionId, idx)
					}

				}
			}
			client.Set("last_synced_block", i, 0)
			if err != nil {
				Error.Fatal(err)
			}
		}
	}
}
