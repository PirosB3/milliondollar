package main

import "os"
import "io"
import "io/ioutil"
import "log"
import "strings"
import "bytes"
import "time"
import "gopkg.in/redis.v4"
import "github.com/satori/go.uuid"
import "github.com/btcsuite/btcutil/hdkeychain"
import "github.com/btcsuite/btcd/chaincfg"

const SESSION_LIFE = time.Hour * 24 * 30
var Info *log.Logger


type KeyManager struct {
    client *redis.Client
    identifier uuid.UUID
}

func init() {
    Info = log.New(os.Stdout, "Info: ", log.Ldate|log.Ltime|log.Lshortfile)
}

func (k *KeyManager) GetChain() (*hdkeychain.ExtendedKey, error) {
    masterKeyByteSlice, err := ioutil.ReadAll(k.GetMasterKey())
    if err != nil {
        return nil, err
    }

    ek, err := hdkeychain.NewMaster(masterKeyByteSlice, &chaincfg.MainNetParams)
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

func NewKeyManager(client *redis.Client, identifier uuid.UUID) *KeyManager {
    return &KeyManager{
        client: client,
        identifier: identifier,
    }
}
