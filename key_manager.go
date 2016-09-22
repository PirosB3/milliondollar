package main

import "io"
import "io/ioutil"
import "strings"
import "bytes"
import "time"
import "gopkg.in/redis.v4"
import "github.com/satori/go.uuid"
import "github.com/btcsuite/btcutil/hdkeychain"
import "github.com/btcsuite/btcd/chaincfg"
import "github.com/btcsuite/btcrpcclient"
import "github.com/btcsuite/btcutil"
import "github.com/jinzhu/gorm"
import _ "github.com/jinzhu/gorm/dialects/postgres"

const SESSION_LIFE = time.Hour * 24 * 30

type AddressGenerator interface {
	MakeAddresses(num int) []string
        GetAddressBalances(num int) []float64
}

type WalletManager struct {
	Client     *btcrpcclient.Client
	identifier uuid.UUID
}

func NewWalletManager(
	client *btcrpcclient.Client,
	identifier uuid.UUID,
) *WalletManager {
	return &WalletManager{
		Client:     client,
		identifier: identifier,
	}
}

func (k *WalletManager) MakeAddresses(num int) []string {
	// Get existing addresses
	var addresses []btcutil.Address
	var err error
	if addresses, err = k.Client.GetAddressesByAccount(k.identifier.String()); err != nil {
		err = k.Client.CreateNewAccount(k.identifier.String())
		if err != nil {
			panic(err)
		}
	}

	// Create remaining addresses
	res := make([]string, num)
	for i := 0; i < num; i++ {
		if i < len(addresses) {
			res[i] = addresses[i].String()
		} else {
			newAddress, err := k.Client.GetNewAddress(k.identifier.String())
			if err != nil {
				panic(err)
			}
			res[i] = newAddress.String()
		}
	}
	return res
}

type KeyManager struct {
	dbs        *gorm.DB
	client     *redis.Client
	identifier uuid.UUID
}

func (k *KeyManager) GetAddressBalances(num int) []float64 {
    addresses := k.MakeAddresses(num)
    balances := make([]float64, len(addresses))
    for i, address := range addresses {
        rows, err := k.dbs.Table("transactions").Select(
            "sum(amount)",
        ).Where(
            "address = ? AND spent = ?",
            address, false,
        ).Rows()
        if err != nil {
            Error.Fatal(err)
        }
        defer rows.Close()

        var balance float64
        rows.Next()
        rows.Scan(&balance)

        balances[i] = balance
    }
    return balances
}

func (k *KeyManager) MakeAddresses(num int) []string {
	chain, err := k.GetChain()
	if err != nil {
		panic(err)
	}

	pkeys := make([]string, num)
	for i := 0; i < num; i++ {
		acct, _ := chain.Child(uint32(i))
		addr, _ := acct.Address(&chaincfg.SimNetParams)
		pkeys[i] = addr.EncodeAddress()
                k.client.SAdd("known_addresses", pkeys[i])
	}
	return pkeys
}

func (k *KeyManager) GetChain() (*hdkeychain.ExtendedKey, error) {
	masterKeyByteSlice, err := ioutil.ReadAll(k.GetMasterKey())
	if err != nil {
		return nil, err
	}

	ek, err := hdkeychain.NewMaster(masterKeyByteSlice, &chaincfg.SimNetParams)
	if err != nil {
		return nil, err
	}
	return ek, nil
}

func (k *KeyManager) GetMasterKey() io.Reader {
	identifierKey := "session:" + k.identifier.String()
	val, err := k.client.Get(identifierKey).Result()
	if err != nil && err != redis.Nil {
		panic(err)
	}

	// If value not present, create key. Else, renew
	if err == redis.Nil {
		Info.Printf("Session not found for user %s. generating a new one", k.identifier.String())
		newSeed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
		if err != nil {
			panic(err)
		}
		k.client.SetNX(identifierKey, newSeed, SESSION_LIFE).Result()
		return bytes.NewReader(newSeed)
	} else {
		Info.Printf("Session found for user %s. renewing", k.identifier.String())
		k.client.Expire(identifierKey, SESSION_LIFE)
		return strings.NewReader(val)
	}
}

func NewKeyManager(client *redis.Client, identifier uuid.UUID, dbs *gorm.DB) *KeyManager {
	return &KeyManager{
		client:     client,
		identifier: identifier,
                dbs: dbs,
	}
}
