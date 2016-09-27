package main

import "io"
import "errors"
import "encoding/hex"
import "io/ioutil"
import "strings"
import "bytes"
import "time"
import "gopkg.in/redis.v4"
import "github.com/satori/go.uuid"
import "github.com/btcsuite/btcutil/hdkeychain"
import "github.com/btcsuite/btcd/chaincfg"
import "github.com/btcsuite/btcrpcclient"
import "github.com/btcsuite/btcd/wire"
import "github.com/btcsuite/btcd/txscript"
import "github.com/btcsuite/btcd/chaincfg/chainhash"
import "github.com/btcsuite/btcutil"
import "github.com/jinzhu/gorm"
import "github.com/btcsuite/btcd/btcec"
import _ "github.com/jinzhu/gorm/dialects/postgres"

const SESSION_LIFE = time.Hour * 24 * 30

type AddressGenerator interface {
	PerformPurchase(address btcutil.Address, amount float64, dstAddress btcutil.Address)
	MakeAddresses(num int) []string
	GetAddressBalances(num int) []float64
	GetBalanceForAddress(address string) float64
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
        rpc        *btcrpcclient.Client
	identifier uuid.UUID
        addressMap map[string]*btcec.PrivateKey
}

func (k *KeyManager) GetAddressBalances(num int) []float64 {
	addresses := k.MakeAddresses(num)
	balances := make([]float64, len(addresses))
	for i, address := range addresses {
		balances[i] = k.GetBalanceForAddress(address)
	}
	return balances
}

func (k *KeyManager) GetBalanceForAddress(address string) float64 {
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
	return balance
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
                privKey, _ := acct.ECPrivKey()
                k.addressMap[pkeys[i]] = privKey
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

func (k *KeyManager) Unspent(address string, amount float64) ([]*wire.OutPoint, float64) {
	rows, err := k.dbs.Table("transactions").Select(
            "transaction_id, idx, amount",
	).Where(
		"address = ? AND spent = ?",
		address, false,
	).Rows()
        if err != nil {
            Error.Fatal(err)
        }
        defer rows.Close()

        var res float64 = 0
        var outPoints []*wire.OutPoint
        for rows.Next() && res < amount {
            var transactionId string
            var idx int
            var amount float64
            rows.Scan(&transactionId, &idx, &amount)

            txHash, err := chainhash.NewHashFromStr(transactionId)
            if err != nil {
                Error.Fatal(err)
            }
            op := wire.NewOutPoint(
                txHash, uint32(idx),
            )
            outPoints = append(outPoints, op)
            res += amount
        }
        return outPoints, res
}

func (k *KeyManager) PerformPurchase(address btcutil.Address, amount float64, dstAddress btcutil.Address) {
    // Get all unspent transactions fot amount
    inputs, totalSpent := k.Unspent(address.String(), amount)
    totalSpent *= 100000000
    amount *= 100000000

    totalSpentInt := int64(totalSpent)
    amountInt := int64(amount)

    // Create TXins
    tx := wire.NewMsgTx()
    for _, input := range inputs {
        txIn := wire.NewTxIn(input, nil)
        tx.AddTxIn(txIn)
    }

    // Create pay-to-addr script
    pkScript, err := txscript.PayToAddrScript(dstAddress)
    if err != nil {
        Error.Fatal(err)
    }

    // Add transactions
    txOut := wire.NewTxOut(amountInt, pkScript)
    tx.AddTxOut(txOut)

    delta := totalSpentInt - amountInt
    if delta > 0 {
        changePkScript, err := txscript.PayToAddrScript(address)
        if err != nil {
            Error.Fatal(err)
        }
        changeTxOut := wire.NewTxOut(delta, changePkScript)
        tx.AddTxOut(changeTxOut)
    }

    // Sign inputs
    for _, txin := range tx.TxIn {
        hash := txin.PreviousOutPoint.Hash
        prevTx, _ := k.rpc.GetRawTransaction(&hash)

        sigScript, err := txscript.SignTxOutput(
            &chaincfg.SimNetParams, tx, 0, prevTx.MsgTx().TxOut[0].PkScript,
            txscript.SigHashAll, k, nil, nil,
        )
        if err != nil {
            Error.Panic(err)
        }
        txin.SignatureScript = sigScript
    }

    data := make([]byte, 0, tx.SerializeSize())
    buf := bytes.NewBuffer(data)
    tx.Serialize(buf)
    Info.Println(hex.EncodeToString(buf.Bytes()))
}

func (k *KeyManager) GetKey(address btcutil.Address) (*btcec.PrivateKey, bool, error) {
    if pk := k.addressMap[address.String()]; pk != nil {
        return pk, true, nil
    }

    return nil, false, errors.New("Could not find key")
}

func NewKeyManager(client *redis.Client, identifier uuid.UUID, dbs *gorm.DB, rpc *btcrpcclient.Client) *KeyManager {
	return &KeyManager{
		client:     client,
		identifier: identifier,
		dbs:        dbs,
                rpc:        rpc,
                addressMap: make(map[string]*btcec.PrivateKey),
	}
}
