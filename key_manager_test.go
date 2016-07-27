package main

import "testing"
import "io/ioutil"
import "github.com/satori/go.uuid"
import "gopkg.in/redis.v4"
import "github.com/btcsuite/btcutil/hdkeychain"

var client *redis.Client

func init() {
    client = redis.NewClient(&redis.Options{
        Addr:     "localhost:6379",
        Password: "", // no password set
        DB:       0,  // use default DB
    })
}

func TestKeyManagerWorks(t *testing.T) {
    identifier := uuid.NewV4()
    manager := NewKeyManager(client, identifier)
    masterKey := manager.GetMasterKey()

    res1, _ := ioutil.ReadAll(masterKey)
    if len(res1) != hdkeychain.RecommendedSeedLen {
        t.Fail()
    }

    // Test renewal
    manager = NewKeyManager(client, identifier)
    masterKey = manager.GetMasterKey()
    res2, _ := ioutil.ReadAll(masterKey)

    if string(res1) != string(res2) {
        t.Fail()
    }
}

func TestMasterKeyEntity(t *testing.T) {
    identifier := uuid.NewV4()
    manager := NewKeyManager(client, identifier)
    chain, err := manager.GetChain()
    t.Log(err)
    if !chain.IsPrivate() {
        t.Fail()
    }
}
