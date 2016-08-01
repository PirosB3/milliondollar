package main

import "gopkg.in/redis.v4"
import "sync"
import "strconv"
import "errors"
import "time"
import "github.com/satori/go.uuid"

const (
	STATE_LOCKED_BY_OTHER        = 1
	STATE_OPEN                   = 2
	STATE_LOCKED_BY_CURRENT_USER = 3
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

func (tm *TileManager) Lock(tile int, duration time.Duration, locker uuid.UUID) (error, int) {
	if tile >= tm.NumTiles {
		return errors.New("This tile is not available"), -1
	}

	tm.lock.Lock()
	defer tm.lock.Unlock()

	val, err := tm.Client.SetNX(
		tm.keyForTile(tile), locker.String(), duration,
	).Result()
	if err != nil && err != redis.Nil {
		return err, -1
	}

	if val == true {
		return nil, STATE_LOCKED_BY_CURRENT_USER
	} else {
		return nil, STATE_OPEN
	}
}

func (tm *TileManager) keyForTile(tile int) string {
	return "tile:" + strconv.Itoa(tile)
}

func (tm *TileManager) GetState(locker uuid.UUID) []int {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	result := make([]int, tm.NumTiles)
	for i := 0; i < tm.NumTiles; i++ {
		val, err := tm.Client.Get(tm.keyForTile(i)).Result()

		if err == redis.Nil {
			result[i] = STATE_OPEN
		} else if err != nil {
			Error.Fatal(err)
		}

		if val == locker.String() {
			result[i] = STATE_LOCKED_BY_CURRENT_USER
		} else if val == "" {
			result[i] = STATE_OPEN
		} else {
			result[i] = STATE_LOCKED_BY_OTHER
		}
	}
	return result
}
