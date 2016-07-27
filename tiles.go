package main

import "gopkg.in/redis.v4"
import "sync"
import "strconv"
import "errors"
import "time"

const (
	STATE_LOCKED = 1
	STATE_OPEN   = 2
)

type TileManager struct {
	NumTiles int
	Client   *redis.Client
	lock     sync.Mutex
}

func NewTileManager(numTiles int, client *redis.Client) *TileManager {
	return &TileManager{
		NumTiles: numTiles,
		Client:   client,
	}
}

func (tm *TileManager) Lock(tile int, duration time.Duration) (error, int) {
	if tile >= tm.NumTiles {
		return errors.New("This tile is not available"), -1
	}

	tm.lock.Lock()
	defer tm.lock.Unlock()

	val, err := tm.Client.SetNX(tm.keyForTile(tile), 1, duration).Result()
	if err != nil {
		return err, -1
	}

	if val == true {
		return nil, STATE_LOCKED
	} else {
		return nil, STATE_OPEN
	}
}

func (tm *TileManager) keyForTile(tile int) string {
	return "tile:" + strconv.Itoa(tile)
}

func (tm *TileManager) GetState() []int {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	result := make([]int, tm.NumTiles)
	for i := 0; i < tm.NumTiles; i++ {
		val, err := tm.Client.Get(tm.keyForTile(i)).Result()
		if err != nil && err != redis.Nil {
			panic(err)
		}
		if val == "1" {
			result[i] = STATE_LOCKED
		} else {
			result[i] = STATE_OPEN
		}
	}
	return result
}
